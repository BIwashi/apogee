package summarizer

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"sync"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	oteltrace "go.opentelemetry.io/otel/trace"

	"github.com/BIwashi/apogee/internal/sse"
	"github.com/BIwashi/apogee/internal/store/duckdb"
	apogeesemconv "github.com/BIwashi/apogee/semconv"
)

// Job is a single unit of work: recap the given turn. Reason is a coarse
// trigger label that ends up on log lines so ops can tell manual replays
// from reconstructor-driven runs.
type Job struct {
	TurnID string
	Reason string
}

// Possible Reason values. Extend as new triggers land.
const (
	ReasonTurnClosed = "turn_closed"
	ReasonManual     = "manual"
)

// recapStaleness is the minimum age of an existing recap before a
// re-enqueue is honoured. Anything newer is treated as fresh and skipped.
const recapStaleness = 30 * time.Second

// Worker owns the async job queue. Multiple workers can share a Worker
// value — Start spawns cfg.Concurrency goroutines internally.
type Worker struct {
	cfg    Config
	runner Runner
	store  *duckdb.Store
	hub    *sse.Hub
	logger *slog.Logger
	clock  func() time.Time

	queue chan Job
	wg    sync.WaitGroup

	mu     sync.Mutex
	closed bool
}

// NewWorker constructs a Worker with the given collaborators. A nil logger
// installs a discard logger; a nil clock uses time.Now.
func NewWorker(cfg Config, runner Runner, store *duckdb.Store, hub *sse.Hub, logger *slog.Logger) *Worker {
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelError}))
	}
	if cfg.QueueSize <= 0 {
		cfg.QueueSize = 256
	}
	if cfg.Concurrency <= 0 {
		cfg.Concurrency = 1
	}
	return &Worker{
		cfg:    cfg,
		runner: runner,
		store:  store,
		hub:    hub,
		logger: logger,
		clock:  time.Now,
		queue:  make(chan Job, cfg.QueueSize),
	}
}

// Enqueue drops a job onto the queue without blocking. A full queue logs
// at WARN and drops the job; this is intentional — the dashboard already
// degrades gracefully when a recap never lands.
func (w *Worker) Enqueue(turnID, reason string) {
	if w == nil || turnID == "" {
		return
	}
	w.mu.Lock()
	closed := w.closed
	w.mu.Unlock()
	if closed {
		return
	}
	job := Job{TurnID: turnID, Reason: reason}
	select {
	case w.queue <- job:
	default:
		w.logger.Warn("summarizer queue full — dropping job",
			"turn_id", turnID, "reason", reason)
	}
}

// Start spawns the worker goroutines. Start returns immediately; call
// Stop on shutdown so in-flight jobs drain cleanly. Safe to call once.
func (w *Worker) Start(ctx context.Context) {
	for i := 0; i < w.cfg.Concurrency; i++ {
		w.wg.Add(1)
		go w.loop(ctx, i)
	}
}

// Stop closes the queue and waits for every in-flight job to finish.
func (w *Worker) Stop() {
	w.mu.Lock()
	if w.closed {
		w.mu.Unlock()
		return
	}
	w.closed = true
	close(w.queue)
	w.mu.Unlock()
	w.wg.Wait()
}

func (w *Worker) loop(ctx context.Context, id int) {
	defer w.wg.Done()
	for {
		select {
		case <-ctx.Done():
			return
		case job, ok := <-w.queue:
			if !ok {
				return
			}
			w.process(ctx, id, job)
		}
	}
}

// process runs a single job. All error paths log at WARN and return
// without updating the turn row so the dashboard keeps its current state.
func (w *Worker) process(ctx context.Context, workerID int, job Job) {
	turn, err := w.store.GetTurn(ctx, job.TurnID)
	if err != nil {
		w.logger.Warn("summarizer: load turn", "turn_id", job.TurnID, "err", err)
		return
	}
	if turn == nil {
		w.logger.Warn("summarizer: unknown turn", "turn_id", job.TurnID)
		return
	}

	// Skip very short turns — they are rarely interesting and recap noise
	// would overwhelm the KPI strip.
	if turn.DurationMs != nil && *turn.DurationMs < w.cfg.MinTurnDurationMs {
		w.logger.Debug("summarizer: skip short turn",
			"turn_id", job.TurnID, "duration_ms", *turn.DurationMs)
		return
	}

	// Staleness check: if we already have a recap and it was generated
	// after the turn ended, keep it.
	if turn.RecapJSON != "" && turn.RecapGeneratedAt != nil && job.Reason != ReasonManual {
		if turn.EndedAt == nil || !turn.EndedAt.After(turn.RecapGeneratedAt.Add(-recapStaleness)) {
			w.logger.Debug("summarizer: recap is fresh — skipping",
				"turn_id", job.TurnID)
			return
		}
	}

	spans, err := w.store.GetSpansByTurn(ctx, job.TurnID)
	if err != nil {
		w.logger.Warn("summarizer: load spans", "turn_id", job.TurnID, "err", err)
		return
	}
	logs, err := w.store.ListLogsByTurn(ctx, job.TurnID, 5000)
	if err != nil {
		w.logger.Warn("summarizer: load logs", "turn_id", job.TurnID, "err", err)
		return
	}

	prompt := BuildPrompt(PromptInput{
		Turn:  *turn,
		Spans: spans,
		Logs:  logs,
	}, w.cfg.MaxSpanCount, w.cfg.MaxLogCount)

	runCtx := ctx
	if w.cfg.Timeout > 0 {
		var cancel context.CancelFunc
		runCtx, cancel = context.WithTimeout(ctx, w.cfg.Timeout)
		defer cancel()
	}

	w.logger.Info("summarizer: running",
		"turn_id", job.TurnID, "model", w.cfg.RecapModel,
		"worker", workerID, "reason", job.Reason,
		"span_count", len(spans))

	output, err := w.runner.Run(runCtx, w.cfg.RecapModel, prompt)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return
		}
		w.logger.Warn("summarizer: runner error",
			"turn_id", job.TurnID, "model", w.cfg.RecapModel, "err", err)
		return
	}
	w.logger.Debug("summarizer: raw output",
		"turn_id", job.TurnID, "raw", truncate(output, 2048))

	recap, err := Parse(output, len(spans))
	if err != nil {
		w.logger.Warn("summarizer: parse error",
			"turn_id", job.TurnID, "err", err,
			"raw", truncate(output, 2048))
		return
	}
	now := w.clock()
	recap.GeneratedAt = now
	recap.Model = w.cfg.RecapModel

	blob, err := json.Marshal(recap)
	if err != nil {
		w.logger.Warn("summarizer: marshal recap", "turn_id", job.TurnID, "err", err)
		return
	}
	if err := w.store.UpdateTurnRecap(ctx, job.TurnID, string(blob), recap.Model, now); err != nil {
		w.logger.Warn("summarizer: persist recap", "turn_id", job.TurnID, "err", err)
		return
	}

	w.logger.Info("summarizer: recap written",
		"turn_id", job.TurnID, "model", recap.Model,
		"outcome", recap.Outcome, "phase_count", len(recap.Phases))

	// Emit a post-hoc OTel enrichment span carrying the recap. This is
	// idiomatic OTel for "data that arrived after the original span
	// closed" — we link to the turn root and stamp recap.* attributes
	// so external backends (Jaeger, Tempo, Honeycomb) surface the
	// recap alongside the turn.
	w.emitRecapEnrichmentSpan(ctx, *turn, recap)

	// Broadcast a turn.updated so the live dashboard re-fetches.
	if w.hub != nil {
		t2, err := w.store.GetTurn(ctx, job.TurnID)
		if err == nil && t2 != nil {
			w.hub.Broadcast(sse.NewTurnEvent(sse.EventTypeTurnUpdated, now, *t2))
		}
	}
}

// emitRecapEnrichmentSpan creates a "claude_code.turn.recap" span via
// the global tracer provider, parented at the turn root via a remote
// span context built from the row's stored trace_id and parent_span_id
// pair. The span starts and ends immediately and never blocks the
// worker — failures (bad ids, no exporter) are silently ignored.
func (w *Worker) emitRecapEnrichmentSpan(ctx context.Context, turn duckdb.Turn, recap Recap) {
	tracer := otel.GetTracerProvider().Tracer("apogee/summarizer")
	if tracer == nil {
		return
	}
	tid, err := oteltrace.TraceIDFromHex(turn.TraceID)
	if err != nil {
		return
	}
	// We don't know the root span id from the turn row alone, so we
	// build a parent SpanContext with only the trace id. OTel accepts
	// this and treats the new span as a fresh root inside the same
	// trace, which is exactly the post-hoc enrichment shape we want.
	parent := oteltrace.NewSpanContext(oteltrace.SpanContextConfig{
		TraceID:    tid,
		TraceFlags: oteltrace.FlagsSampled,
		Remote:     true,
	})
	parentCtx := oteltrace.ContextWithRemoteSpanContext(ctx, parent)
	attrs := []attribute.KeyValue{
		apogeesemconv.TurnID.String(turn.TurnID),
		apogeesemconv.RecapHeadline.String(recap.Headline),
		apogeesemconv.RecapOutcome.String(string(recap.Outcome)),
		apogeesemconv.RecapModel.String(recap.Model),
	}
	if len(recap.KeySteps) > 0 {
		attrs = append(attrs, apogeesemconv.RecapKeySteps.StringSlice(recap.KeySteps))
	}
	if recap.FailureCause != nil {
		attrs = append(attrs, apogeesemconv.RecapFailureCause.String(*recap.FailureCause))
	}
	_, span := tracer.Start(parentCtx, apogeesemconv.SpanTurnRecap,
		oteltrace.WithSpanKind(oteltrace.SpanKindInternal),
		oteltrace.WithLinks(oteltrace.Link{SpanContext: parent}),
		oteltrace.WithAttributes(attrs...),
	)
	if span == nil {
		return
	}
	span.End()
}

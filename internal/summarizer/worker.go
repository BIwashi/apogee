package summarizer

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strconv"
	"strings"
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
	prefs  PreferencesReader

	// availability is a snapshot of the model_availability cache. nil
	// means "not yet probed" — ResolveModelForUseCase treats missing
	// entries as available so the worker degrades gracefully when
	// nobody has populated the cache.
	availMu      sync.RWMutex
	availability map[string]bool

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
		prefs:  NewStaticPreferencesReader(Defaults()),
		queue:  make(chan Job, cfg.QueueSize),
	}
}

// SetPreferencesReader installs the operator-controlled preferences source.
// Worker.process calls this reader at the top of every job so prompts
// reflect the latest UI tweak without a restart.
func (w *Worker) SetPreferencesReader(r PreferencesReader) {
	if w == nil || r == nil {
		return
	}
	w.prefs = r
}

// SetAvailability installs the latest model availability snapshot so
// ResolveModelForUseCase can skip probed-unavailable aliases. A nil
// map reverts to "assume everything is available". Safe to call from
// any goroutine.
func (w *Worker) SetAvailability(avail map[string]bool) {
	if w == nil {
		return
	}
	w.availMu.Lock()
	defer w.availMu.Unlock()
	if avail == nil {
		w.availability = nil
		return
	}
	cp := make(map[string]bool, len(avail))
	for k, v := range avail {
		cp[k] = v
	}
	w.availability = cp
}

// Availability returns a safe copy of the current availability map.
// Safe to call from any goroutine.
func (w *Worker) Availability() map[string]bool {
	if w == nil {
		return nil
	}
	w.availMu.RLock()
	defer w.availMu.RUnlock()
	if w.availability == nil {
		return nil
	}
	out := make(map[string]bool, len(w.availability))
	for k, v := range w.availability {
		out[k] = v
	}
	return out
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

	// Load operator preferences (language + system prompt + optional
	// model override) at job start so updates land without a restart.
	prefs := Defaults()
	if w.prefs != nil {
		loaded, err := w.prefs.LoadSummarizerPreferences(ctx)
		if err != nil {
			w.logger.Warn("summarizer: load preferences", "turn_id", job.TurnID, "err", err)
		} else {
			prefs = loaded
		}
	}
	// Resolve the recap model via the catalog + availability cache.
	// Order: preference override > config override > cheapest-available
	// current catalog entry. The resolver never returns the empty
	// string when the catalog is non-empty.
	model := ResolveModelForUseCase(
		UseCaseRecap,
		prefs.RecapModelOverride,
		w.cfg.RecapModel,
		w.Availability(),
	)

	// Load the session's recent open topics so the recap classifier can
	// decide whether this turn continues the most recent topic, opens
	// a new branch, or resumes an earlier one. List failure is
	// non-fatal — the recap still works without topic context, the
	// classifier just degrades to "always new" which the worker then
	// silently drops on the persistence path.
	openTopics, err := w.store.ListOpenTopicsForSession(ctx, turn.SessionID, 5)
	if err != nil {
		w.logger.Warn("summarizer: load open topics",
			"turn_id", job.TurnID, "session_id", turn.SessionID, "err", err)
		openTopics = nil
	}

	prompt := BuildPrompt(PromptInput{
		Turn:       *turn,
		Spans:      spans,
		Logs:       logs,
		OpenTopics: openTopics,
	}, w.cfg.MaxSpanCount, w.cfg.MaxLogCount, prefs)

	runCtx := ctx
	if w.cfg.Timeout > 0 {
		var cancel context.CancelFunc
		runCtx, cancel = context.WithTimeout(ctx, w.cfg.Timeout)
		defer cancel()
	}

	w.logger.Info("summarizer: running",
		"turn_id", job.TurnID, "model", model,
		"language", prefs.Language,
		"worker", workerID, "reason", job.Reason,
		"span_count", len(spans))

	output, err := w.runner.Run(runCtx, model, prompt)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return
		}
		w.logger.Warn("summarizer: runner error",
			"turn_id", job.TurnID, "model", model, "err", err)
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
	recap.Model = model

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

	// Apply the topic decision (Phase 1 of the topic-branch tree feature).
	// All branches degrade gracefully: the recap is already persisted at
	// this point, so a topic-side failure never erases any work.
	if recap.TopicDecision != nil {
		if err := w.applyTopicDecision(ctx, *turn, recap, openTopics, now); err != nil {
			w.logger.Warn("summarizer: apply topic decision",
				"turn_id", job.TurnID, "err", err)
		}
	}

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

// applyTopicDecision resolves the LLM's topic_decision into actual
// session_topics + topic_transitions rows and stamps turns.topic_id.
// Failure paths log and return — the caller already persisted the
// recap so the worst case is a turn that stays unassigned and shows
// up as "unknown" on the topic spine. Idempotent on turn_id.
func (w *Worker) applyTopicDecision(
	ctx context.Context,
	turn duckdb.Turn,
	recap Recap,
	openTopics []duckdb.SessionTopic,
	now time.Time,
) error {
	td := recap.TopicDecision
	if td == nil {
		return nil
	}
	// Marshal the raw decision once — we always store it on the
	// transition row regardless of resolution outcome so future
	// re-runs of the classifier can replay history.
	rawBlob, err := json.Marshal(td)
	if err != nil {
		return fmt.Errorf("marshal topic decision: %w", err)
	}

	// Resolution.
	//
	// kind = "new" or low confidence              → mint a fresh topic id
	// kind = "continue" + at least one open topic → reuse openTopics[0]
	// kind = "resume"  + valid target_topic_ref   → reuse that ref
	// otherwise                                   → record as "unknown"
	//                                               and skip persistence
	var (
		resolvedKind = string(td.Kind)
		toTopicID    string
		fromTopicID  string
		newTopic     *duckdb.SessionTopic
		updatedSeen  *duckdb.SessionTopic
	)
	if len(openTopics) > 0 {
		fromTopicID = openTopics[0].TopicID
	}

	if td.Confidence < TopicMinConfidence {
		resolvedKind = string(TopicKindUnknown)
	} else {
		switch td.Kind {
		case TopicKindNew:
			toTopicID = newTopicID(turn)
			parent := sql.NullString{}
			if fromTopicID != "" {
				parent = sql.NullString{String: fromTopicID, Valid: true}
			}
			goal := strings.TrimSpace(td.Goal)
			if goal == "" {
				goal = recap.Headline
			}
			newTopic = &duckdb.SessionTopic{
				TopicID:       toTopicID,
				SessionID:     turn.SessionID,
				ParentTopicID: parent,
				Goal:          truncate(goal, 200),
				OpenedAt:      now,
				LastSeenAt:    now,
			}
		case TopicKindContinue:
			if len(openTopics) == 0 {
				// No prior topic to continue from — promote to a new one.
				toTopicID = newTopicID(turn)
				newTopic = &duckdb.SessionTopic{
					TopicID:    toTopicID,
					SessionID:  turn.SessionID,
					Goal:       truncate(firstNonEmpty(td.Goal, recap.Headline), 200),
					OpenedAt:   now,
					LastSeenAt: now,
				}
				resolvedKind = string(TopicKindNew)
			} else {
				toTopicID = openTopics[0].TopicID
				bumped := openTopics[0]
				bumped.LastSeenAt = now
				updatedSeen = &bumped
			}
		case TopicKindResume:
			idx, ok := parseRecentRef(td.TargetTopicRef)
			if !ok || idx < 0 || idx >= len(openTopics) {
				// Unparseable / out-of-range reference. Treat as
				// unknown rather than guess.
				resolvedKind = string(TopicKindUnknown)
			} else {
				toTopicID = openTopics[idx].TopicID
				bumped := openTopics[idx]
				bumped.LastSeenAt = now
				updatedSeen = &bumped
			}
		}
	}

	// Always write the transition row, including the unknown / dropped
	// cases — the audit trail is the value here.
	tr := duckdb.TopicTransition{
		TurnID:        turn.TurnID,
		SessionID:     turn.SessionID,
		Kind:          resolvedKind,
		Confidence:    sql.NullFloat64{Float64: td.Confidence, Valid: true},
		Model:         recap.Model,
		PromptVersion: TopicPromptVersion,
		DecisionJSON:  string(rawBlob),
		CreatedAt:     now,
	}
	if fromTopicID != "" {
		tr.FromTopicID = sql.NullString{String: fromTopicID, Valid: true}
	}
	if toTopicID != "" {
		tr.ToTopicID = sql.NullString{String: toTopicID, Valid: true}
	}
	if err := w.store.RecordTopicTransition(ctx, tr); err != nil {
		return fmt.Errorf("record topic transition: %w", err)
	}

	// Skip the per-topic persistence path when the decision was
	// rejected. The transition row above is enough breadcrumbs for the
	// backfill CLI to retry later.
	if resolvedKind == string(TopicKindUnknown) {
		w.logger.Info("summarizer: topic decision rejected",
			"turn_id", turn.TurnID,
			"kind", td.Kind,
			"confidence", td.Confidence)
		return nil
	}

	if newTopic != nil {
		if err := w.store.UpsertSessionTopic(ctx, *newTopic); err != nil {
			return fmt.Errorf("upsert new topic: %w", err)
		}
	} else if updatedSeen != nil {
		if err := w.store.UpsertSessionTopic(ctx, *updatedSeen); err != nil {
			return fmt.Errorf("bump topic last_seen: %w", err)
		}
	}

	if err := w.store.SetTurnTopic(ctx, turn.TurnID, toTopicID); err != nil {
		return fmt.Errorf("stamp turn topic: %w", err)
	}

	w.logger.Info("summarizer: topic decision applied",
		"turn_id", turn.TurnID,
		"session_id", turn.SessionID,
		"kind", resolvedKind,
		"topic_id", toTopicID,
		"confidence", td.Confidence)
	return nil
}

// newTopicID mints a deterministic-ish topic id keyed on the turn.
// We embed the turn id rather than a UUID so debugging is easier:
// `grep <turn_id>` finds both the turn and its anchor topic.
func newTopicID(turn duckdb.Turn) string {
	return "topic-" + turn.TurnID
}

// parseRecentRef parses the "recent:N" form the LLM emits as
// `target_topic_ref`. Tolerates a bare integer too — strings that
// already lack the prefix go straight to strconv.Atoi.
func parseRecentRef(ref string) (int, bool) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return 0, false
	}
	n, err := strconv.Atoi(strings.TrimPrefix(ref, "recent:"))
	if err != nil {
		return 0, false
	}
	return n, true
}

func firstNonEmpty(a, b string) string {
	if v := strings.TrimSpace(a); v != "" {
		return v
	}
	return b
}

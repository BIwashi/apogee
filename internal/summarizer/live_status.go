package summarizer

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/BIwashi/apogee/internal/sse"
	"github.com/BIwashi/apogee/internal/store/duckdb"
)

// LiveStatusJob asks the live-status worker to refresh the
// "currently <verb>-ing <noun>" blurb for a session. Reason is a coarse
// trigger label carried on log lines so ops can tell manual replays
// from ingest-driven runs.
type LiveStatusJob struct {
	SessionID string
	Reason    string
}

// LiveStatusReason values carried on LiveStatusJob.Reason.
const (
	LiveStatusReasonSpanInserted = "span_inserted"
	LiveStatusReasonTurnStarted  = "turn_started"
	LiveStatusReasonManual       = "manual"
)

// liveStatusPromptMaxSpans caps how many recent spans the prompt
// serialises. Keep this tiny — the output is a single sentence and the
// worker fires on every span insert, so prompt cost compounds.
const liveStatusPromptMaxSpans = 8

// liveStatusMaxOutputChars is the hard cap on the returned sentence. The
// model is told to stay under 120 characters; anything longer is hard-
// truncated so a chatty response never blows up the card layout.
const liveStatusMaxOutputChars = 120

// LiveStatusWorker is the live-status tier. One Worker per process.
// Contrary to the per-turn recap worker this one is keyed on session
// id — a session has at most one in-flight representative turn, so
// one live-status row per session is the right unit.
type LiveStatusWorker struct {
	cfg    Config
	runner Runner
	store  *duckdb.Store
	hub    *sse.Hub
	logger *slog.Logger
	clock  func() time.Time

	availMu      sync.RWMutex
	availability map[string]bool

	queue chan LiveStatusJob
	wg    sync.WaitGroup

	mu     sync.Mutex
	closed bool
}

// NewLiveStatusWorker constructs a worker with the given collaborators.
// A nil logger installs a discard logger.
func NewLiveStatusWorker(cfg Config, runner Runner, store *duckdb.Store, hub *sse.Hub, logger *slog.Logger) *LiveStatusWorker {
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelError}))
	}
	if cfg.QueueSize <= 0 {
		cfg.QueueSize = 256
	}
	if cfg.LiveStatusDebounce <= 0 {
		cfg.LiveStatusDebounce = 10 * time.Second
	}
	return &LiveStatusWorker{
		cfg:    cfg,
		runner: runner,
		store:  store,
		hub:    hub,
		logger: logger,
		clock:  time.Now,
		queue:  make(chan LiveStatusJob, cfg.QueueSize),
	}
}

// SetAvailability installs the latest availability snapshot.
func (w *LiveStatusWorker) SetAvailability(avail map[string]bool) {
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
func (w *LiveStatusWorker) Availability() map[string]bool {
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

// Enqueue drops a job onto the queue without blocking. A full queue
// logs at WARN and drops the job — the dashboard degrades gracefully
// when a live status never lands.
func (w *LiveStatusWorker) Enqueue(sessionID, reason string) {
	if w == nil || sessionID == "" {
		return
	}
	w.mu.Lock()
	closed := w.closed
	w.mu.Unlock()
	if closed {
		return
	}
	job := LiveStatusJob{SessionID: sessionID, Reason: reason}
	select {
	case w.queue <- job:
	default:
		w.logger.Warn("live_status queue full — dropping job",
			"session_id", sessionID, "reason", reason)
	}
}

// Start spawns the worker loop.
func (w *LiveStatusWorker) Start(ctx context.Context) {
	w.wg.Add(1)
	go w.loop(ctx)
}

// Stop closes the queue and waits for the inflight job to drain.
func (w *LiveStatusWorker) Stop() {
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

func (w *LiveStatusWorker) loop(ctx context.Context) {
	defer w.wg.Done()
	for {
		select {
		case <-ctx.Done():
			return
		case job, ok := <-w.queue:
			if !ok {
				return
			}
			w.process(ctx, job)
		}
	}
}

// process runs a single job. All error paths log at WARN and return
// without updating the session so the card keeps its last state.
func (w *LiveStatusWorker) process(ctx context.Context, job LiveStatusJob) {
	sess, err := w.store.GetSession(ctx, job.SessionID)
	if err != nil {
		w.logger.Warn("live_status: load session", "session_id", job.SessionID, "err", err)
		return
	}
	if sess == nil {
		return
	}

	// Skip idle sessions — there is nothing to report on a terminal
	// whose representative turn is already closed. The bubble worker
	// will have written live_state="idle" onto the row.
	if sess.LiveState != "live" {
		w.logger.Debug("live_status: skip non-live session",
			"session_id", job.SessionID, "live_state", sess.LiveState)
		return
	}

	// Debounce: if we already have a status and it's newer than the
	// configured window, skip. Manual triggers bypass so the
	// /v1/sessions/:id/live-status POST (if ever added) can force a
	// refresh.
	if job.Reason != LiveStatusReasonManual && sess.LiveStatusAt != nil {
		if w.clock().Sub(*sess.LiveStatusAt) < w.cfg.LiveStatusDebounce {
			w.logger.Debug("live_status: within debounce — skipping",
				"session_id", job.SessionID,
				"age", w.clock().Sub(*sess.LiveStatusAt).String())
			return
		}
	}

	rep, err := w.store.RepresentativeTurn(ctx, job.SessionID)
	if err != nil {
		w.logger.Warn("live_status: representative turn", "session_id", job.SessionID, "err", err)
		return
	}
	if rep == nil || rep.Status != "running" {
		// The session transitioned between the bubble and now — skip
		// so we never stamp a closed turn with a "currently X-ing" line.
		return
	}

	spans, err := w.store.GetSpansByTurn(ctx, rep.TurnID)
	if err != nil {
		w.logger.Warn("live_status: load spans", "turn_id", rep.TurnID, "err", err)
		return
	}

	model := ResolveModelForUseCase(
		UseCaseLiveStatus,
		"", // operator preference override lands via a future preferences field
		w.cfg.LiveStatusModel,
		w.Availability(),
	)

	prompt := buildLiveStatusPrompt(*sess, *rep, spans)

	runCtx := ctx
	if w.cfg.Timeout > 0 {
		var cancel context.CancelFunc
		runCtx, cancel = context.WithTimeout(ctx, w.cfg.Timeout)
		defer cancel()
	}

	w.logger.Info("live_status: running",
		"session_id", job.SessionID, "turn_id", rep.TurnID,
		"model", model, "reason", job.Reason, "span_count", len(spans))

	output, err := w.runner.Run(runCtx, model, prompt)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return
		}
		w.logger.Warn("live_status: runner error",
			"session_id", job.SessionID, "model", model, "err", err)
		return
	}

	text := cleanLiveStatusOutput(output)
	if text == "" {
		w.logger.Debug("live_status: empty output — skipping write", "session_id", job.SessionID)
		return
	}

	now := w.clock()
	if err := w.store.UpdateSessionLiveStatus(ctx, job.SessionID, text, model, now); err != nil {
		w.logger.Warn("live_status: persist", "session_id", job.SessionID, "err", err)
		return
	}

	w.logger.Info("live_status: written",
		"session_id", job.SessionID, "text", text, "model", model)

	// Broadcast session.updated so the Live dashboard gets the new
	// blurb via SSE without having to re-poll.
	if w.hub != nil {
		refreshed, err := w.store.GetSession(ctx, job.SessionID)
		if err == nil && refreshed != nil {
			w.hub.Broadcast(sse.NewSessionEvent(now, *refreshed))
		}
	}
}

// cleanLiveStatusOutput strips wrapping quotes / whitespace / stray
// markdown from the model's response and hard-caps the length.
func cleanLiveStatusOutput(raw string) string {
	out := strings.TrimSpace(raw)
	// Strip common wrapping quote pairs.
	for _, p := range []string{`"`, "`", "'"} {
		if strings.HasPrefix(out, p) && strings.HasSuffix(out, p) && len(out) >= 2*len(p) {
			out = strings.TrimSpace(out[len(p) : len(out)-len(p)])
		}
	}
	// Collapse newlines — the UI renders one line.
	out = strings.ReplaceAll(out, "\r", " ")
	out = strings.ReplaceAll(out, "\n", " ")
	for strings.Contains(out, "  ") {
		out = strings.ReplaceAll(out, "  ", " ")
	}
	out = strings.TrimSpace(out)
	if len(out) > liveStatusMaxOutputChars {
		out = strings.TrimSpace(out[:liveStatusMaxOutputChars])
		out = strings.TrimRight(out, ".,;:")
		out += "…"
	}
	return out
}

// buildLiveStatusPrompt assembles the minimal prompt the live-status
// worker feeds to Haiku. The prompt is intentionally tiny — one
// sentence in, one sentence out — because this fires on every span
// insert during a busy turn.
func buildLiveStatusPrompt(sess duckdb.Session, turn duckdb.Turn, spans []duckdb.SpanRow) string {
	var sb strings.Builder
	sb.WriteString("You are watching a Claude Code agent as it works. Answer in one short English sentence (max 120 characters) describing what the agent is CURRENTLY doing, as of the most recent span. Start with a gerund (\"Editing\", \"Running\", \"Searching\", \"Debugging\", etc.) or a short imperative. Do not add punctuation beyond a trailing period. Do not wrap the sentence in quotes or markdown. Do not explain.\n\n")

	sb.WriteString("# Session\n")
	fmt.Fprintf(&sb, "source_app: %s\n", sess.SourceApp)
	if sess.CurrentPhase != "" {
		fmt.Fprintf(&sb, "phase: %s\n", sess.CurrentPhase)
	}
	sb.WriteString("\n")

	sb.WriteString("# Current turn\n")
	if snippet := truncate(strings.TrimSpace(turn.PromptText), 240); snippet != "" {
		fmt.Fprintf(&sb, "user_prompt: %s\n", snippet)
	}
	if turn.Headline != "" {
		fmt.Fprintf(&sb, "headline: %s\n", turn.Headline)
	}
	fmt.Fprintf(&sb, "tool_call_count: %d\n", turn.ToolCallCount)
	fmt.Fprintf(&sb, "error_count: %d\n", turn.ErrorCount)
	sb.WriteString("\n")

	// Span table — newest last so the model naturally anchors on the
	// most recent line. We want the tail of the execution, not the head.
	recent := spans
	if len(recent) > liveStatusPromptMaxSpans {
		recent = recent[len(recent)-liveStatusPromptMaxSpans:]
	}
	if len(recent) > 0 {
		sb.WriteString("# Recent spans (oldest to newest)\n")
		for _, sp := range recent {
			label := liveStatusSpanLabel(sp)
			if label == "" {
				continue
			}
			status := strings.TrimSpace(string(sp.StatusCode))
			if status == "" || status == "UNSET" {
				status = "running"
			}
			fmt.Fprintf(&sb, "- %s [%s]\n", label, strings.ToLower(status))
		}
		sb.WriteString("\n")
	}

	sb.WriteString("Now answer with the one-sentence status.")
	return sb.String()
}

// liveStatusSpanLabel picks the most identifying label for a span:
// tool name when present, else hook event, else the raw span name.
func liveStatusSpanLabel(sp duckdb.SpanRow) string {
	if sp.ToolName != "" {
		if sp.MCPTool != "" && sp.MCPServer != "" {
			return fmt.Sprintf("%s (%s via %s)", sp.ToolName, sp.MCPTool, sp.MCPServer)
		}
		return sp.ToolName
	}
	if sp.HookEvent != "" {
		return "hook:" + sp.HookEvent
	}
	return sp.Name
}

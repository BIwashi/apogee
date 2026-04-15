package ingest

import (
	"context"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"log/slog"
	"strings"
	"sync"
	"time"

	"go.opentelemetry.io/otel/codes"
	oteltrace "go.opentelemetry.io/otel/trace"

	"github.com/BIwashi/apogee/internal/attention"
	"github.com/BIwashi/apogee/internal/otel"
	"github.com/BIwashi/apogee/internal/sse"
	"github.com/BIwashi/apogee/internal/store/duckdb"
)

// numShards is the number of per-session mutex buckets the reconstructor
// distributes Apply calls across. PR #37 introduces sharding so two
// unrelated sessions that happen to be arriving concurrently do not
// serialise behind a single global mutex. 16 is a conservative default —
// enough parallelism to make a difference on tool-heavy workloads, not so
// many buckets that the memory overhead of an unused shard matters.
const numShards = 16

// reconstructorShard is one bucket of the reconstructor's sharded lock. The
// mu guards mutations to the session_state map entries that hash to this
// shard. The higher-level Reconstructor.sessionsMu still protects the
// sessions map itself so lookups / inserts / deletes can atomically swap
// entries without blocking per-session work happening elsewhere.
type reconstructorShard struct {
	mu sync.Mutex
}

// InterventionsObserver is the subset of the interventions service the
// reconstructor uses. Kept as an interface so the ingest package does not
// have to import internal/interventions (which would create a cycle if the
// service later needed to reach into the reconstructor).
type InterventionsObserver interface {
	ExpireForTurn(ctx context.Context, turnID string) error
	ExpireForSession(ctx context.Context, sessionID string) error
	ObservePostHookConsumption(ctx context.Context, sessionID, hookEvent string, logID int64)
}

func jsonUnmarshal(b []byte, v any) error { return json.Unmarshal(b, v) }

// agentFrame is one entry on a per-agent span stack.
type agentFrame struct {
	Span      *otel.Span
	StartedAt time.Time
	// OTel-side mirror of this span. nil when no tracer is wired or when
	// the OTel span could not be created. Children of this frame use
	// OTelCtx as the parent context for Tracer.Start so the exported
	// trace tree matches the apogee internal tree.
	OTelSpan oteltrace.Span
	OTelCtx  context.Context
}

const mainAgentKey = "main"

// sessionState is the per-session in-memory bookkeeping the reconstructor
// holds between events. It is small by design; all durable state lives in the
// store.
type sessionState struct {
	SourceApp     string
	Model         string
	StartedAt     time.Time
	LastSeen      time.Time
	SessionInDB   bool

	// Active turn (zero values when between turns).
	TraceID       otel.TraceID
	TurnID        string
	TurnRoot      *otel.Span
	TurnStartedAt time.Time
	PromptText    string

	// TurnRootOTel is the OTel-side mirror of TurnRoot. nil when the
	// reconstructor has no tracer wired. closeTurn finishes this span
	// after the apogee row has been written. Tracked outside of Stacks
	// because closeTurn drains Stacks before the root is closed.
	TurnRootOTel oteltrace.Span
	// TurnRootOTelCtx is the context returned by Tracer.Start for the
	// turn root. Used by EmitRecapEnrichment so the post-hoc recap
	// span carries a parent link to the right trace.
	TurnRootOTelCtx context.Context

	Stacks       map[string][]*agentFrame
	PendingTools map[string]*otel.Span // tool_use_id -> open tool span
	// PendingHITL is the set of open HITL spans for this turn keyed by
	// span_id. Tracked separately from Stacks so HITL requests do not
	// hijack the parent frame for subsequent tool calls.
	PendingHITL map[string]*otel.Span
	// OTel mirror tracking. ToolOTelSpans/HITLOTelSpans hold the OTel
	// span handle keyed the same way as PendingTools/PendingHITL so the
	// Post* / response paths can call End on the right one. Nil when
	// the reconstructor's tracer is nil.
	ToolOTelSpans map[string]oteltrace.Span
	HITLOTelSpans map[string]oteltrace.Span

	ToolCallCount int
	SubagentCount int
	ErrorCount    int
}

func (st *sessionState) hasActiveTurn() bool {
	return st.TurnRoot != nil
}

// attentionDebounce is the minimum interval between attention re-scores of
// the same turn. It prevents the SSE fan-out from storming when a busy turn
// mutates dozens of times per second.
const attentionDebounce = 250 * time.Millisecond

// Reconstructor turns a stream of HookEvent values into spans, logs, and
// session/turn rows. It is safe for concurrent use; callers may invoke Apply
// from many goroutines.
//
// Locking model (PR #37):
//   - sessionsMu protects the sessions map itself (get/put/delete). It is
//     held for the shortest possible window — usually a single map lookup
//     or insertion — so unrelated sessions never serialise against each
//     other here.
//   - shards[hash(session_id)%numShards].mu protects all per-session
//     state mutation: Apply, CloseHITLSpan, and the turn-counter debouncer
//     all acquire the relevant shard. Independent sessions fall into
//     different shards, so up to numShards concurrent ingest goroutines
//     can run in parallel.
//   - lastScoredAtMu guards the rescoreAttention debounce map, which is
//     shared across shards and updated from every Apply call.
type Reconstructor struct {
	sessionsMu sync.RWMutex
	sessions   map[string]*sessionState
	shards     [numShards]reconstructorShard

	store  *duckdb.Store
	clock  func() time.Time
	logger *slog.Logger
	// Hub, when non-nil, receives a broadcast for every session/turn/span
	// mutation once the underlying DuckDB write has succeeded. The hub must
	// never block — see internal/sse for the back-pressure policy.
	Hub *sse.Hub

	// Attention engine wiring. Engine may be nil, in which case the
	// reconstructor skips re-scoring entirely (useful for tests that don't
	// care). lastScoredAt debounces per-turn re-scores to at most once every
	// attentionDebounce interval.
	Engine         *attention.Engine
	HistoryWrite   attention.HistoryWriter
	lastScoredAtMu sync.Mutex
	lastScoredAt   map[string]time.Time

	// turnCounters is the debouncer for touchTurnCounters. Tool-heavy
	// turns can fire 50+ counter updates in a second; before PR #37 every
	// one of those turned into a DuckDB UPDATE and an SSE broadcast. The
	// debouncer coalesces them to at most one flush per 250 ms per turn.
	turnCounters *turnCounterDebouncer

	// OnTurnClosed, when non-nil, is invoked once a turn row has been
	// fully updated by closeTurn. It receives the terminal turn id and
	// is called without the reconstructor lock held so callbacks that
	// enqueue follow-up work (the LLM summariser, for example) do not
	// back-pressure the ingest hot path. Callbacks must not block.
	OnTurnClosed func(turnID string)

	// OnSessionEnded, when non-nil, fires once the SessionEnd hook has been
	// applied and the sessions row marked ended. Used by the rollup worker
	// to enqueue a final per-session digest. Must not block.
	OnSessionEnded func(sessionID string)

	// OnHITLRequested, when non-nil, is invoked after the reconstructor
	// inserts a fresh hitl_events row for an inbound permission request.
	// Wire this to the hitl.Service so the SSE hub broadcasts a
	// hitl.requested event without the reconstructor depending on the
	// service package directly.
	OnHITLRequested func(ev duckdb.HITLEvent)

	// Tracer, when non-nil, mirrors every reconstructor span to an
	// OpenTelemetry trace via the OTel SDK. Errors on the OTel side are
	// logged at WARN and never propagate back to the caller. See
	// otelmirror.go for the helpers that own this side of the
	// reconstructor.
	Tracer oteltrace.Tracer

	// InterventionsSvc, when non-nil, observes every inbound hook event
	// for downstream consumption of operator interventions and receives
	// turn/session-close notifications so pending rows can be expired.
	InterventionsSvc InterventionsObserver
}

// NewReconstructor returns a Reconstructor backed by the given store. logger
// may be nil (a discard logger is installed). clock is the wall-clock source
// for synthetic events; pass nil to use time.Now.
func NewReconstructor(store *duckdb.Store, logger *slog.Logger, clock func() time.Time) *Reconstructor {
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(discardWriter{}, &slog.HandlerOptions{Level: slog.LevelError}))
	}
	if clock == nil {
		clock = time.Now
	}
	r := &Reconstructor{
		sessions:     make(map[string]*sessionState),
		store:        store,
		logger:       logger,
		clock:        clock,
		lastScoredAt: make(map[string]time.Time),
	}
	r.turnCounters = newTurnCounterDebouncer(r.flushTurnCounters, turnCounterDebounce)
	return r
}

// shardFor returns the per-session mutex bucket for a session id. The
// hash function is FNV-1a 32-bit — fast, good enough distribution for
// UUIDv4 session ids, and zero allocations in the hot path.
func (r *Reconstructor) shardFor(sessionID string) *reconstructorShard {
	h := fnv.New32a()
	_, _ = h.Write([]byte(sessionID))
	return &r.shards[h.Sum32()%uint32(numShards)]
}

// lockSessionForApply takes the sessions-map read lock, resolves-or-creates
// the session state, and returns the locked shard along with the state.
// Callers must Unlock the shard when done. This is the canonical entry
// point for goroutines that want to mutate per-session state without
// contending against the sessions map for the duration of the update.
func (r *Reconstructor) lockSessionForApply(ev *HookEvent) (*reconstructorShard, *sessionState) {
	// Fast path: shared lock, session already exists.
	r.sessionsMu.RLock()
	st, ok := r.sessions[ev.SessionID]
	r.sessionsMu.RUnlock()
	if !ok {
		// Slow path: exclusive insert. We re-check because another
		// goroutine may have inserted the same session while we were
		// waiting for the write lock.
		r.sessionsMu.Lock()
		st, ok = r.sessions[ev.SessionID]
		if !ok {
			st = &sessionState{
				SourceApp:     ev.SourceApp,
				StartedAt:     ev.Time(),
				LastSeen:      ev.Time(),
				Stacks:        map[string][]*agentFrame{},
				PendingTools:  map[string]*otel.Span{},
				PendingHITL:   map[string]*otel.Span{},
				ToolOTelSpans: map[string]oteltrace.Span{},
				HITLOTelSpans: map[string]oteltrace.Span{},
			}
			r.sessions[ev.SessionID] = st
		}
		r.sessionsMu.Unlock()
	}
	shard := r.shardFor(ev.SessionID)
	shard.mu.Lock()
	// Per-session field refresh (previously done inside
	// getOrCreateSession) is applied under the shard lock so it races
	// against no other Apply on the same session.
	if ev.SourceApp != "" {
		st.SourceApp = ev.SourceApp
	}
	if ev.ModelName != "" {
		st.Model = ev.ModelName
	}
	return shard, st
}

type discardWriter struct{}

func (discardWriter) Write(p []byte) (int, error) { return len(p), nil }

// Apply ingests a single hook event, updating in-memory state and writing to
// the store. Safe for concurrent use; Apply holds only the per-session
// shard mutex so two events for different sessions can be ingested in
// parallel.
func (r *Reconstructor) Apply(ctx context.Context, ev *HookEvent) error {
	if err := ev.Validate(); err != nil {
		return err
	}
	shard, st := r.lockSessionForApply(ev)
	defer shard.mu.Unlock()
	st.LastSeen = ev.Time()

	// Always write the raw log row first so the audit trail is lossless even
	// when the rest of the pipeline drops the event.
	if err := r.writeLog(ctx, st, ev); err != nil {
		r.logger.Error("write log", "err", err, "event", ev.HookEventType)
	}

	// Capture the turn id this event pertains to before the handler runs,
	// so we can re-score the right row even when the handler itself closes
	// the turn (handleStop).
	preTurnID := st.TurnID

	var err error
	switch ev.HookEventType {
	case HookSessionStart:
		err = r.handleSessionStart(ctx, st, ev)
	case HookSessionEnd:
		err = r.handleSessionEnd(ctx, st, ev)
	case HookUserPromptSubmit:
		err = r.handleUserPromptSubmit(ctx, st, ev)
	case HookPreToolUse:
		err = r.handlePreToolUse(ctx, st, ev)
	case HookPostToolUse:
		err = r.handlePostToolUse(ctx, st, ev, false)
	case HookPostToolUseFail:
		err = r.handlePostToolUse(ctx, st, ev, true)
	case HookPermissionRequest:
		err = r.handlePermissionRequest(ctx, st, ev)
	case HookNotification:
		err = r.handleNotification(ctx, st, ev)
	case HookSubagentStart:
		err = r.handleSubagentStart(ctx, st, ev)
	case HookSubagentStop:
		err = r.handleSubagentStop(ctx, st, ev)
	case HookPreCompact:
		err = r.handlePreCompact(ctx, st, ev)
	case HookStop:
		err = r.handleStop(ctx, st, ev)
	default:
		// Unknown hook event: log it and add a span event on the turn root if
		// one is open. The log row is already persisted above.
		r.logger.Warn("unknown hook event", "type", ev.HookEventType, "session", ev.SessionID)
		if st.hasActiveTurn() {
			r.appendSpanEvent(ctx, st.TurnRoot, otel.SpanEvent{
				Name:       "claude_code.unknown_hook",
				Time:       ev.Time(),
				Attributes: map[string]any{"hook_event_type": ev.HookEventType},
			})
		}
	}

	// Re-score the affected turn once the handler is done. If the event
	// closed or didn't start a turn we fall back to the pre-handler turn id
	// so the dashboard still gets a broadcast.
	r.rescoreAttention(ctx, chooseTurnID(preTurnID, st.TurnID), ev.Time())

	// Observation pass for operator interventions: if any intervention was
	// previously delivered on this session, the current downstream hook is
	// a plausible proxy that Claude Code has now processed the block/
	// additionalContext. The service flips it to consumed. The log id is
	// not threaded through InsertLog, so we pass 0; consumption records
	// it as NULL which the brief tolerates as a best-effort observation.
	if r.InterventionsSvc != nil {
		r.InterventionsSvc.ObservePostHookConsumption(ctx, ev.SessionID, ev.HookEventType, 0)
	}

	return err
}

// chooseTurnID picks the turn id to rescore. Prefer the pre-handler id
// because if the handler just closed a turn (Stop) that's still the right
// row; otherwise fall through to whatever the session is currently working
// on.
func chooseTurnID(pre, post string) string {
	if pre != "" {
		return pre
	}
	return post
}

// rescoreAttention computes the engine's classification for the given turn
// and writes it back to the row, then broadcasts a turn.updated SSE event.
// Debounced per-turn at attentionDebounce so busy turns do not storm the
// hub.
func (r *Reconstructor) rescoreAttention(ctx context.Context, turnID string, evTime time.Time) {
	if r.Engine == nil || turnID == "" || r.store == nil {
		return
	}
	now := r.clock()
	r.lastScoredAtMu.Lock()
	if last, ok := r.lastScoredAt[turnID]; ok && now.Sub(last) < attentionDebounce {
		r.lastScoredAtMu.Unlock()
		return
	}
	r.lastScoredAt[turnID] = now
	r.lastScoredAtMu.Unlock()

	turn, err := r.store.GetTurn(ctx, turnID)
	if err != nil || turn == nil {
		if err != nil {
			r.logger.Debug("rescore: load turn", "err", err)
		}
		return
	}
	spans, err := r.store.GetSpansByTurn(ctx, turnID)
	if err != nil {
		r.logger.Debug("rescore: load spans", "err", err)
		return
	}
	hitlRows, err := r.store.ListHITLByTurn(ctx, turnID)
	if err != nil {
		r.logger.Debug("rescore: load hitl", "err", err)
	}
	interventions, err := r.store.ListPendingInterventionsByTurn(ctx, turnID)
	if err != nil {
		r.logger.Debug("rescore: load interventions", "err", err)
	}

	decision := r.Engine.Score(attention.Input{
		Turn:          *turn,
		Spans:         spans,
		HITL:          hitlRows,
		Interventions: interventions,
		Now:           now,
	})

	var confidence float64
	var since time.Time
	confidence = decision.Phase.Confidence
	since = decision.Phase.Since
	if since.IsZero() {
		since = turn.StartedAt
	}
	score := decision.Score
	signalsJSON := ""
	if len(decision.Signals) > 0 {
		if b, err := json.Marshal(decision.Signals); err == nil {
			signalsJSON = string(b)
		}
	}
	if err := r.store.UpdateTurnAttention(ctx,
		turnID,
		decision.State.String(),
		decision.Reason,
		decision.Tone,
		score,
		string(decision.Phase.Name),
		confidence,
		since,
		signalsJSON,
	); err != nil {
		r.logger.Debug("rescore: update turn", "err", err)
		return
	}

	// If the turn just ended, record the pattern outcome in the history.
	if r.HistoryWrite != nil && turn.Status != "running" && turn.Status != "" {
		pattern := attention.ToolNamesForPattern(spans)
		if pattern != "" {
			success := turn.Status == "completed"
			_ = r.HistoryWrite.Upsert(pattern, attention.Outcome{
				Success: success,
				TurnID:  turnID,
			})
		}
	}

	r.broadcastTurn(ctx, turnID, sse.EventTypeTurnUpdated)
}

// getOrCreateSession has been replaced by lockSessionForApply. The former
// required the global r.mu lock for the entire duration of an Apply call;
// the new helper only takes the sessions map lock for the lookup/insert
// and then switches to the per-session shard mutex so unrelated sessions
// can run in parallel.

func (r *Reconstructor) handleSessionStart(ctx context.Context, st *sessionState, ev *HookEvent) error {
	st.StartedAt = ev.Time()
	if err := r.upsertSession(ctx, st, ev); err != nil {
		return err
	}
	return nil
}

func (r *Reconstructor) handleSessionEnd(ctx context.Context, st *sessionState, ev *HookEvent) error {
	if err := r.store.MarkSessionEnded(ctx, ev.SessionID, ev.Time()); err != nil {
		return err
	}
	// Expire any pending operator interventions queued against this
	// session before we forget the in-memory state.
	if r.InterventionsSvc != nil {
		if err := r.InterventionsSvc.ExpireForSession(ctx, ev.SessionID); err != nil {
			r.logger.Debug("interventions: expire for session", "err", err)
		}
	}
	r.broadcastSession(ctx, ev.SessionID)
	// Drop the in-memory session. The shard mutex is still held by the
	// caller of Apply so no other goroutine can observe the session in a
	// half-dismantled state; the sessions-map write lock only blocks
	// lookups, never per-session work.
	r.sessionsMu.Lock()
	delete(r.sessions, ev.SessionID)
	r.sessionsMu.Unlock()
	// Also flush any pending turn-counter write for this session so the
	// debouncer does not fire against a deleted turn.
	if r.turnCounters != nil {
		r.turnCounters.cancelSession(ev.SessionID)
	}
	if r.OnSessionEnded != nil {
		// Invoke without the reconstructor lock held — callbacks enqueue
		// follow-up work and must never back-pressure ingest.
		go r.OnSessionEnded(ev.SessionID)
	}
	return nil
}

func (r *Reconstructor) handleUserPromptSubmit(ctx context.Context, st *sessionState, ev *HookEvent) error {
	// Make sure we have a sessions row even if SessionStart was missed.
	if err := r.upsertSession(ctx, st, ev); err != nil {
		return err
	}

	// If a previous turn is still active, close it as 'stopped' first.
	if st.hasActiveTurn() {
		r.closeTurn(ctx, st, ev.Time(), "stopped")
	}

	traceID := otel.NewTraceID()
	turnID := otel.NewTurnID()
	rootSpanID := otel.NewSpanID()

	prompt := ev.Prompt
	if prompt == "" {
		prompt = pluckString(ev.Payload, "prompt")
	}

	root := &otel.Span{
		TraceID:     traceID,
		SpanID:      rootSpanID,
		Name:        "claude_code.turn",
		Kind:        otel.SpanKindInternal,
		StartTime:   ev.Time(),
		StatusCode:  otel.StatusUnset,
		ServiceName: "claude-code",
		SessionID:   ev.SessionID,
		TurnID:      turnID,
		AgentID:     "main",
		AgentKind:   "main",
		HookEvent:   ev.HookEventType,
		Attributes: map[string]any{
			"claude_code.session.id":  ev.SessionID,
			"claude_code.source_app":  ev.SourceApp,
			"claude_code.turn.id":     turnID,
			"service.name":            "claude-code",
		},
	}
	if ev.ModelName != "" {
		root.Attributes["gen_ai.system"] = "anthropic"
		root.Attributes["gen_ai.request.model"] = ev.ModelName
	}
	if prompt != "" {
		root.Events = append(root.Events, otel.SpanEvent{
			Name:       "claude_code.prompt",
			Time:       ev.Time(),
			Attributes: map[string]any{"text": prompt, "chars": len(prompt)},
		})
	}

	// Open the OTel-side mirror first so the apogee Span TraceID/SpanID
	// reflect the OTel-generated values. We then persist with the SDK
	// ids so DuckDB rows and exported spans share the same ids — no
	// drift between the two side channels.
	rootCtx, rootOTel := r.startOTelSpan(ctx, root, oteltrace.SpanKindServer)

	if err := r.store.InsertSpan(ctx, root); err != nil {
		if rootOTel != nil {
			rootOTel.SetStatus(codes.Error, "insert turn root: "+err.Error())
			rootOTel.End()
		}
		return err
	}
	r.broadcastSpan(sse.EventTypeSpanInserted, root)

	// When the OTel mirror is enabled, root.TraceID/SpanID were
	// rewritten to the SDK-generated values inside startOTelSpan, so
	// the persisted ids match the exported trace. The apogee turn id
	// (UUIDv7) is dashboard-facing and stays unchanged.
	st.TraceID = root.TraceID
	st.TurnID = root.TurnID
	st.TurnRoot = root
	st.TurnStartedAt = ev.Time()
	st.PromptText = prompt
	st.ToolCallCount = 0
	st.SubagentCount = 0
	st.ErrorCount = 0
	st.Stacks = map[string][]*agentFrame{
		mainAgentKey: {{
			Span:      root,
			StartedAt: ev.Time(),
			OTelSpan:  rootOTel,
			OTelCtx:   rootCtx,
		}},
	}
	st.TurnRootOTel = rootOTel
	st.TurnRootOTelCtx = rootCtx
	st.PendingTools = map[string]*otel.Span{}
	st.PendingHITL = map[string]*otel.Span{}
	st.ToolOTelSpans = map[string]oteltrace.Span{}
	st.HITLOTelSpans = map[string]oteltrace.Span{}

	turn := duckdb.Turn{
		TurnID:     turnID,
		TraceID:    string(traceID),
		SessionID:  ev.SessionID,
		SourceApp:  ev.SourceApp,
		StartedAt:  ev.Time(),
		Status:     "running",
		Model:      ev.ModelName,
		PromptText: prompt,
	}
	if prompt != "" {
		c := len(prompt)
		turn.PromptChars = &c
	}
	if err := r.store.InsertTurn(ctx, turn); err != nil {
		return err
	}
	if err := r.store.IncrementSessionTurnCount(ctx, ev.SessionID); err != nil {
		return err
	}
	r.broadcastTurn(ctx, turnID, sse.EventTypeTurnStarted)
	r.broadcastSession(ctx, ev.SessionID)
	return nil
}

func (r *Reconstructor) handlePreToolUse(ctx context.Context, st *sessionState, ev *HookEvent) error {
	// Defensive: open a synthetic turn so the tool call has somewhere to live.
	if !st.hasActiveTurn() {
		r.logger.Warn("PreToolUse without active turn — synthesising", "session", ev.SessionID)
		synth := *ev
		synth.HookEventType = HookUserPromptSubmit
		synth.Prompt = ""
		if err := r.handleUserPromptSubmit(ctx, st, &synth); err != nil {
			return err
		}
	}

	parent := r.parentFrame(st, ev.AgentID)
	if parent == nil {
		// Should not happen because we just synthesised a turn.
		return fmt.Errorf("PreToolUse: no parent frame")
	}

	toolName := ev.ToolName
	spanName := "claude_code.tool"
	if toolName != "" {
		spanName = "claude_code.tool." + toolName
	}

	mcpServer, mcpTool := parseMCPName(toolName)
	if mcpServer != "" {
		spanName = fmt.Sprintf("claude_code.tool.mcp.%s.%s", mcpServer, mcpTool)
	}

	span := &otel.Span{
		TraceID:      st.TraceID,
		SpanID:       otel.NewSpanID(),
		ParentSpanID: parent.Span.SpanID,
		Name:         spanName,
		Kind:         otel.SpanKindInternal,
		StartTime:    ev.Time(),
		StatusCode:   otel.StatusUnset,
		ServiceName:  "claude-code",
		SessionID:    ev.SessionID,
		TurnID:       st.TurnID,
		AgentID:      coalesce(ev.AgentID, "main"),
		AgentKind:    parent.Span.AgentKind,
		ToolName:     toolName,
		ToolUseID:    ev.ToolUseID,
		MCPServer:    mcpServer,
		MCPTool:      mcpTool,
		HookEvent:    ev.HookEventType,
		Attributes: map[string]any{
			"claude_code.session.id":   ev.SessionID,
			"claude_code.turn.id":      st.TurnID,
			"claude_code.tool.name":    toolName,
			"claude_code.tool.use_id":  ev.ToolUseID,
		},
	}
	if mcpServer != "" {
		span.Attributes["claude_code.mcp.server"] = mcpServer
		span.Attributes["claude_code.mcp.tool"] = mcpTool
	}
	if len(ev.Payload) > 0 {
		span.Attributes["claude_code.tool.input"] = string(ev.Payload)
	}

	// Open the OTel-side mirror first so the apogee Span TraceID/SpanID
	// reflect the SDK-generated values before the row is persisted.
	parentCtx := ctx
	if parent != nil && parent.OTelCtx != nil {
		parentCtx = parent.OTelCtx
	}
	toolCtx, toolOTel := r.startOTelSpan(parentCtx, span, oteltrace.SpanKindInternal)

	if err := r.store.InsertSpan(ctx, span); err != nil {
		if toolOTel != nil {
			toolOTel.SetStatus(codes.Error, "insert tool span: "+err.Error())
			toolOTel.End()
		}
		return err
	}
	r.broadcastSpan(sse.EventTypeSpanInserted, span)

	stackKey := stackKeyFor(ev.AgentID)
	st.Stacks[stackKey] = append(st.Stacks[stackKey], &agentFrame{
		Span:      span,
		StartedAt: ev.Time(),
		OTelSpan:  toolOTel,
		OTelCtx:   toolCtx,
	})
	if ev.ToolUseID != "" {
		st.PendingTools[ev.ToolUseID] = span
		if toolOTel != nil {
			st.ToolOTelSpans[ev.ToolUseID] = toolOTel
		}
	}
	st.ToolCallCount++
	r.touchTurnCounters(ctx, st)
	return nil
}

func (r *Reconstructor) handlePostToolUse(ctx context.Context, st *sessionState, ev *HookEvent, failure bool) error {
	if !st.hasActiveTurn() || ev.ToolUseID == "" {
		// No active turn, or no tool_use_id to match against. Log row is
		// already written; nothing more to do.
		return nil
	}
	span, ok := st.PendingTools[ev.ToolUseID]
	if !ok {
		r.logger.Warn("PostToolUse with unknown tool_use_id",
			"tool_use_id", ev.ToolUseID, "session", ev.SessionID)
		return nil
	}
	end := ev.Time()
	span.EndTime = &end
	if failure {
		span.StatusCode = otel.StatusError
		if ev.Error != "" {
			span.StatusMessage = ev.Error
		}
		span.Events = append(span.Events, otel.SpanEvent{
			Name:       "exception",
			Time:       end,
			Attributes: map[string]any{"exception.message": ev.Error},
		})
		st.ErrorCount++
	} else {
		span.StatusCode = otel.StatusOK
	}
	if len(ev.Payload) > 0 {
		span.Attributes["claude_code.tool.output"] = string(ev.Payload)
	}
	if err := r.store.UpdateSpan(ctx, span); err != nil {
		return err
	}
	r.broadcastSpan(sse.EventTypeSpanUpdated, span)
	if otSpan, ok := st.ToolOTelSpans[ev.ToolUseID]; ok {
		r.finishOTelSpan(otSpan, span)
		delete(st.ToolOTelSpans, ev.ToolUseID)
	}
	r.popFrameBySpan(st, span)
	delete(st.PendingTools, ev.ToolUseID)
	r.touchTurnCounters(ctx, st)
	return nil
}

func (r *Reconstructor) handlePermissionRequest(ctx context.Context, st *sessionState, ev *HookEvent) error {
	if !st.hasActiveTurn() {
		return nil
	}
	parent := r.parentFrame(st, ev.AgentID)
	if parent == nil {
		return nil
	}

	hitlID := otel.NewHITLID()
	question := pluckHITLQuestion(ev)
	hitlContext := buildHITLContext(ev)
	contextJSON := encodeJSONString(hitlContext)
	suggestionsJSON := encodeJSONString(ev.PermissionSuggestions)

	span := &otel.Span{
		TraceID:      st.TraceID,
		SpanID:       otel.NewSpanID(),
		ParentSpanID: parent.Span.SpanID,
		Name:         "claude_code.hitl.permission",
		Kind:         otel.SpanKindInternal,
		StartTime:    ev.Time(),
		StatusCode:   otel.StatusUnset,
		ServiceName:  "claude-code",
		SessionID:    ev.SessionID,
		TurnID:       st.TurnID,
		AgentID:      coalesce(ev.AgentID, "main"),
		HookEvent:    ev.HookEventType,
		Attributes: map[string]any{
			"claude_code.hitl.id":          hitlID,
			"claude_code.hitl.kind":        "permission",
			"claude_code.hitl.suggestions": ev.PermissionSuggestions,
			"claude_code.tool.name":        ev.ToolName,
		},
	}
	parentCtx := ctx
	if parent != nil && parent.OTelCtx != nil {
		parentCtx = parent.OTelCtx
	}
	hitlCtx, hitlOTel := r.startOTelSpan(parentCtx, span, oteltrace.SpanKindInternal)
	_ = hitlCtx
	if err := r.store.InsertSpan(ctx, span); err != nil {
		if hitlOTel != nil {
			hitlOTel.SetStatus(codes.Error, "insert hitl span: "+err.Error())
			hitlOTel.End()
		}
		return err
	}
	// Track this open HITL span in a dedicated map so closeTurn can find
	// it and so RespondHITL can close it via SpanID. We deliberately do
	// not push HITL spans onto the agent stack — doing so would re-parent
	// subsequent tool calls to the HITL request.
	if st.PendingHITL == nil {
		st.PendingHITL = map[string]*otel.Span{}
	}
	st.PendingHITL[string(span.SpanID)] = span
	if hitlOTel != nil {
		if st.HITLOTelSpans == nil {
			st.HITLOTelSpans = map[string]oteltrace.Span{}
		}
		st.HITLOTelSpans[string(span.SpanID)] = hitlOTel
	}

	r.broadcastSpan(sse.EventTypeSpanInserted, span)

	hitlEv := duckdb.HITLEvent{
		HitlID:          hitlID,
		SpanID:          string(span.SpanID),
		TraceID:         string(st.TraceID),
		SessionID:       ev.SessionID,
		TurnID:          st.TurnID,
		Kind:            "permission",
		Status:          duckdb.HITLStatusPending,
		RequestedAt:     ev.Time(),
		Question:        question,
		SuggestionsJSON: suggestionsJSON,
		ContextJSON:     contextJSON,
	}
	if err := r.store.InsertHITL(ctx, hitlEv); err != nil {
		r.logger.Error("insert hitl", "err", err, "hitl_id", hitlID)
		return nil
	}
	if r.OnHITLRequested != nil {
		// Re-fetch so the broadcast carries the row id assigned by the DB
		// sequence. Failure here is non-fatal — degrade to the in-memory
		// shape we already have.
		stored, ok, err := r.store.GetHITL(ctx, hitlID)
		if err == nil && ok {
			r.OnHITLRequested(stored)
		} else {
			r.OnHITLRequested(hitlEv)
		}
	}
	return nil
}

// CloseHITLSpan finalises an open HITL span when its corresponding row has
// transitioned to responded/expired. It stamps a status code derived from
// the decision and records the response details as span attributes so
// downstream consumers (span tree, OTel export) see the resolution.
//
// Safe to call from the HTTP handler goroutine — it acquires the
// reconstructor shard mutex for the owning session internally.
func (r *Reconstructor) CloseHITLSpan(ctx context.Context, ev duckdb.HITLEvent) {
	if r == nil || r.store == nil {
		return
	}
	// Lock the shard owning this session. When the session id is missing
	// (HITL row persisted before SessionStart, unusual but possible) we
	// fall back to sharding on the hitl id itself so concurrent calls do
	// not stomp on each other.
	shardKey := ev.SessionID
	if shardKey == "" {
		shardKey = ev.HitlID
	}
	shard := r.shardFor(shardKey)
	shard.mu.Lock()
	defer shard.mu.Unlock()

	// The HITL span lives under a session/turn we may not be actively
	// holding state for any more. Locate it via the stored span id and
	// load its current state from the DB so we don't lose attributes.
	spans, err := r.store.GetSpansByTurn(ctx, ev.TurnID)
	if err != nil {
		r.logger.Debug("close hitl span: load", "err", err)
		return
	}
	var found *duckdb.SpanRow
	for i := range spans {
		if spans[i].SpanID == ev.SpanID {
			found = &spans[i]
			break
		}
	}
	if found == nil {
		return
	}
	if found.EndTime != nil {
		// Already closed (turn ended first). Still update attributes via
		// in-memory state if we have it; for now, skip.
		return
	}
	end := time.Time{}
	if ev.RespondedAt.Valid {
		end = ev.RespondedAt.Time
	} else {
		end = r.clock()
	}
	statusCode := otel.StatusOK
	statusMessage := ""
	if ev.Decision.Valid {
		switch ev.Decision.String {
		case "deny":
			statusCode = otel.StatusError
			statusMessage = "denied"
		case "timeout":
			statusCode = otel.StatusError
			statusMessage = "timeout"
		}
	}
	if ev.Status == duckdb.HITLStatusExpired {
		statusCode = otel.StatusError
		statusMessage = "expired"
	}

	// Build a synthetic otel.Span carrying just the columns UpdateSpan
	// touches, plus a merged attribute bag.
	attrs := map[string]any{}
	if found.Attributes != nil {
		for k, v := range found.Attributes {
			attrs[k] = v
		}
	}
	attrs["claude_code.hitl.status"] = ev.Status
	if ev.Decision.Valid {
		attrs["claude_code.hitl.decision"] = ev.Decision.String
	}
	if ev.ReasonCategory.Valid {
		attrs["claude_code.hitl.reason_category"] = ev.ReasonCategory.String
	}
	if ev.OperatorNote.Valid {
		attrs["claude_code.hitl.operator_note"] = ev.OperatorNote.String
	}
	if ev.ResumeMode.Valid {
		attrs["claude_code.hitl.resume_mode"] = ev.ResumeMode.String
	}

	sp := &otel.Span{
		TraceID:       otel.TraceID(found.TraceID),
		SpanID:        otel.SpanID(found.SpanID),
		StartTime:     found.StartTime,
		EndTime:       &end,
		StatusCode:    statusCode,
		StatusMessage: statusMessage,
		Attributes:    attrs,
		Events:        spanEventsFromAny(found.Events),
	}
	if err := r.store.UpdateSpan(ctx, sp); err != nil {
		r.logger.Debug("close hitl span: update", "err", err)
		return
	}
	r.broadcastSpan(sse.EventTypeSpanUpdated, sp)
	// Drop the in-memory pending entry so the next closeTurn does not
	// re-process this span. Also finish the OTel mirror if one is held.
	// Under sharding we already know the session id the HITL event
	// belongs to, so we look up the session directly instead of ranging
	// over r.sessions (which would require crossing shard boundaries).
	if ev.SessionID != "" {
		r.sessionsMu.RLock()
		st := r.sessions[ev.SessionID]
		r.sessionsMu.RUnlock()
		if st != nil {
			if _, ok := st.PendingHITL[ev.SpanID]; ok {
				delete(st.PendingHITL, ev.SpanID)
			}
			if otSpan, ok := st.HITLOTelSpans[ev.SpanID]; ok {
				r.finishOTelSpan(otSpan, sp)
				delete(st.HITLOTelSpans, ev.SpanID)
			}
		}
	}
	// Also nudge the turn row so the dashboard re-renders.
	r.broadcastTurn(ctx, ev.TurnID, sse.EventTypeTurnUpdated)
}

// spanEventsFromAny converts the loose []any shape we get back from the
// store into the typed otel.SpanEvent slice that UpdateSpan re-serialises.
// Best-effort: events that fail to unmarshal are dropped.
func spanEventsFromAny(in []any) []otel.SpanEvent {
	if len(in) == 0 {
		return nil
	}
	out := make([]otel.SpanEvent, 0, len(in))
	for _, raw := range in {
		obj, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		ev := otel.SpanEvent{}
		if name, ok := obj["name"].(string); ok {
			ev.Name = name
		}
		if attrs, ok := obj["attributes"].(map[string]any); ok {
			ev.Attributes = attrs
		}
		if ts, ok := obj["time"].(string); ok {
			if t, err := time.Parse(time.RFC3339Nano, ts); err == nil {
				ev.Time = t
			}
		}
		out = append(out, ev)
	}
	return out
}

// pluckHITLQuestion extracts a natural-language question from a hook
// event payload. Several payload shapes carry this field under different
// keys; we probe in priority order.
func pluckHITLQuestion(ev *HookEvent) string {
	if ev == nil {
		return ""
	}
	for _, key := range []string{"question", "message", "prompt", "summary"} {
		if v := pluckString(ev.Payload, key); v != "" {
			return v
		}
	}
	if ev.Summary != "" {
		return ev.Summary
	}
	if ev.Reason != "" {
		return ev.Reason
	}
	return "Permission requested for " + ev.ToolName
}

// buildHITLContext sniffs the payload for the typed context fields the
// dashboard renders alongside a pending HITL row.
func buildHITLContext(ev *HookEvent) map[string]any {
	out := map[string]any{}
	if ev == nil {
		return out
	}
	if ev.ToolName != "" {
		out["tool_name"] = ev.ToolName
	}
	if len(ev.Payload) > 0 {
		var obj map[string]any
		if err := jsonUnmarshal(ev.Payload, &obj); err == nil {
			if input, ok := obj["tool_input"]; ok {
				out["tool_input_summary"] = summariseToolInput(input)
				if ev.ToolName == "Bash" {
					if asMap, ok := input.(map[string]any); ok {
						if cmd, ok := asMap["command"].(string); ok && cmd != "" {
							out["command_preview"] = truncate(cmd, 240)
						}
					}
				}
			}
			if file, ok := obj["target_file"].(string); ok && file != "" {
				out["target_file"] = file
			} else if file, ok := obj["file_path"].(string); ok && file != "" {
				out["target_file"] = file
			} else if input, ok := obj["tool_input"].(map[string]any); ok {
				if file, ok := input["file_path"].(string); ok && file != "" {
					out["target_file"] = file
				}
			}
		}
	}
	return out
}

// summariseToolInput turns an arbitrary tool input shape into a short
// string the UI can render in a chip. Maps are formatted as key=value
// pairs; everything else is JSON-encoded and truncated.
func summariseToolInput(v any) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return truncate(s, 200)
	}
	b, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	return truncate(string(b), 200)
}

func truncate(s string, max int) string {
	if max <= 0 || len(s) <= max {
		return s
	}
	if max <= 1 {
		return s[:max]
	}
	return s[:max-1] + "…"
}

// encodeJSONString marshals v to its JSON form, returning "" on error so
// the caller can fall through to the column default.
func encodeJSONString(v any) string {
	if v == nil {
		return ""
	}
	b, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	return string(b)
}

func (r *Reconstructor) handleNotification(ctx context.Context, st *sessionState, ev *HookEvent) error {
	if !st.hasActiveTurn() {
		return nil
	}
	r.appendSpanEvent(ctx, st.TurnRoot, otel.SpanEvent{
		Name: "claude_code.notification",
		Time: ev.Time(),
		Attributes: map[string]any{
			"notification_type": ev.NotificationType,
			"reason":            ev.Reason,
			"summary":           ev.Summary,
		},
	})
	return nil
}

func (r *Reconstructor) handleSubagentStart(ctx context.Context, st *sessionState, ev *HookEvent) error {
	if !st.hasActiveTurn() {
		return nil
	}
	parent := r.parentFrame(st, "")
	if parent == nil {
		return nil
	}
	agentID := ev.AgentID
	if agentID == "" {
		agentID = fmt.Sprintf("subagent-%d", len(st.Stacks))
	}
	name := "claude_code.subagent"
	if ev.AgentType != "" {
		name = "claude_code.subagent." + ev.AgentType
	}
	span := &otel.Span{
		TraceID:      st.TraceID,
		SpanID:       otel.NewSpanID(),
		ParentSpanID: parent.Span.SpanID,
		Name:         name,
		Kind:         otel.SpanKindInternal,
		StartTime:    ev.Time(),
		StatusCode:   otel.StatusUnset,
		ServiceName:  "claude-code",
		SessionID:    ev.SessionID,
		TurnID:       st.TurnID,
		AgentID:      agentID,
		AgentKind:    "subagent",
		HookEvent:    ev.HookEventType,
		Attributes: map[string]any{
			"claude_code.agent.id":              agentID,
			"claude_code.agent.kind":            "subagent",
			"claude_code.agent.type":            ev.AgentType,
			"claude_code.agent.transcript_path": ev.AgentTranscriptPath,
		},
	}
	parentCtx := ctx
	if parent != nil && parent.OTelCtx != nil {
		parentCtx = parent.OTelCtx
	}
	subCtx, subOTel := r.startOTelSpan(parentCtx, span, oteltrace.SpanKindInternal)
	if err := r.store.InsertSpan(ctx, span); err != nil {
		if subOTel != nil {
			subOTel.SetStatus(codes.Error, "insert subagent span: "+err.Error())
			subOTel.End()
		}
		return err
	}
	r.broadcastSpan(sse.EventTypeSpanInserted, span)
	st.Stacks[agentID] = []*agentFrame{{
		Span:      span,
		StartedAt: ev.Time(),
		OTelSpan:  subOTel,
		OTelCtx:   subCtx,
	}}
	st.SubagentCount++
	r.touchTurnCounters(ctx, st)
	return nil
}

func (r *Reconstructor) handleSubagentStop(ctx context.Context, st *sessionState, ev *HookEvent) error {
	if !st.hasActiveTurn() || ev.AgentID == "" {
		return nil
	}
	frames, ok := st.Stacks[ev.AgentID]
	if !ok || len(frames) == 0 {
		return nil
	}
	root := frames[0]
	end := ev.Time()
	root.Span.EndTime = &end
	root.Span.StatusCode = otel.StatusOK
	if err := r.store.UpdateSpan(ctx, root.Span); err != nil {
		return err
	}
	r.broadcastSpan(sse.EventTypeSpanUpdated, root.Span)
	if root.OTelSpan != nil {
		r.finishOTelSpan(root.OTelSpan, root.Span)
	}
	delete(st.Stacks, ev.AgentID)
	return nil
}

func (r *Reconstructor) handlePreCompact(ctx context.Context, st *sessionState, ev *HookEvent) error {
	if !st.hasActiveTurn() {
		return nil
	}
	r.appendSpanEvent(ctx, st.TurnRoot, otel.SpanEvent{
		Name: "claude_code.compaction",
		Time: ev.Time(),
		Attributes: map[string]any{
			"claude_code.compaction.trigger": ev.Reason,
		},
	})
	if err := r.store.UpdateTurnStatus(ctx, st.TurnID, "compacted", nil, nil, st.ToolCallCount, st.SubagentCount, st.ErrorCount); err != nil {
		return err
	}
	r.broadcastTurn(ctx, st.TurnID, sse.EventTypeTurnUpdated)
	return nil
}

func (r *Reconstructor) handleStop(ctx context.Context, st *sessionState, ev *HookEvent) error {
	if !st.hasActiveTurn() {
		return nil
	}
	r.closeTurn(ctx, st, ev.Time(), "completed")
	return nil
}

// closeTurn finalises every open span belonging to the active turn and writes
// the terminal turn row.
func (r *Reconstructor) closeTurn(ctx context.Context, st *sessionState, end time.Time, status string) {
	// Drop any pending debounced counter write for this turn before we
	// issue the terminal UpdateTurnStatus call. Otherwise the timer may
	// still fire after closeTurn and revert status from
	// "completed"/"stopped"/"errored" back to "running".
	if r.turnCounters != nil && st.TurnID != "" {
		r.turnCounters.cancelTurn(st.TurnID)
	}
	// Close any still-open tool / subagent spans on the agent stacks.
	for key, frames := range st.Stacks {
		for _, f := range frames {
			if f.Span == st.TurnRoot {
				continue
			}
			if f.Span.EndTime != nil {
				continue
			}
			f.Span.EndTime = &end
			if f.Span.StatusCode == otel.StatusUnset {
				f.Span.StatusCode = otel.StatusOK
			}
			if err := r.store.UpdateSpan(ctx, f.Span); err != nil {
				r.logger.Error("close span", "err", err)
				continue
			}
			r.broadcastSpan(sse.EventTypeSpanUpdated, f.Span)
			if f.OTelSpan != nil {
				r.finishOTelSpan(f.OTelSpan, f.Span)
			}
		}
		delete(st.Stacks, key)
	}
	// Close any still-pending HITL spans. These inherit ERROR status with
	// "expired" message and their typed hitl_events twin is moved to
	// status=expired so listings stay consistent.
	for spanID, sp := range st.PendingHITL {
		if sp.EndTime != nil {
			continue
		}
		sp.EndTime = &end
		sp.StatusCode = otel.StatusError
		sp.StatusMessage = "expired"
		if hitlID, ok := sp.Attributes["claude_code.hitl.id"].(string); ok && hitlID != "" {
			if err := r.store.ExpireHITL(ctx, hitlID, end); err != nil {
				r.logger.Debug("close turn: expire hitl", "err", err)
			}
		}
		if err := r.store.UpdateSpan(ctx, sp); err != nil {
			r.logger.Error("close hitl span", "err", err)
			continue
		}
		r.broadcastSpan(sse.EventTypeSpanUpdated, sp)
		if otSpan, ok := st.HITLOTelSpans[spanID]; ok {
			r.finishOTelSpan(otSpan, sp)
			delete(st.HITLOTelSpans, spanID)
		}
		delete(st.PendingHITL, spanID)
	}
	// Close the turn root.
	st.TurnRoot.EndTime = &end
	if status == "completed" {
		st.TurnRoot.StatusCode = otel.StatusOK
	} else if status == "errored" {
		st.TurnRoot.StatusCode = otel.StatusError
	}
	if err := r.store.UpdateSpan(ctx, st.TurnRoot); err != nil {
		r.logger.Error("close turn root", "err", err)
	} else {
		r.broadcastSpan(sse.EventTypeSpanUpdated, st.TurnRoot)
	}
	if st.TurnRootOTel != nil {
		r.finishOTelSpan(st.TurnRootOTel, st.TurnRoot)
	}
	durationMs := end.Sub(st.TurnStartedAt).Milliseconds()
	closedTurnID := st.TurnID
	turnWriteOK := true
	if err := r.store.UpdateTurnStatus(ctx, closedTurnID, status, &end, &durationMs, st.ToolCallCount, st.SubagentCount, st.ErrorCount); err != nil {
		r.logger.Error("close turn", "err", err)
		turnWriteOK = false
	} else {
		r.broadcastTurn(ctx, closedTurnID, sse.EventTypeTurnEnded)
	}
	if turnWriteOK && r.OnTurnClosed != nil {
		r.OnTurnClosed(closedTurnID)
	}
	// Expire any pending turn-scoped interventions now that the turn
	// has moved to a terminal state.
	if r.InterventionsSvc != nil && closedTurnID != "" {
		if err := r.InterventionsSvc.ExpireForTurn(ctx, closedTurnID); err != nil {
			r.logger.Debug("interventions: expire for turn", "err", err)
		}
	}
	st.TurnRoot = nil
	st.TurnRootOTel = nil
	st.TurnRootOTelCtx = nil
	st.TurnID = ""
	st.TraceID = ""
	st.PendingTools = map[string]*otel.Span{}
	st.PendingHITL = map[string]*otel.Span{}
	st.ToolOTelSpans = map[string]oteltrace.Span{}
	st.HITLOTelSpans = map[string]oteltrace.Span{}
	st.Stacks = map[string][]*agentFrame{}
}

// touchTurnCounters coalesces the per-turn counter writes through the
// debouncer. Before PR #37 every tool call (Pre + Post) wrote a fresh
// turns row + fired an SSE turn.updated broadcast; on a 50-tool-call turn
// that was 100 DuckDB round trips. The debouncer defers the flush by
// turnCounterDebounce (250 ms) and replaces intermediate updates in-place,
// so the typical busy turn goes from 100 writes to fewer than 5.
//
// Callers must hold the shard mutex for st's session.
func (r *Reconstructor) touchTurnCounters(ctx context.Context, st *sessionState) {
	if !st.hasActiveTurn() {
		return
	}
	// Snapshot under the caller's shard lock so the debouncer sees a
	// consistent tuple even if a subsequent Apply mutates the state
	// before the flush timer fires.
	snap := pendingTurnUpdate{
		turnID:     st.TurnID,
		sessionID:  st.TurnRoot.SessionID,
		tools:      st.ToolCallCount,
		subagents:  st.SubagentCount,
		errors:     st.ErrorCount,
	}
	if r.turnCounters != nil {
		r.turnCounters.schedule(ctx, snap)
		return
	}
	// Debouncer disabled (tests only): fall through to the old direct
	// write path.
	r.flushTurnCounters(ctx, snap)
}

// flushTurnCounters is the terminal path that actually writes a turn
// counter update to DuckDB and emits an SSE broadcast. It is invoked
// either synchronously (debouncer disabled) or from the debouncer's timer
// goroutine once the 250 ms quiet window elapses.
func (r *Reconstructor) flushTurnCounters(ctx context.Context, u pendingTurnUpdate) {
	if r == nil || r.store == nil || u.turnID == "" {
		return
	}
	if err := r.store.UpdateTurnStatus(ctx, u.turnID, "running", nil, nil, u.tools, u.subagents, u.errors); err != nil {
		r.logger.Error("update turn counters", "err", err)
		return
	}
	r.broadcastTurn(ctx, u.turnID, sse.EventTypeTurnUpdated)
}

func (r *Reconstructor) appendSpanEvent(ctx context.Context, span *otel.Span, ev otel.SpanEvent) {
	span.Events = append(span.Events, ev)
	if err := r.store.UpdateSpan(ctx, span); err != nil {
		r.logger.Error("append span event", "err", err)
		return
	}
	r.broadcastSpan(sse.EventTypeSpanUpdated, span)
	// Mirror the event onto the OTel side. Under sharding we cannot
	// safely range over r.sessions without cross-shard locks, so we
	// look up the owning session by id (the caller always passes a
	// turn-root span which carries the session id as an attribute).
	if r.otelMirrorEnabled() {
		sid := span.SessionID
		if sid == "" {
			return
		}
		r.sessionsMu.RLock()
		st := r.sessions[sid]
		r.sessionsMu.RUnlock()
		if st != nil && st.TurnRoot == span && st.TurnRootOTel != nil {
			r.applyOTelEvents(st.TurnRootOTel, &otel.Span{Events: []otel.SpanEvent{ev}})
		}
	}
}

// parentFrame returns the top of the relevant stack for a tool or subagent
// child span. agentID may be empty, meaning the main agent.
func (r *Reconstructor) parentFrame(st *sessionState, agentID string) *agentFrame {
	key := stackKeyFor(agentID)
	if frames, ok := st.Stacks[key]; ok && len(frames) > 0 {
		return frames[len(frames)-1]
	}
	if agentID != "" {
		// Fall back to main if the requested agent stack is empty (defence
		// against out-of-order subagent events).
		if frames, ok := st.Stacks[mainAgentKey]; ok && len(frames) > 0 {
			return frames[len(frames)-1]
		}
	}
	return nil
}

// popFrameBySpan removes the frame whose span matches sp from whichever stack
// holds it. Searching by span_id rather than blindly popping the top frame
// keeps interleaved tool calls correct.
func (r *Reconstructor) popFrameBySpan(st *sessionState, sp *otel.Span) {
	for key, frames := range st.Stacks {
		for i := len(frames) - 1; i >= 0; i-- {
			if frames[i].Span == sp {
				st.Stacks[key] = append(frames[:i], frames[i+1:]...)
				return
			}
		}
	}
}

func (r *Reconstructor) upsertSession(ctx context.Context, st *sessionState, ev *HookEvent) error {
	sess := duckdb.Session{
		SessionID:  ev.SessionID,
		SourceApp:  ev.SourceApp,
		StartedAt:  st.StartedAt,
		LastSeenAt: ev.Time(),
		Model:      st.Model,
	}
	if err := r.store.UpsertSession(ctx, sess); err != nil {
		return err
	}
	st.SessionInDB = true
	r.broadcastSession(ctx, ev.SessionID)
	return nil
}

func (r *Reconstructor) writeLog(ctx context.Context, st *sessionState, ev *HookEvent) error {
	// Make sure we have a sessions row before logs reference it. Logs do not
	// have a foreign key in the schema, but downstream queries assume the
	// session exists.
	if !st.SessionInDB {
		_ = r.upsertSession(ctx, st, ev)
	}
	body, _ := summariseEvent(ev)
	rec := &otel.LogRecord{
		Timestamp:  ev.Time(),
		TraceID:    st.TraceID,
		SeverityText: "INFO",
		SeverityNumber: 9,
		Body:       body,
		SessionID:  ev.SessionID,
		TurnID:     st.TurnID,
		HookEvent:  ev.HookEventType,
		SourceApp:  ev.SourceApp,
		Attributes: map[string]any{
			"tool_name":   ev.ToolName,
			"tool_use_id": ev.ToolUseID,
			"agent_id":    ev.AgentID,
			"agent_type":  ev.AgentType,
		},
	}
	if ev.HookEventType == HookPostToolUseFail {
		rec.SeverityText = "ERROR"
		rec.SeverityNumber = 17
	}
	return r.store.InsertLog(ctx, rec)
}

func summariseEvent(ev *HookEvent) (string, error) {
	switch ev.HookEventType {
	case HookPreToolUse:
		return fmt.Sprintf("PreToolUse %s", ev.ToolName), nil
	case HookPostToolUse:
		return fmt.Sprintf("PostToolUse %s", ev.ToolName), nil
	case HookPostToolUseFail:
		return fmt.Sprintf("PostToolUseFailure %s: %s", ev.ToolName, ev.Error), nil
	case HookSubagentStart:
		return fmt.Sprintf("SubagentStart %s/%s", ev.AgentType, ev.AgentID), nil
	case HookSubagentStop:
		return fmt.Sprintf("SubagentStop %s", ev.AgentID), nil
	case HookUserPromptSubmit:
		return "UserPromptSubmit", nil
	}
	return ev.HookEventType, nil
}

// pluckString reads a top-level string field from an arbitrary JSON object
// without unmarshalling the whole thing.
func pluckString(raw []byte, field string) string {
	if len(raw) == 0 {
		return ""
	}
	var obj map[string]any
	if err := jsonUnmarshal(raw, &obj); err != nil {
		return ""
	}
	if v, ok := obj[field].(string); ok {
		return v
	}
	return ""
}

func parseMCPName(toolName string) (server, tool string) {
	if !strings.HasPrefix(toolName, "mcp__") {
		return "", ""
	}
	parts := strings.Split(toolName, "__")
	if len(parts) < 3 {
		return "", ""
	}
	return parts[1], strings.Join(parts[2:], "__")
}

func stackKeyFor(agentID string) string {
	if agentID == "" {
		return mainAgentKey
	}
	return agentID
}

// broadcastSession loads the session row and publishes a session.updated
// event. No-op when the hub is nil.
func (r *Reconstructor) broadcastSession(ctx context.Context, sessionID string) {
	if r.Hub == nil {
		return
	}
	sess, err := r.store.GetSession(ctx, sessionID)
	if err != nil || sess == nil {
		if err != nil {
			r.logger.Debug("broadcast session: load", "err", err)
		}
		return
	}
	r.Hub.Broadcast(sse.NewSessionEvent(r.clock(), *sess))
}

// broadcastTurn loads the turn row and publishes a turn.* event using the
// provided kind.
func (r *Reconstructor) broadcastTurn(ctx context.Context, turnID, kind string) {
	if r.Hub == nil {
		return
	}
	t, err := r.store.GetTurn(ctx, turnID)
	if err != nil || t == nil {
		if err != nil {
			r.logger.Debug("broadcast turn: load", "err", err)
		}
		return
	}
	r.Hub.Broadcast(sse.NewTurnEvent(kind, r.clock(), *t))
}

// broadcastSpan emits a span.inserted or span.updated event for the given
// in-memory span. No DB round-trip is needed because reconstructor already
// holds the full struct.
func (r *Reconstructor) broadcastSpan(kind string, sp *otel.Span) {
	if r.Hub == nil || sp == nil {
		return
	}
	row := sse.SpanRowFromOTel(sp)
	r.Hub.Broadcast(sse.NewSpanEvent(kind, r.clock(), row))
}

func coalesce(s, fallback string) string {
	if s == "" {
		return fallback
	}
	return s
}

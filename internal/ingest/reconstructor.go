package ingest

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/BIwashi/apogee/internal/otel"
	"github.com/BIwashi/apogee/internal/store/duckdb"
)

func jsonUnmarshal(b []byte, v any) error { return json.Unmarshal(b, v) }

// agentFrame is one entry on a per-agent span stack.
type agentFrame struct {
	Span      *otel.Span
	StartedAt time.Time
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

	Stacks       map[string][]*agentFrame
	PendingTools map[string]*otel.Span // tool_use_id -> open tool span

	ToolCallCount int
	SubagentCount int
	ErrorCount    int
}

func (st *sessionState) hasActiveTurn() bool {
	return st.TurnRoot != nil
}

// Reconstructor turns a stream of HookEvent values into spans, logs, and
// session/turn rows. It is safe for concurrent use; callers may invoke Apply
// from many goroutines.
type Reconstructor struct {
	mu       sync.Mutex
	sessions map[string]*sessionState
	store    *duckdb.Store
	clock    func() time.Time
	logger   *slog.Logger
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
	return &Reconstructor{
		sessions: make(map[string]*sessionState),
		store:    store,
		logger:   logger,
		clock:    clock,
	}
}

type discardWriter struct{}

func (discardWriter) Write(p []byte) (int, error) { return len(p), nil }

// Apply ingests a single hook event, updating in-memory state and writing to
// the store.
func (r *Reconstructor) Apply(ctx context.Context, ev *HookEvent) error {
	if err := ev.Validate(); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()

	st := r.getOrCreateSession(ev)
	st.LastSeen = ev.Time()

	// Always write the raw log row first so the audit trail is lossless even
	// when the rest of the pipeline drops the event.
	if err := r.writeLog(ctx, st, ev); err != nil {
		r.logger.Error("write log", "err", err, "event", ev.HookEventType)
	}

	switch ev.HookEventType {
	case HookSessionStart:
		return r.handleSessionStart(ctx, st, ev)
	case HookSessionEnd:
		return r.handleSessionEnd(ctx, st, ev)
	case HookUserPromptSubmit:
		return r.handleUserPromptSubmit(ctx, st, ev)
	case HookPreToolUse:
		return r.handlePreToolUse(ctx, st, ev)
	case HookPostToolUse:
		return r.handlePostToolUse(ctx, st, ev, false)
	case HookPostToolUseFail:
		return r.handlePostToolUse(ctx, st, ev, true)
	case HookPermissionRequest:
		return r.handlePermissionRequest(ctx, st, ev)
	case HookNotification:
		return r.handleNotification(ctx, st, ev)
	case HookSubagentStart:
		return r.handleSubagentStart(ctx, st, ev)
	case HookSubagentStop:
		return r.handleSubagentStop(ctx, st, ev)
	case HookPreCompact:
		return r.handlePreCompact(ctx, st, ev)
	case HookStop:
		return r.handleStop(ctx, st, ev)
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
		return nil
	}
}

// getOrCreateSession returns the existing in-memory session state for a
// session id, creating a fresh one when first seen.
func (r *Reconstructor) getOrCreateSession(ev *HookEvent) *sessionState {
	st, ok := r.sessions[ev.SessionID]
	if !ok {
		st = &sessionState{
			SourceApp:    ev.SourceApp,
			StartedAt:    ev.Time(),
			LastSeen:     ev.Time(),
			Stacks:       map[string][]*agentFrame{},
			PendingTools: map[string]*otel.Span{},
		}
		r.sessions[ev.SessionID] = st
	}
	if ev.SourceApp != "" {
		st.SourceApp = ev.SourceApp
	}
	if ev.ModelName != "" {
		st.Model = ev.ModelName
	}
	return st
}

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
	delete(r.sessions, ev.SessionID)
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

	if err := r.store.InsertSpan(ctx, root); err != nil {
		return err
	}

	st.TraceID = traceID
	st.TurnID = turnID
	st.TurnRoot = root
	st.TurnStartedAt = ev.Time()
	st.PromptText = prompt
	st.ToolCallCount = 0
	st.SubagentCount = 0
	st.ErrorCount = 0
	st.Stacks = map[string][]*agentFrame{
		mainAgentKey: {{Span: root, StartedAt: ev.Time()}},
	}
	st.PendingTools = map[string]*otel.Span{}

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

	if err := r.store.InsertSpan(ctx, span); err != nil {
		return err
	}

	stackKey := stackKeyFor(ev.AgentID)
	st.Stacks[stackKey] = append(st.Stacks[stackKey], &agentFrame{Span: span, StartedAt: ev.Time()})
	if ev.ToolUseID != "" {
		st.PendingTools[ev.ToolUseID] = span
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
			"claude_code.hitl.kind":        "permission",
			"claude_code.hitl.suggestions": ev.PermissionSuggestions,
			"claude_code.tool.name":        ev.ToolName,
		},
	}
	return r.store.InsertSpan(ctx, span)
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
	if err := r.store.InsertSpan(ctx, span); err != nil {
		return err
	}
	st.Stacks[agentID] = []*agentFrame{{Span: span, StartedAt: ev.Time()}}
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
	// Close any still-open tool / subagent / hitl spans.
	for key, frames := range st.Stacks {
		for _, f := range frames {
			if f.Span == st.TurnRoot {
				continue
			}
			if f.Span.EndTime != nil {
				continue
			}
			f.Span.EndTime = &end
			if f.Span.Name == "claude_code.hitl.permission" {
				// Leave HITL as UNSET so the UI can render it as pending.
			} else if f.Span.StatusCode == otel.StatusUnset {
				f.Span.StatusCode = otel.StatusOK
			}
			if err := r.store.UpdateSpan(ctx, f.Span); err != nil {
				r.logger.Error("close span", "err", err)
			}
		}
		delete(st.Stacks, key)
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
	}
	durationMs := end.Sub(st.TurnStartedAt).Milliseconds()
	if err := r.store.UpdateTurnStatus(ctx, st.TurnID, status, &end, &durationMs, st.ToolCallCount, st.SubagentCount, st.ErrorCount); err != nil {
		r.logger.Error("close turn", "err", err)
	}
	st.TurnRoot = nil
	st.TurnID = ""
	st.TraceID = ""
	st.PendingTools = map[string]*otel.Span{}
	st.Stacks = map[string][]*agentFrame{}
}

func (r *Reconstructor) touchTurnCounters(ctx context.Context, st *sessionState) {
	if !st.hasActiveTurn() {
		return
	}
	if err := r.store.UpdateTurnStatus(ctx, st.TurnID, "running", nil, nil, st.ToolCallCount, st.SubagentCount, st.ErrorCount); err != nil {
		r.logger.Error("update turn counters", "err", err)
	}
}

func (r *Reconstructor) appendSpanEvent(ctx context.Context, span *otel.Span, ev otel.SpanEvent) {
	span.Events = append(span.Events, ev)
	if err := r.store.UpdateSpan(ctx, span); err != nil {
		r.logger.Error("append span event", "err", err)
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

func coalesce(s, fallback string) string {
	if s == "" {
		return fallback
	}
	return s
}

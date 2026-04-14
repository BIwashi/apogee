package summarizer

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/BIwashi/apogee/internal/store/duckdb"
)

// PromptInput is the per-turn bundle the prompt builder serialises. Turn
// is the enriched row from the store; Spans and Logs are ordered oldest
// first (matching ListLogsByTurn / GetSpansByTurn ordering).
type PromptInput struct {
	Turn  duckdb.Turn
	Spans []duckdb.SpanRow
	Logs  []duckdb.LogRow
}

// hookEventsForLog is the allow-list of log-record hook_event values the
// prompt surfaces. Everything else is filtered out so the model sees a
// dense, relevant event log.
var hookEventsForLog = map[string]bool{
	"PreToolUse":          true,
	"PostToolUse":         true,
	"PostToolUseFailure":  true,
	"UserPromptSubmit":    true,
	"PermissionRequest":   true,
	"Notification":        true,
}

// BuildPrompt assembles the LLM input for one turn. It returns a single
// string ready to hand to Runner.Run. The caller passes the final
// max-spans / max-logs caps so the worker config is the single source of
// truth.
func BuildPrompt(input PromptInput, maxSpans, maxLogs int) string {
	if maxSpans <= 0 {
		maxSpans = 500
	}
	if maxLogs <= 0 {
		maxLogs = 300
	}

	var sb strings.Builder
	writeMetadata(&sb, input.Turn)
	sb.WriteString("\n")
	writeSpanTable(&sb, input.Spans, maxSpans, input.Turn.StartedAt)
	sb.WriteString("\n")
	writeEventLog(&sb, input.Logs, maxLogs)
	sb.WriteString("\n")
	sb.WriteString(instructionBlock)
	return sb.String()
}

const instructionBlock = `You are reviewing one execution turn of a Claude Code agent.

Write a concise structured recap. Respond with a single JSON object matching
this TypeScript type exactly — no prose, no markdown, no backticks:

type Recap = {
  headline: string;          // one sentence, max 140 chars
  outcome: "success" | "partial" | "failure" | "aborted";
  phases: Array<{
    name: "plan" | "explore" | "edit" | "test" | "commit" | "delegate" | "verify" | "debug" | "idle";
    start_span_index: number;  // inclusive, into the span table above
    end_span_index: number;    // inclusive
    summary: string;           // max 80 chars
  }>;
  key_steps: string[];         // 3 to 6 items, max 80 chars each
  failure_cause: string | null; // set only when outcome is not "success"
  notable_events: string[];    // 0 to 5 items, max 80 chars each
};

Rules:
- "headline" is the one-sentence answer a teammate wants when they ask
  "what did the agent just do?"
- Phases must tile the span range contiguously with no overlap.
- Prefer "success" when the turn ended cleanly even if individual tool
  calls errored, as long as the agent recovered.
- Use "failure" when the user goal was clearly not met.
- Use "partial" when some but not all of the ask was completed.
- Use "aborted" when the turn was stopped externally before completion.

Output ONLY the JSON object.
`

func writeMetadata(sb *strings.Builder, t duckdb.Turn) {
	sb.WriteString("TURN METADATA\n")
	sb.WriteString(fmt.Sprintf("session_id: %s\n", t.SessionID))
	sb.WriteString(fmt.Sprintf("turn_id: %s\n", t.TurnID))
	sb.WriteString(fmt.Sprintf("started_at: %s\n", formatTime(t.StartedAt)))
	if t.EndedAt != nil {
		sb.WriteString(fmt.Sprintf("ended_at: %s\n", formatTime(*t.EndedAt)))
	} else {
		sb.WriteString("ended_at: (still running)\n")
	}
	if t.DurationMs != nil {
		sb.WriteString(fmt.Sprintf("duration_ms: %d\n", *t.DurationMs))
	}
	sb.WriteString(fmt.Sprintf("status: %s\n", t.Status))
	sb.WriteString(fmt.Sprintf("tool_call_count: %d\n", t.ToolCallCount))
	sb.WriteString(fmt.Sprintf("subagent_count: %d\n", t.SubagentCount))
	sb.WriteString(fmt.Sprintf("error_count: %d\n", t.ErrorCount))
	if t.Model != "" {
		sb.WriteString(fmt.Sprintf("model: %s\n", t.Model))
	}
	prompt := t.PromptText
	if len(prompt) > 500 {
		prompt = prompt[:500] + "…"
	}
	sb.WriteString(fmt.Sprintf("prompt_text: %s\n", oneLine(prompt)))
}

func writeSpanTable(sb *strings.Builder, spans []duckdb.SpanRow, maxSpans int, turnStart time.Time) {
	sb.WriteString("SPAN TABLE\n")
	if len(spans) == 0 {
		sb.WriteString("(no spans recorded)\n")
		return
	}
	// Ensure deterministic ordering by start_time, then span_id.
	sorted := make([]duckdb.SpanRow, len(spans))
	copy(sorted, spans)
	sort.SliceStable(sorted, func(i, j int) bool {
		if sorted[i].StartTime.Equal(sorted[j].StartTime) {
			return sorted[i].SpanID < sorted[j].SpanID
		}
		return sorted[i].StartTime.Before(sorted[j].StartTime)
	})

	total := len(sorted)
	keep := sorted
	skipped := 0
	if total > maxSpans {
		half := maxSpans / 2
		first := sorted[:half]
		last := sorted[total-(maxSpans-half):]
		keep = append([]duckdb.SpanRow{}, first...)
		keep = append(keep, last...)
		skipped = total - maxSpans
	}

	idx := 0
	for i, sp := range keep {
		if skipped > 0 && i == len(keep)/2 {
			sb.WriteString(fmt.Sprintf("... (%d spans skipped) ...\n", skipped))
		}
		elapsedMs := int64(0)
		if !turnStart.IsZero() {
			elapsedMs = sp.StartTime.Sub(turnStart).Milliseconds()
		}
		var durMs int64 = -1
		if sp.DurationNs != nil {
			durMs = *sp.DurationNs / int64(time.Millisecond)
		}
		status := sp.StatusCode
		if status == "" {
			status = "UNSET"
		}
		shortAttrs := shortSpanAttrs(sp)
		if durMs < 0 {
			sb.WriteString(fmt.Sprintf("[%d] t+%dms open %s %s %s\n", idx, elapsedMs, status, sp.Name, shortAttrs))
		} else {
			sb.WriteString(fmt.Sprintf("[%d] t+%dms %dms %s %s %s\n", idx, elapsedMs, durMs, status, sp.Name, shortAttrs))
		}
		idx++
	}
}

func shortSpanAttrs(sp duckdb.SpanRow) string {
	parts := []string{}
	if sp.ToolName != "" {
		parts = append(parts, "tool="+truncate(sp.ToolName, 80))
	}
	if sp.AgentKind != "" && sp.AgentKind != "main" {
		parts = append(parts, "agent_kind="+truncate(sp.AgentKind, 80))
	}
	if sp.AgentID != "" && sp.AgentID != "main" {
		parts = append(parts, "agent="+truncate(sp.AgentID, 80))
	}
	if sp.MCPServer != "" {
		parts = append(parts, "mcp="+truncate(sp.MCPServer, 80)+"/"+truncate(sp.MCPTool, 80))
	}
	if sp.StatusMessage != "" {
		parts = append(parts, "msg="+truncate(oneLine(sp.StatusMessage), 80))
	}
	if len(parts) == 0 {
		return "-"
	}
	return strings.Join(parts, " ")
}

func writeEventLog(sb *strings.Builder, logs []duckdb.LogRow, maxLogs int) {
	sb.WriteString("EVENT LOG\n")
	filtered := make([]duckdb.LogRow, 0, len(logs))
	for _, l := range logs {
		if hookEventsForLog[l.HookEvent] {
			filtered = append(filtered, l)
		}
	}
	if len(filtered) == 0 {
		sb.WriteString("(no matching events)\n")
		return
	}
	// Keep the newest `maxLogs` records but print oldest-first for the
	// model. We rely on ListLogsByTurn returning ascending timestamps.
	start := 0
	if len(filtered) > maxLogs {
		start = len(filtered) - maxLogs
	}
	for _, l := range filtered[start:] {
		body := oneLine(l.Body)
		if len(body) > 160 {
			body = body[:160] + "…"
		}
		sb.WriteString(fmt.Sprintf("%s  %s  %s\n", formatTime(l.Timestamp), l.HookEvent, body))
	}
}

func oneLine(s string) string {
	s = strings.ReplaceAll(s, "\r", " ")
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.Join(strings.Fields(s), " ")
	return s
}

func formatTime(t time.Time) string {
	if t.IsZero() {
		return "-"
	}
	return t.UTC().Format("2006-01-02T15:04:05.000Z")
}

package ingest

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/BIwashi/apogee/internal/store/duckdb"
)

func newTestRec(t *testing.T) (*Reconstructor, *duckdb.Store) {
	t.Helper()
	ctx := context.Background()
	s, err := duckdb.Open(ctx, ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })
	return NewReconstructor(s, nil, nil), s
}

func ev(typ, sess string, ts time.Time, mods ...func(*HookEvent)) *HookEvent {
	e := &HookEvent{
		SourceApp:     "demo",
		SessionID:     sess,
		HookEventType: typ,
		Timestamp:     ts.UnixMilli(),
	}
	for _, m := range mods {
		m(e)
	}
	return e
}

func TestFullScenarioBashRead(t *testing.T) {
	ctx := context.Background()
	rec, store := newTestRec(t)

	t0 := time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)
	require.NoError(t, rec.Apply(ctx, ev("SessionStart", "s-1", t0)))
	require.NoError(t, rec.Apply(ctx, ev("UserPromptSubmit", "s-1", t0.Add(time.Second), func(e *HookEvent) {
		e.Prompt = "list files"
		e.ModelName = "claude-sonnet-4"
	})))
	require.NoError(t, rec.Apply(ctx, ev("PreToolUse", "s-1", t0.Add(2*time.Second), func(e *HookEvent) {
		e.ToolName = "Bash"
		e.ToolUseID = "tu-bash"
	})))
	require.NoError(t, rec.Apply(ctx, ev("PostToolUse", "s-1", t0.Add(3*time.Second), func(e *HookEvent) {
		e.ToolName = "Bash"
		e.ToolUseID = "tu-bash"
	})))
	require.NoError(t, rec.Apply(ctx, ev("PreToolUse", "s-1", t0.Add(4*time.Second), func(e *HookEvent) {
		e.ToolName = "Read"
		e.ToolUseID = "tu-read"
	})))
	require.NoError(t, rec.Apply(ctx, ev("PostToolUse", "s-1", t0.Add(5*time.Second), func(e *HookEvent) {
		e.ToolName = "Read"
		e.ToolUseID = "tu-read"
	})))
	require.NoError(t, rec.Apply(ctx, ev("Stop", "s-1", t0.Add(6*time.Second))))
	require.NoError(t, rec.Apply(ctx, ev("SessionEnd", "s-1", t0.Add(7*time.Second))))

	turns, err := store.ListRecentTurns(ctx, 0)
	require.NoError(t, err)
	require.Len(t, turns, 1)
	turn := turns[0]
	require.Equal(t, "completed", turn.Status)
	require.Equal(t, 2, turn.ToolCallCount)
	require.NotNil(t, turn.DurationMs)
	require.Equal(t, int64(5000), *turn.DurationMs)

	spans, err := store.GetSpansByTurn(ctx, turn.TurnID)
	require.NoError(t, err)
	require.Len(t, spans, 3) // turn root + 2 tools

	// Identify the root.
	var rootSpanID string
	for _, sp := range spans {
		if sp.Name == "claude_code.turn" {
			rootSpanID = sp.SpanID
		}
	}
	require.NotEmpty(t, rootSpanID)
	for _, sp := range spans {
		if sp.Name == "claude_code.turn" {
			continue
		}
		require.Equal(t, rootSpanID, sp.ParentSpanID, "tool span %s parent", sp.Name)
		require.Equal(t, "OK", sp.StatusCode)
	}
}

func TestSubagentScenario(t *testing.T) {
	ctx := context.Background()
	rec, store := newTestRec(t)

	t0 := time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)
	require.NoError(t, rec.Apply(ctx, ev("UserPromptSubmit", "s-1", t0)))
	require.NoError(t, rec.Apply(ctx, ev("SubagentStart", "s-1", t0.Add(time.Second), func(e *HookEvent) {
		e.AgentID = "A"
		e.AgentType = "Explore"
	})))
	require.NoError(t, rec.Apply(ctx, ev("PreToolUse", "s-1", t0.Add(2*time.Second), func(e *HookEvent) {
		e.ToolName = "Grep"
		e.ToolUseID = "tu-grep"
		e.AgentID = "A"
	})))
	require.NoError(t, rec.Apply(ctx, ev("PostToolUse", "s-1", t0.Add(3*time.Second), func(e *HookEvent) {
		e.ToolName = "Grep"
		e.ToolUseID = "tu-grep"
		e.AgentID = "A"
	})))
	require.NoError(t, rec.Apply(ctx, ev("SubagentStop", "s-1", t0.Add(4*time.Second), func(e *HookEvent) {
		e.AgentID = "A"
	})))
	require.NoError(t, rec.Apply(ctx, ev("Stop", "s-1", t0.Add(5*time.Second))))

	turns, err := store.ListRecentTurns(ctx, 0)
	require.NoError(t, err)
	require.Len(t, turns, 1)
	require.Equal(t, 1, turns[0].SubagentCount)

	spans, err := store.GetSpansByTurn(ctx, turns[0].TurnID)
	require.NoError(t, err)
	require.Len(t, spans, 3)

	byName := map[string]duckdb.SpanRow{}
	for _, sp := range spans {
		byName[sp.Name] = sp
	}
	root := byName["claude_code.turn"]
	subagent := byName["claude_code.subagent.Explore"]
	grep := byName["claude_code.tool.Grep"]
	require.NotEmpty(t, root.SpanID)
	require.NotEmpty(t, subagent.SpanID)
	require.NotEmpty(t, grep.SpanID)
	require.Equal(t, root.SpanID, subagent.ParentSpanID)
	require.Equal(t, subagent.SpanID, grep.ParentSpanID)
	require.Equal(t, "subagent", subagent.AgentKind)
}

func TestErrorScenario(t *testing.T) {
	ctx := context.Background()
	rec, store := newTestRec(t)

	t0 := time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)
	require.NoError(t, rec.Apply(ctx, ev("UserPromptSubmit", "s-1", t0)))
	require.NoError(t, rec.Apply(ctx, ev("PreToolUse", "s-1", t0.Add(time.Second), func(e *HookEvent) {
		e.ToolName = "Bash"
		e.ToolUseID = "tu-bash"
	})))
	require.NoError(t, rec.Apply(ctx, ev("PostToolUseFailure", "s-1", t0.Add(2*time.Second), func(e *HookEvent) {
		e.ToolName = "Bash"
		e.ToolUseID = "tu-bash"
		e.Error = "exit 1"
	})))
	require.NoError(t, rec.Apply(ctx, ev("Stop", "s-1", t0.Add(3*time.Second))))

	turns, err := store.ListRecentTurns(ctx, 0)
	require.NoError(t, err)
	require.Len(t, turns, 1)
	require.Equal(t, 1, turns[0].ErrorCount)

	spans, err := store.GetSpansByTurn(ctx, turns[0].TurnID)
	require.NoError(t, err)
	var bash *duckdb.SpanRow
	for i := range spans {
		if spans[i].ToolName == "Bash" {
			bash = &spans[i]
		}
	}
	require.NotNil(t, bash)
	require.Equal(t, "ERROR", bash.StatusCode)
	require.Equal(t, "exit 1", bash.StatusMessage)
}

func TestOutOfOrderPostToolUse(t *testing.T) {
	ctx := context.Background()
	rec, store := newTestRec(t)

	t0 := time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)
	require.NoError(t, rec.Apply(ctx, ev("UserPromptSubmit", "s-1", t0)))
	// PostToolUse with unknown id — must not crash.
	require.NoError(t, rec.Apply(ctx, ev("PostToolUse", "s-1", t0.Add(time.Second), func(e *HookEvent) {
		e.ToolName = "Bash"
		e.ToolUseID = "tu-ghost"
	})))
	require.NoError(t, rec.Apply(ctx, ev("Stop", "s-1", t0.Add(2*time.Second))))

	spans, err := store.ListRecentSpans(ctx, 0)
	require.NoError(t, err)
	// Only the turn root span exists (no phantom Bash span).
	require.Len(t, spans, 1)
	require.Equal(t, "claude_code.turn", spans[0].Name)
}

func TestPreCompactMarksTurnCompacted(t *testing.T) {
	ctx := context.Background()
	rec, store := newTestRec(t)

	t0 := time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)
	require.NoError(t, rec.Apply(ctx, ev("UserPromptSubmit", "s-1", t0)))
	require.NoError(t, rec.Apply(ctx, ev("PreCompact", "s-1", t0.Add(time.Second), func(e *HookEvent) {
		e.Reason = "auto"
	})))

	turns, err := store.ListRecentTurns(ctx, 0)
	require.NoError(t, err)
	require.Len(t, turns, 1)
	require.Equal(t, "compacted", turns[0].Status)
}

func TestSecondUserPromptClosesPrevious(t *testing.T) {
	ctx := context.Background()
	rec, store := newTestRec(t)

	t0 := time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)
	require.NoError(t, rec.Apply(ctx, ev("UserPromptSubmit", "s-1", t0)))
	require.NoError(t, rec.Apply(ctx, ev("UserPromptSubmit", "s-1", t0.Add(time.Second))))

	turns, err := store.ListRecentTurns(ctx, 0)
	require.NoError(t, err)
	require.Len(t, turns, 2)
	// Newest first.
	require.Equal(t, "running", turns[0].Status)
	require.Equal(t, "stopped", turns[1].Status)
}

func TestPermissionRequestSpanLeftOpen(t *testing.T) {
	ctx := context.Background()
	rec, store := newTestRec(t)

	t0 := time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)
	require.NoError(t, rec.Apply(ctx, ev("UserPromptSubmit", "s-1", t0)))
	require.NoError(t, rec.Apply(ctx, ev("PermissionRequest", "s-1", t0.Add(time.Second), func(e *HookEvent) {
		e.ToolName = "Bash"
		e.PermissionSuggestions = []string{"allow once", "always"}
	})))

	spans, err := store.ListRecentSpans(ctx, 0)
	require.NoError(t, err)
	var hitl *duckdb.SpanRow
	for i := range spans {
		if spans[i].Name == "claude_code.hitl.permission" {
			hitl = &spans[i]
		}
	}
	require.NotNil(t, hitl)
	require.Equal(t, "UNSET", hitl.StatusCode)
	require.Nil(t, hitl.EndTime)
	// Structured HITL row should also exist with status=pending and the
	// permission_suggestions copied verbatim.
	hitlID, _ := hitl.Attributes["claude_code.hitl.id"].(string)
	require.NotEmpty(t, hitlID)
	row, ok, err := store.GetHITL(ctx, hitlID)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, duckdb.HITLStatusPending, row.Status)
	require.Equal(t, "permission", row.Kind)
	require.Contains(t, row.SuggestionsJSON, "allow once")
}

func TestPermissionRequestBroadcastsViaCallback(t *testing.T) {
	ctx := context.Background()
	rec, _ := newTestRec(t)
	var captured duckdb.HITLEvent
	rec.OnHITLRequested = func(ev duckdb.HITLEvent) {
		captured = ev
	}

	t0 := time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)
	require.NoError(t, rec.Apply(ctx, ev("UserPromptSubmit", "s-1", t0)))
	require.NoError(t, rec.Apply(ctx, ev("PermissionRequest", "s-1", t0.Add(time.Second), func(e *HookEvent) {
		e.ToolName = "Bash"
		e.Payload = []byte(`{"tool_input":{"command":"rm -rf /"},"question":"Allow rm -rf /?"}`)
	})))
	require.NotEmpty(t, captured.HitlID)
	require.Equal(t, duckdb.HITLStatusPending, captured.Status)
	require.Equal(t, "Allow rm -rf /?", captured.Question)
	require.Contains(t, captured.ContextJSON, "rm -rf /")
}

func TestCloseTurnExpiresPendingHITL(t *testing.T) {
	ctx := context.Background()
	rec, store := newTestRec(t)

	t0 := time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)
	require.NoError(t, rec.Apply(ctx, ev("UserPromptSubmit", "s-1", t0)))
	require.NoError(t, rec.Apply(ctx, ev("PermissionRequest", "s-1", t0.Add(time.Second), func(e *HookEvent) {
		e.ToolName = "Bash"
	})))
	require.NoError(t, rec.Apply(ctx, ev("Stop", "s-1", t0.Add(2*time.Second))))

	rows, err := store.ListRecentHITL(ctx, duckdb.HITLFilter{}, 10)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, duckdb.HITLStatusExpired, rows[0].Status)
}

func TestMCPToolNameParsing(t *testing.T) {
	server, tool := parseMCPName("mcp__filesystem__read_file")
	require.Equal(t, "filesystem", server)
	require.Equal(t, "read_file", tool)

	server, tool = parseMCPName("Bash")
	require.Empty(t, server)
	require.Empty(t, tool)
}

func TestMCPToolSpanName(t *testing.T) {
	ctx := context.Background()
	rec, store := newTestRec(t)
	t0 := time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)
	require.NoError(t, rec.Apply(ctx, ev("UserPromptSubmit", "s-1", t0)))
	require.NoError(t, rec.Apply(ctx, ev("PreToolUse", "s-1", t0.Add(time.Second), func(e *HookEvent) {
		e.ToolName = "mcp__filesystem__read_file"
		e.ToolUseID = "tu-1"
	})))
	spans, err := store.ListRecentSpans(ctx, 0)
	require.NoError(t, err)
	var found bool
	for _, sp := range spans {
		if sp.MCPServer == "filesystem" && sp.MCPTool == "read_file" {
			require.Equal(t, "claude_code.tool.mcp.filesystem.read_file", sp.Name)
			found = true
		}
	}
	require.True(t, found)
}

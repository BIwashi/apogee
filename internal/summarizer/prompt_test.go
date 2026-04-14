package summarizer

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/BIwashi/apogee/internal/store/duckdb"
)

func TestBuildPromptContainsKeySections(t *testing.T) {
	start := time.Date(2026, 4, 14, 12, 0, 0, 0, time.UTC)
	ended := start.Add(3 * time.Second)
	dur := int64(3000)
	turn := duckdb.Turn{
		TurnID:        "turn-a",
		SessionID:     "sess-1",
		StartedAt:     start,
		EndedAt:       &ended,
		DurationMs:    &dur,
		Status:        "completed",
		Model:         "claude-sonnet-4-6",
		PromptText:    "edit the reconstructor to add a hook",
		ToolCallCount: 2,
		SubagentCount: 0,
		ErrorCount:    0,
	}
	spans := []duckdb.SpanRow{
		{
			SpanID:    "sp1",
			Name:      "claude_code.tool.Edit",
			StartTime: start.Add(100 * time.Millisecond),
			ToolName:  "Edit",
			StatusCode: "OK",
		},
		{
			SpanID:    "sp2",
			Name:      "claude_code.tool.Bash",
			StartTime: start.Add(1500 * time.Millisecond),
			ToolName:  "Bash",
			StatusCode: "OK",
		},
	}
	logs := []duckdb.LogRow{
		{Timestamp: start, HookEvent: "UserPromptSubmit", Body: "UserPromptSubmit"},
		{Timestamp: start.Add(200 * time.Millisecond), HookEvent: "PreToolUse", Body: "PreToolUse Edit"},
	}

	out := BuildPrompt(PromptInput{Turn: turn, Spans: spans, Logs: logs}, 500, 300)

	require.True(t, strings.Contains(out, "TURN METADATA"), out)
	require.True(t, strings.Contains(out, "SPAN TABLE"), out)
	require.True(t, strings.Contains(out, "EVENT LOG"), out)
	require.True(t, strings.Contains(out, "Output ONLY the JSON object."), out)
	require.True(t, strings.Contains(out, "claude_code.tool.Edit"), out)
	require.True(t, strings.Contains(out, "UserPromptSubmit"), out)
	require.True(t, strings.Contains(out, "edit the reconstructor"), out)
}

func TestBuildPromptTruncatesSpans(t *testing.T) {
	start := time.Date(2026, 4, 14, 12, 0, 0, 0, time.UTC)
	turn := duckdb.Turn{TurnID: "turn-b", StartedAt: start, Status: "completed"}
	var spans []duckdb.SpanRow
	for i := 0; i < 20; i++ {
		spans = append(spans, duckdb.SpanRow{
			SpanID:    "sp" + string(rune('a'+i)),
			Name:      "claude_code.tool.x",
			StartTime: start.Add(time.Duration(i) * time.Second),
			ToolName:  "x",
			StatusCode: "OK",
		})
	}
	out := BuildPrompt(PromptInput{Turn: turn, Spans: spans}, 10, 300)
	require.Contains(t, out, "spans skipped")
}

func TestBuildPromptNoSpansNoLogs(t *testing.T) {
	turn := duckdb.Turn{TurnID: "turn-c", StartedAt: time.Now(), Status: "completed"}
	out := BuildPrompt(PromptInput{Turn: turn}, 500, 300)
	require.Contains(t, out, "(no spans recorded)")
	require.Contains(t, out, "(no matching events)")
}

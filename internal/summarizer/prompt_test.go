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

	out := BuildPrompt(PromptInput{Turn: turn, Spans: spans, Logs: logs}, 500, 300, Defaults())

	require.True(t, strings.Contains(out, "TURN METADATA"), out)
	require.True(t, strings.Contains(out, "SPAN TABLE"), out)
	require.True(t, strings.Contains(out, "EVENT LOG"), out)
	require.True(t, strings.Contains(out, "Output ONLY the JSON object."), out)
	require.True(t, strings.Contains(out, "claude_code.tool.Edit"), out)
	require.True(t, strings.Contains(out, "UserPromptSubmit"), out)
	require.True(t, strings.Contains(out, "edit the reconstructor"), out)
}

func TestBuildPromptJapaneseLanguage(t *testing.T) {
	turn := duckdb.Turn{TurnID: "turn-ja", StartedAt: time.Now(), Status: "completed"}
	out := BuildPrompt(PromptInput{Turn: turn}, 500, 300, Preferences{Language: LanguageJA})

	// The TypeScript schema block stays English so the model still sees
	// the canonical type definition.
	require.True(t, strings.Contains(out, "type Recap = {"), out)
	// The instruction prose flips to Japanese.
	require.True(t, strings.Contains(out, "日本語で応答してください"), out)
	require.True(t, strings.Contains(out, "JSON オブジェクトのみを出力してください"), out)
}

func TestBuildPromptUserSystemPromptPrepended(t *testing.T) {
	turn := duckdb.Turn{TurnID: "turn-sys", StartedAt: time.Now(), Status: "completed"}
	prefs := Preferences{
		Language:          LanguageEN,
		RecapSystemPrompt: "Always mention the file paths Claude touched.",
	}
	out := BuildPrompt(PromptInput{Turn: turn}, 500, 300, prefs)
	require.True(t, strings.Contains(out, "# User system prompt"), out)
	require.True(t, strings.Contains(out, "Always mention the file paths Claude touched."), out)
	// The schema block still follows the user system prompt.
	require.True(t, strings.Contains(out, "type Recap = {"), out)
	// Order: user system prompt comes BEFORE the canonical instruction.
	sysIdx := strings.Index(out, "# User system prompt")
	schemaIdx := strings.Index(out, "type Recap = {")
	require.Greater(t, schemaIdx, sysIdx)
}

func TestBuildRollupPromptJapanese(t *testing.T) {
	sess := duckdb.Session{SessionID: "sess-ja", SourceApp: "demo"}
	t1 := time.Date(2026, 4, 14, 12, 0, 0, 0, time.UTC)
	t2 := t1.Add(time.Minute)
	dur := int64(60_000)
	closed := "completed"
	turns := []duckdb.Turn{
		{TurnID: "t-1", StartedAt: t1, EndedAt: &t2, DurationMs: &dur, Status: closed, Headline: "did a thing"},
		{TurnID: "t-2", StartedAt: t2, EndedAt: &t2, DurationMs: &dur, Status: closed, Headline: "did another thing"},
	}
	out := BuildRollupPrompt(sess, turns, Preferences{Language: LanguageJA, RollupSystemPrompt: "強調してほしい点: パフォーマンス"})
	require.True(t, strings.Contains(out, "type Rollup = {"), out)
	require.True(t, strings.Contains(out, "日本語で応答してください"), out)
	require.True(t, strings.Contains(out, "## User system prompt"), out)
	require.True(t, strings.Contains(out, "強調してほしい点: パフォーマンス"), out)
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
	out := BuildPrompt(PromptInput{Turn: turn, Spans: spans}, 10, 300, Defaults())
	require.Contains(t, out, "spans skipped")
}

func TestBuildPromptNoSpansNoLogs(t *testing.T) {
	turn := duckdb.Turn{TurnID: "turn-c", StartedAt: time.Now(), Status: "completed"}
	out := BuildPrompt(PromptInput{Turn: turn}, 500, 300, Defaults())
	require.Contains(t, out, "(no spans recorded)")
	require.Contains(t, out, "(no matching events)")
}

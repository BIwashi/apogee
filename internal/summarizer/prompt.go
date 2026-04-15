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
// truth, plus the operator-controlled Preferences (language + an optional
// system prompt prepended to the instruction block).
func BuildPrompt(input PromptInput, maxSpans, maxLogs int, prefs Preferences) string {
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
	if extra := strings.TrimSpace(prefs.RecapSystemPrompt); extra != "" {
		sb.WriteString("# User system prompt\n")
		sb.WriteString(extra)
		sb.WriteString("\n\n")
	}
	sb.WriteString(recapInstructionBlock(prefs.Language))
	return sb.String()
}

// recapInstructionBlock returns the recap instruction text for the given
// language. Unknown / empty language falls back to English. The TypeScript
// schema block is identical across languages — only the prose rules change
// so the model still sees the canonical Recap type.
func recapInstructionBlock(language string) string {
	switch language {
	case LanguageJA:
		return recapInstructionBlockJA
	default:
		return recapInstructionBlockEN
	}
}

const recapInstructionBlockEN = `You are reviewing one execution turn of a Claude Code agent.

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

const recapInstructionBlockJA = `あなたは Claude Code エージェントの 1 回の実行ターンをレビューしています。

簡潔な構造化レキャップを作成してください。日本語で応答してください。
以下の TypeScript 型に正確に一致する単一の JSON オブジェクトを返してください
— プローズ、マークダウン、バッククォートは禁止です:

type Recap = {
  headline: string;          // 一文、最大 140 文字
  outcome: "success" | "partial" | "failure" | "aborted";
  phases: Array<{
    name: "plan" | "explore" | "edit" | "test" | "commit" | "delegate" | "verify" | "debug" | "idle";
    start_span_index: number;  // 上記スパンテーブルへのインデックス、両端含む
    end_span_index: number;    // 両端含む
    summary: string;           // 最大 80 文字
  }>;
  key_steps: string[];         // 3 〜 6 項目、各最大 80 文字
  failure_cause: string | null; // outcome が "success" でない場合のみ設定
  notable_events: string[];    // 0 〜 5 項目、各最大 80 文字
};

ルール:
- "headline" は「エージェントは今何をしたのか?」と尋ねたチームメイトが
  欲しい一文の答えです。
- フェーズはスパン範囲を重複なく連続的にタイリングする必要があります。
- 個々のツール呼び出しがエラーになっても、エージェントが回復してターンが
  きれいに終わった場合は "success" を優先してください。
- ユーザーの目標が明らかに達成されなかった場合は "failure" を使用してください。
- 依頼の一部だけが完了した場合は "partial" を使用してください。
- 完了前に外部からターンが停止された場合は "aborted" を使用してください。
- すべてのテキストフィールド (headline, summary, key_steps, failure_cause,
  notable_events) は日本語で記述してください。

JSON オブジェクトのみを出力してください。
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

// NarrativePromptInput is the per-session bundle the narrative prompt
// builder serialises. The rollup field carries the tier-2 digest already
// written to session_rollups so the tier-3 model has the big-picture
// narrative as context. Turns is ordered by started_at oldest-first.
type NarrativePromptInput struct {
	SessionID string
	SourceApp string
	Turns     []NarrativeTurn
	Rollup    Rollup
}

// NarrativeTurn is the projection of one turn fed to the narrative prompt.
// Index is the 0-based position in the turn list — the model returns
// first_turn_index / last_turn_index referring to this ordinal, which the
// worker maps back to turn ids before persisting.
type NarrativeTurn struct {
	Index       int
	TurnID      string
	StartedAt   time.Time
	EndedAt     time.Time
	DurationMs  int64
	Status      string
	Headline    string
	Outcome     string
	KeySteps    []string
	ToolSummary map[string]int
}

// BuildNarrativePrompt assembles the tier-3 LLM input. The instruction
// block is mostly identical across languages — only the rule prose flips
// to Japanese when prefs.Language == "ja"; the TypeScript schema stays
// English so the model still sees the canonical type definition.
func BuildNarrativePrompt(input NarrativePromptInput, prefs Preferences) string {
	var sb strings.Builder
	sb.WriteString("You are reviewing a Claude Code session. I will give you the\n")
	sb.WriteString("session metadata, the existing session rollup, and an ordered\n")
	sb.WriteString("list of turns with their per-turn recaps.\n\n")

	sb.WriteString("## Session metadata\n")
	sb.WriteString(fmt.Sprintf("session_id: %s\n", input.SessionID))
	sb.WriteString(fmt.Sprintf("source_app: %s\n", input.SourceApp))
	sb.WriteString(fmt.Sprintf("turn_count: %d\n", len(input.Turns)))
	if len(input.Turns) > 0 {
		first := input.Turns[0]
		last := input.Turns[len(input.Turns)-1]
		sb.WriteString(fmt.Sprintf("started_at: %s\n", formatTime(first.StartedAt)))
		sb.WriteString(fmt.Sprintf("last_ended_at: %s\n", formatTime(last.EndedAt)))
	}
	sb.WriteString("\n")

	// Tier-2 rollup context. The headline + narrative alone is enough — we
	// want the model to build phases *from* the turn list, not to
	// regurgitate the rollup.
	if input.Rollup.Headline != "" || input.Rollup.Narrative != "" {
		sb.WriteString("## Tier-2 rollup (context only)\n")
		if input.Rollup.Headline != "" {
			sb.WriteString("headline: ")
			sb.WriteString(oneLine(input.Rollup.Headline))
			sb.WriteString("\n")
		}
		if input.Rollup.Narrative != "" {
			sb.WriteString("narrative: ")
			sb.WriteString(oneLine(input.Rollup.Narrative))
			sb.WriteString("\n")
		}
		if len(input.Rollup.Highlights) > 0 {
			sb.WriteString("highlights: ")
			sb.WriteString(strings.Join(input.Rollup.Highlights, "; "))
			sb.WriteString("\n")
		}
		sb.WriteString("\n")
	}

	sb.WriteString("## Ordered turns\n")
	for i, t := range input.Turns {
		writeNarrativeTurn(&sb, i, t)
	}
	sb.WriteString("\n")

	if extra := strings.TrimSpace(prefs.NarrativeSystemPrompt); extra != "" {
		sb.WriteString("# User system prompt\n")
		sb.WriteString(extra)
		sb.WriteString("\n\n")
	}
	sb.WriteString("## Instruction\n")
	sb.WriteString(narrativeInstructionBlock(prefs.Language))
	return sb.String()
}

func writeNarrativeTurn(sb *strings.Builder, idx int, t NarrativeTurn) {
	dur := "-"
	if t.DurationMs > 0 {
		dur = fmt.Sprintf("%dms", t.DurationMs)
	}
	headline := t.Headline
	if headline == "" {
		headline = "(no headline)"
	}
	sb.WriteString(fmt.Sprintf("[%d] turn_id=%s %s → %s %s status=%s outcome=%s\n",
		idx,
		truncate(t.TurnID, 20),
		formatTime(t.StartedAt),
		formatTime(t.EndedAt),
		dur,
		t.Status,
		t.Outcome,
	))
	sb.WriteString("    headline: ")
	sb.WriteString(oneLine(headline))
	sb.WriteString("\n")
	if len(t.KeySteps) > 0 {
		sb.WriteString("    key_steps: ")
		sb.WriteString(strings.Join(t.KeySteps, "; "))
		sb.WriteString("\n")
	}
	if len(t.ToolSummary) > 0 {
		// Deterministic ordering for test + cache friendliness.
		keys := make([]string, 0, len(t.ToolSummary))
		for k := range t.ToolSummary {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		parts := make([]string, 0, len(keys))
		for _, k := range keys {
			parts = append(parts, fmt.Sprintf("%s=%d", k, t.ToolSummary[k]))
		}
		sb.WriteString("    tools: ")
		sb.WriteString(strings.Join(parts, " "))
		sb.WriteString("\n")
	}
}

func narrativeInstructionBlock(language string) string {
	switch language {
	case LanguageJA:
		return narrativeInstructionBlockJA
	default:
		return narrativeInstructionBlockEN
	}
}

const narrativeInstructionBlockEN = `
Your job is to group the turns into a small number of semantic *phases* —
short, human-readable chunks that describe the big-picture work being done.
A phase can cover one turn or several consecutive turns. Every turn must
belong to exactly one phase.

Respond with a single JSON object matching this TypeScript type exactly — no
prose, no markdown, no backticks:

type NarrativeResponse = {
  phases: Array<{
    headline: string;          // one sentence, max 140 chars
    narrative: string;         // 1-3 sentences, max 500 chars
    key_steps: string[];       // 2 to 5 items, max 80 chars each
    kind:
      | "implement"
      | "review"
      | "debug"
      | "plan"
      | "test"
      | "commit"
      | "delegate"
      | "explore"
      | "other";
    first_turn_index: number;  // 0-based, into the turn list I gave you
    last_turn_index: number;   // inclusive
  }>;
};

Rules:
- The phases must be ordered chronologically.
- Every turn index in [0, N-1] must be covered by exactly one phase.
- Adjacent phases may not overlap. first_turn_index of phase i+1 must be
  last_turn_index of phase i + 1.
- Phase headlines read like commit messages: past tense, concrete, specific.
- Prefer fewer phases over more. If the whole session is one coherent effort,
  one phase is fine. Multi-hour sessions typically have 3-8 phases.
- The "kind" enum should reflect the dominant activity in the phase:
  implement=writing code, review=reading/approving, debug=hunting bugs,
  plan=planning before writing, test=running tests, commit=git operations,
  delegate=spawning subagents, explore=read-only exploration, other=anything else.

Output ONLY the JSON object.
`

const narrativeInstructionBlockJA = `
あなたの仕事はターンを少数の意味的「フェーズ」にグルーピングすることです。
フェーズは、進行中の作業を大局的に記述する短い人間向けの塊です。1 つの
フェーズは 1 ターンでも複数の連続したターンでも構いません。すべての
ターンは必ずちょうど 1 つのフェーズに属する必要があります。

以下の TypeScript 型に正確に一致する単一の JSON オブジェクトで応答して
ください — プローズ、マークダウン、バッククォートは禁止です:

type NarrativeResponse = {
  phases: Array<{
    headline: string;          // 一文、最大 140 文字
    narrative: string;         // 1〜3 文、最大 500 文字
    key_steps: string[];       // 2〜5 項目、各最大 80 文字
    kind:
      | "implement"
      | "review"
      | "debug"
      | "plan"
      | "test"
      | "commit"
      | "delegate"
      | "explore"
      | "other";
    first_turn_index: number;  // 0-based、私が渡したターンリストへのインデックス
    last_turn_index: number;   // 両端含む
  }>;
};

ルール:
- フェーズは時系列順である必要があります。
- [0, N-1] のすべてのターンインデックスは、ちょうど 1 つのフェーズで
  カバーされる必要があります。
- 隣接するフェーズは重複してはいけません。フェーズ i+1 の first_turn_index
  は、フェーズ i の last_turn_index + 1 でなければなりません。
- フェーズの headline はコミットメッセージのように読めること: 過去形、
  具体的、明確に。
- フェーズは多いより少ない方を優先してください。セッション全体が 1 つの
  まとまった取り組みなら、1 フェーズで問題ありません。数時間のセッションは
  通常 3〜8 フェーズになります。
- "kind" enum はフェーズの主要な活動を反映してください:
  implement=コードを書く、review=読む/承認する、debug=バグ調査、
  plan=書く前の計画、test=テスト実行、commit=git 操作、
  delegate=サブエージェント起動、explore=読み取り専用の探索、other=その他。
- すべてのテキストフィールド (headline, narrative, key_steps) は日本語で
  記述してください。

JSON オブジェクトのみを出力してください。
`

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

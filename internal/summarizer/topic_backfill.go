package summarizer

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"sort"
	"strings"
	"time"

	"github.com/BIwashi/apogee/internal/store/duckdb"
)

// BackfillOptions controls one run of the offline topic classifier.
//
// SessionID, when non-empty, restricts the backfill to a single
// session. The empty string scans every session that has at least
// one classifier candidate.
//
// Force re-classifies turns that already have topic_id set. Without
// it, the backfill skips them so a re-run never thrashes.
//
// Limit caps how many turns the backfill will look at across all
// sessions in one run. 0 means "no limit" — the worker walks every
// candidate. The cap is per-turn, not per-session, so a small cap
// just stops mid-session.
//
// Model lets the caller pin the classifier model alias. Empty falls
// back to the standard ResolveModelForUseCase chain (UseCaseRecap).
type BackfillOptions struct {
	SessionID string
	Force     bool
	Limit     int
	Model     string
	DryRun    bool
}

// BackfillResult is the per-run summary returned to the CLI so it can
// print a one-line-per-session report.
type BackfillResult struct {
	SessionsConsidered int
	TurnsConsidered    int
	TurnsClassified    int
	TurnsSkipped       int
	TurnsErrored       int
}

// BackfillTopics walks closed turns chronologically, calls the local
// `claude` CLI to produce a topic decision per turn, and persists the
// result via the same ApplyTopicDecision path the live recap worker
// uses. The function is intentionally synchronous and single-threaded
// — it is the offline path, run from `apogee topics backfill`, and
// keeping the order stable matters for correct topic-id resolution
// (each turn sees the topics opened by every preceding turn in the
// same session).
//
// Failure paths log and continue: a single bad turn must not stop the
// whole backfill.
func BackfillTopics(
	ctx context.Context,
	store *duckdb.Store,
	runner Runner,
	prefs Preferences,
	opts BackfillOptions,
	logger *slog.Logger,
) (BackfillResult, error) {
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelError}))
	}

	res := BackfillResult{}

	sessions, err := listBackfillSessions(ctx, store, opts.SessionID)
	if err != nil {
		return res, fmt.Errorf("list sessions: %w", err)
	}
	res.SessionsConsidered = len(sessions)

	model := opts.Model
	if model == "" {
		model = ResolveModelForUseCase(UseCaseAgentSummary, "", "", nil)
		if model == "" {
			model = ResolveModelForUseCase(UseCaseRecap, "", "", nil)
		}
	}

	for _, sessionID := range sessions {
		if opts.Limit > 0 && res.TurnsConsidered >= opts.Limit {
			break
		}
		turns, err := loadBackfillTurns(ctx, store, sessionID, opts.Force)
		if err != nil {
			logger.Warn("topic backfill: load turns",
				"session_id", sessionID, "err", err)
			continue
		}
		if len(turns) == 0 {
			continue
		}

		// Reset open-topics state per session so the resolution path
		// only ever sees topics opened by previous turns *in this
		// session*. Without --force, ListOpenTopicsForSession already
		// reflects whatever earlier turns produced (so a second
		// backfill run after an interrupted first one continues
		// cleanly). With --force, all turns will be reclassified, but
		// the first turn's resolution still sees an empty topic
		// list because no earlier turn has stamped one yet.
		for _, turn := range turns {
			if opts.Limit > 0 && res.TurnsConsidered >= opts.Limit {
				break
			}
			res.TurnsConsidered++

			recap, ok := loadRecap(turn)
			if !ok {
				res.TurnsSkipped++
				continue
			}

			openTopics, err := store.ListOpenTopicsForSession(ctx, turn.SessionID, 5)
			if err != nil {
				logger.Warn("topic backfill: list open topics",
					"turn_id", turn.TurnID, "err", err)
				res.TurnsErrored++
				continue
			}

			prompt := BuildTopicBackfillPrompt(turn, recap, openTopics, prefs)
			if opts.DryRun {
				logger.Info("topic backfill: dry-run",
					"turn_id", turn.TurnID,
					"session_id", turn.SessionID,
					"prompt_chars", len(prompt))
				res.TurnsSkipped++
				continue
			}

			runCtx := ctx
			out, err := runner.Run(runCtx, model, prompt)
			if err != nil {
				logger.Warn("topic backfill: runner",
					"turn_id", turn.TurnID, "err", err)
				res.TurnsErrored++
				continue
			}
			decision, err := ParseTopicDecision(out)
			if err != nil {
				logger.Warn("topic backfill: parse decision",
					"turn_id", turn.TurnID, "err", err,
					"raw", truncate(out, 512))
				res.TurnsErrored++
				continue
			}

			recap.TopicDecision = &decision
			recap.Model = model
			now := time.Now().UTC()
			if turn.EndedAt != nil {
				now = *turn.EndedAt
			}
			if err := ApplyTopicDecision(ctx, store, logger, turn, recap, openTopics, now); err != nil {
				logger.Warn("topic backfill: apply decision",
					"turn_id", turn.TurnID, "err", err)
				res.TurnsErrored++
				continue
			}
			res.TurnsClassified++
		}
	}

	return res, nil
}

// listBackfillSessions returns the session ids the backfill should
// walk. When sessionID is non-empty the result is just that one id
// (no validation — the loop will harmlessly produce zero turns if
// the session does not exist). Otherwise the function pulls every
// distinct session id that has at least one turn with a recap.
func listBackfillSessions(ctx context.Context, store *duckdb.Store, sessionID string) ([]string, error) {
	if sessionID != "" {
		return []string{sessionID}, nil
	}
	const q = `
SELECT DISTINCT session_id
FROM turns
WHERE recap_json IS NOT NULL AND recap_json <> ''
ORDER BY session_id ASC
`
	rows, err := store.DB().QueryContext(ctx, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// loadBackfillTurns returns the candidate turns for one session,
// ordered chronologically (oldest first). Without force, turns that
// already carry a topic_id are filtered out. Without recap, the turn
// is kept so the caller can decide what to do — but the typical
// outcome is "skip" because we have nothing useful to feed the LLM.
func loadBackfillTurns(ctx context.Context, store *duckdb.Store, sessionID string, force bool) ([]duckdb.Turn, error) {
	turns, err := store.ListSessionTurns(ctx, sessionID, 1_000)
	if err != nil {
		return nil, err
	}
	// ListSessionTurns is attention-priority ordered; sort
	// chronologically for the classifier's "see prior context" logic.
	sort.SliceStable(turns, func(i, j int) bool {
		return turns[i].StartedAt.Before(turns[j].StartedAt)
	})
	out := turns[:0]
	for _, t := range turns {
		if !force && t.TopicID != "" {
			continue
		}
		if t.RecapJSON == "" {
			continue
		}
		out = append(out, t)
	}
	return out, nil
}

// loadRecap unmarshals the persisted recap blob. Returns ok=false on
// any parse error — the caller treats it as "skip this turn".
func loadRecap(turn duckdb.Turn) (Recap, bool) {
	if turn.RecapJSON == "" {
		return Recap{}, false
	}
	var r Recap
	if err := json.Unmarshal([]byte(turn.RecapJSON), &r); err != nil {
		return Recap{}, false
	}
	return r, true
}

// BuildTopicBackfillPrompt assembles a small classifier-only prompt.
// Unlike BuildPrompt (which feeds the model the entire span + log
// table so it can produce a fresh recap from scratch), the backfill
// prompt only ships the previously-stored recap headline / key_steps
// / outcome plus the OPEN TOPICS list. That keeps the call cheap
// (typically < 1000 input tokens) and reduces the chance of the
// model second-guessing the original recap.
func BuildTopicBackfillPrompt(turn duckdb.Turn, recap Recap, openTopics []duckdb.SessionTopic, prefs Preferences) string {
	var sb strings.Builder
	sb.WriteString("You are classifying one turn of a Claude Code session into the session's topic tree.\n\n")
	sb.WriteString("## Turn metadata\n")
	fmt.Fprintf(&sb, "session_id: %s\n", turn.SessionID)
	fmt.Fprintf(&sb, "turn_id: %s\n", turn.TurnID)
	fmt.Fprintf(&sb, "started_at: %s\n", formatTime(turn.StartedAt))
	if turn.EndedAt != nil {
		fmt.Fprintf(&sb, "ended_at: %s\n", formatTime(*turn.EndedAt))
	}
	if prompt := strings.TrimSpace(turn.PromptText); prompt != "" {
		if len(prompt) > 600 {
			prompt = prompt[:600] + "…"
		}
		fmt.Fprintf(&sb, "prompt_text: %s\n", oneLine(prompt))
	}

	sb.WriteString("\n## Previously-recorded recap\n")
	fmt.Fprintf(&sb, "headline: %s\n", recap.Headline)
	fmt.Fprintf(&sb, "outcome: %s\n", recap.Outcome)
	if len(recap.KeySteps) > 0 {
		sb.WriteString("key_steps:\n")
		for _, s := range recap.KeySteps {
			fmt.Fprintf(&sb, "- %s\n", s)
		}
	}
	if recap.FailureCause != nil && *recap.FailureCause != "" {
		fmt.Fprintf(&sb, "failure_cause: %s\n", *recap.FailureCause)
	}

	sb.WriteString("\n")
	writeOpenTopics(&sb, openTopics)

	sb.WriteString("## Instruction\n")
	sb.WriteString(topicBackfillInstructionBlock(prefs.Language, len(openTopics) > 0))
	return sb.String()
}

func topicBackfillInstructionBlock(language string, hasTopics bool) string {
	switch language {
	case LanguageJA:
		if hasTopics {
			return topicBackfillInstructionEN_NoOp + topicInstructionBlockJA
		}
		return topicBackfillInstructionEN_NoOp + topicBackfillNoTopicsJA
	default:
		if hasTopics {
			return topicBackfillInstructionEN_NoOp + topicInstructionBlockEN
		}
		return topicBackfillInstructionEN_NoOp + topicBackfillNoTopicsEN
	}
}

// topicBackfillInstructionEN_NoOp is the small intro reused for every
// language. It tells the model to emit only the topic_decision JSON
// (no recap, no prose) so the parser only has to handle one shape.
const topicBackfillInstructionEN_NoOp = `Respond with a JSON object that contains exactly one field, "topic_decision".
Schema and rules:
`

const topicBackfillNoTopicsEN = `
This is the session's first classified turn. The OPEN TOPICS list is empty,
so the only valid kind is "new". Pick a short goal (≤ 80 chars) that
captures what this turn does for the operator.

type TopicDecision = {
  kind: "new";
  confidence: number; // 0.0 to 1.0
  goal: string;       // mandatory; ≤ 80 chars
  reason?: string;
};

Output ONLY a JSON object of the shape: { "topic_decision": { ... } }.
`

const topicBackfillNoTopicsJA = `
このセッションで分類済みのトピックがまだありません。OPEN TOPICS が空なので、
有効な kind は "new" のみです。このターンが何をしているかをオペレーターに
伝える短い goal（80 文字以内）を選んでください。

type TopicDecision = {
  kind: "new";
  confidence: number; // 0.0 〜 1.0
  goal: string;       // 必須、80 文字以内
  reason?: string;
};

{ "topic_decision": { ... } } の形の JSON オブジェクトのみを出力してください。
`

// ParseTopicDecision parses a backfill response. The model is
// instructed to wrap the decision in a "topic_decision" envelope so
// the parser can tolerate either shape ("topic_decision":{...} or
// the bare {...}).
func ParseTopicDecision(raw string) (TopicDecision, error) {
	cleaned := strings.TrimSpace(raw)
	cleaned = stripCodeFences(cleaned)
	cleaned = extractJSONObject(cleaned)
	if cleaned == "" {
		return TopicDecision{}, fmt.Errorf("topic decision: empty input")
	}
	var envelope struct {
		TopicDecision *TopicDecision `json:"topic_decision"`
	}
	if err := json.Unmarshal([]byte(cleaned), &envelope); err == nil && envelope.TopicDecision != nil {
		return *envelope.TopicDecision, nil
	}
	// Fall back to the bare shape.
	var bare TopicDecision
	if err := json.Unmarshal([]byte(cleaned), &bare); err != nil {
		return TopicDecision{}, fmt.Errorf("topic decision: unmarshal: %w", err)
	}
	if !bare.Kind.IsValid() {
		return TopicDecision{}, fmt.Errorf("topic decision: invalid kind %q", bare.Kind)
	}
	return bare, nil
}

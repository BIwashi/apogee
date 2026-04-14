package duckdb

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// Turn is a row in the turns table.
type Turn struct {
	TurnID          string     `json:"turn_id"`
	TraceID         string     `json:"trace_id"`
	SessionID       string     `json:"session_id"`
	SourceApp       string     `json:"source_app"`
	StartedAt       time.Time  `json:"started_at"`
	EndedAt         *time.Time `json:"ended_at,omitempty"`
	DurationMs      *int64     `json:"duration_ms,omitempty"`
	Status          string     `json:"status"`
	Model           string     `json:"model,omitempty"`
	PromptText      string     `json:"prompt_text,omitempty"`
	PromptChars     *int       `json:"prompt_chars,omitempty"`
	OutputChars     *int       `json:"output_chars,omitempty"`
	ToolCallCount   int        `json:"tool_call_count"`
	SubagentCount   int        `json:"subagent_count"`
	ErrorCount      int        `json:"error_count"`
	InputTokens     *int64     `json:"input_tokens,omitempty"`
	OutputTokens    *int64     `json:"output_tokens,omitempty"`
	Headline        string     `json:"headline,omitempty"`
	OutcomeSummary  string     `json:"outcome_summary,omitempty"`
	AttentionState  string     `json:"attention_state,omitempty"`
	AttentionReason string     `json:"attention_reason,omitempty"`
	AttentionScore  *float64   `json:"attention_score,omitempty"`
	AttentionTone   string     `json:"attention_tone,omitempty"`
	Phase           string     `json:"phase,omitempty"`
	PhaseConfidence *float64   `json:"phase_confidence,omitempty"`
	PhaseSince      *time.Time `json:"phase_since,omitempty"`
	// AttentionSignalsJSON is the raw JSON serialization of the attention
	// engine's full signal slice. Empty string when the engine has not run
	// against this turn yet.
	AttentionSignalsJSON string `json:"attention_signals_json,omitempty"`
}

// InsertTurn creates a new turn row. The caller is expected to have already
// inserted the matching session.
func (s *Store) InsertTurn(ctx context.Context, t Turn) error {
	const q = `
INSERT INTO turns (
  turn_id, trace_id, session_id, source_app, started_at, status,
  model, prompt_text, prompt_chars, tool_call_count, subagent_count, error_count
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
`
	_, err := s.db.ExecContext(ctx, q,
		t.TurnID,
		t.TraceID,
		t.SessionID,
		t.SourceApp,
		t.StartedAt,
		t.Status,
		nullString(t.Model),
		nullString(t.PromptText),
		nullableInt(t.PromptChars),
		t.ToolCallCount,
		t.SubagentCount,
		t.ErrorCount,
	)
	if err != nil {
		return fmt.Errorf("insert turn: %w", err)
	}
	return nil
}

// UpdateTurnStatus updates the rolling counters and lifecycle fields of a
// turn. Callers pass the turn id and the fields they want to set.
func (s *Store) UpdateTurnStatus(ctx context.Context, turnID, status string, endedAt *time.Time, durationMs *int64, toolCallCount, subagentCount, errorCount int) error {
	const q = `
UPDATE turns
SET status            = ?,
    ended_at          = ?,
    duration_ms       = ?,
    tool_call_count   = ?,
    subagent_count    = ?,
    error_count       = ?
WHERE turn_id = ?
`
	_, err := s.db.ExecContext(ctx, q,
		status,
		nullableTime(endedAt),
		nullableInt64(durationMs),
		toolCallCount,
		subagentCount,
		errorCount,
		turnID,
	)
	if err != nil {
		return fmt.Errorf("update turn status: %w", err)
	}
	return nil
}

// GetTurn fetches one turn by id.
func (s *Store) GetTurn(ctx context.Context, turnID string) (*Turn, error) {
	const q = selectTurn + ` WHERE turn_id = ?`
	row := s.db.QueryRowContext(ctx, q, turnID)
	t, err := scanTurn(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return t, nil
}

// attentionOrder is the shared ORDER BY clause that sorts turns by attention
// priority (intervene_now first) then by recency. Rows with a NULL
// attention_state (pre-engine data) sort after healthy so they degrade
// gracefully.
const attentionOrder = `
ORDER BY
  CASE attention_state
    WHEN 'intervene_now' THEN 0
    WHEN 'watch'         THEN 1
    WHEN 'watchlist'     THEN 2
    WHEN 'healthy'       THEN 3
    ELSE 4
  END ASC,
  started_at DESC
`

// ListRecentTurns returns up to limit turns. Rows are sorted by attention
// priority then recency. Default limit is 100, max 500.
func (s *Store) ListRecentTurns(ctx context.Context, limit int) ([]Turn, error) {
	if limit <= 0 {
		limit = 100
	}
	if limit > 500 {
		limit = 500
	}
	rows, err := s.db.QueryContext(ctx, selectTurn+attentionOrder+` LIMIT ?`, limit)
	if err != nil {
		return nil, fmt.Errorf("list recent turns: %w", err)
	}
	defer rows.Close()
	var out []Turn
	for rows.Next() {
		t, err := scanTurn(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *t)
	}
	return out, rows.Err()
}

// ListActiveTurns returns every turn with status='running' sorted by
// attention priority then started_at desc.
func (s *Store) ListActiveTurns(ctx context.Context, limit int) ([]Turn, error) {
	if limit <= 0 {
		limit = 200
	}
	rows, err := s.db.QueryContext(ctx,
		selectTurn+` WHERE status = 'running' `+attentionOrder+` LIMIT ?`, limit)
	if err != nil {
		return nil, fmt.Errorf("list active turns: %w", err)
	}
	defer rows.Close()
	var out []Turn
	for rows.Next() {
		t, err := scanTurn(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *t)
	}
	return out, rows.Err()
}

// AttentionCounts is the aggregated output of CountAttention. Missing
// buckets are zero.
type AttentionCounts struct {
	InterveneNow int `json:"intervene_now"`
	Watch        int `json:"watch"`
	Watchlist    int `json:"watchlist"`
	Healthy      int `json:"healthy"`
	Total        int `json:"total"`
}

// CountAttention returns the per-bucket count of running turns. When
// includeEnded is true the query considers every row in the turns table
// instead of filtering by status.
func (s *Store) CountAttention(ctx context.Context, includeEnded bool) (AttentionCounts, error) {
	q := `SELECT COALESCE(attention_state, 'healthy') AS state, COUNT(*) FROM turns`
	if !includeEnded {
		q += ` WHERE status = 'running'`
	}
	q += ` GROUP BY 1`
	rows, err := s.db.QueryContext(ctx, q)
	if err != nil {
		return AttentionCounts{}, fmt.Errorf("count attention: %w", err)
	}
	defer rows.Close()
	var out AttentionCounts
	for rows.Next() {
		var state string
		var c int
		if err := rows.Scan(&state, &c); err != nil {
			return AttentionCounts{}, err
		}
		switch state {
		case "intervene_now":
			out.InterveneNow = c
		case "watch":
			out.Watch = c
		case "watchlist":
			out.Watchlist = c
		default:
			out.Healthy += c
		}
		out.Total += c
	}
	return out, rows.Err()
}

// CountRunningTurns returns the number of turns currently in the running
// state. Used by the metrics collector.
func (s *Store) CountRunningTurns(ctx context.Context) (int64, error) {
	var n int64
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM turns WHERE status = 'running'`).Scan(&n); err != nil {
		return 0, fmt.Errorf("count running turns: %w", err)
	}
	return n, nil
}

// UpdateTurnAttention writes the engine's classification onto a turn row.
// Called by the reconstructor after every meaningful mutation. signalsJSON is
// the raw JSON serialization of the engine's Signal slice; pass an empty
// string when there is nothing to record.
func (s *Store) UpdateTurnAttention(ctx context.Context, turnID string, state, reason, tone string, score float64, phase string, phaseConfidence float64, phaseSince time.Time, signalsJSON string) error {
	const q = `
UPDATE turns SET
  attention_state         = ?,
  attention_reason        = ?,
  attention_score         = ?,
  attention_tone          = ?,
  phase                   = ?,
  phase_confidence        = ?,
  phase_since             = ?,
  attention_signals_json  = ?
WHERE turn_id = ?
`
	_, err := s.db.ExecContext(ctx, q,
		nullString(state),
		nullString(reason),
		score,
		nullString(tone),
		nullString(phase),
		phaseConfidence,
		phaseSince,
		nullString(signalsJSON),
		turnID,
	)
	if err != nil {
		return fmt.Errorf("update turn attention: %w", err)
	}
	return nil
}

// ListSessionTurns returns turns belonging to a session, newest first.
func (s *Store) ListSessionTurns(ctx context.Context, sessionID string, limit int) ([]Turn, error) {
	if limit <= 0 {
		limit = 200
	}
	rows, err := s.db.QueryContext(ctx, selectTurn+` WHERE session_id = ? `+attentionOrder+` LIMIT ?`, sessionID, limit)
	if err != nil {
		return nil, fmt.Errorf("list session turns: %w", err)
	}
	defer rows.Close()
	var out []Turn
	for rows.Next() {
		t, err := scanTurn(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *t)
	}
	return out, rows.Err()
}

const selectTurn = `
SELECT
  turn_id, trace_id, session_id, source_app, started_at, ended_at, duration_ms,
  status, model, prompt_text, prompt_chars, output_chars,
  tool_call_count, subagent_count, error_count,
  input_tokens, output_tokens,
  headline, outcome_summary,
  attention_state, attention_reason, attention_score, attention_tone,
  phase, phase_confidence, phase_since, attention_signals_json
FROM turns
`

// rowScanner abstracts *sql.Row / *sql.Rows for scanTurn.
type rowScanner interface {
	Scan(dest ...any) error
}

func scanTurn(r rowScanner) (*Turn, error) {
	var (
		t               Turn
		endedAt         sql.NullTime
		durationMs      sql.NullInt64
		model           sql.NullString
		promptText      sql.NullString
		promptChars     sql.NullInt32
		outputChars     sql.NullInt32
		inputTokens     sql.NullInt64
		outputTokens    sql.NullInt64
		headline        sql.NullString
		outcomeSummary  sql.NullString
		attentionState  sql.NullString
		attentionReas   sql.NullString
		attentionScore  sql.NullFloat64
		attentionTone   sql.NullString
		phase           sql.NullString
		phaseConfidence sql.NullFloat64
		phaseSince      sql.NullTime
		attentionSignal sql.NullString
	)
	if err := r.Scan(
		&t.TurnID, &t.TraceID, &t.SessionID, &t.SourceApp, &t.StartedAt, &endedAt, &durationMs,
		&t.Status, &model, &promptText, &promptChars, &outputChars,
		&t.ToolCallCount, &t.SubagentCount, &t.ErrorCount,
		&inputTokens, &outputTokens,
		&headline, &outcomeSummary,
		&attentionState, &attentionReas, &attentionScore, &attentionTone,
		&phase, &phaseConfidence, &phaseSince, &attentionSignal,
	); err != nil {
		return nil, err
	}
	if endedAt.Valid {
		v := endedAt.Time
		t.EndedAt = &v
	}
	if durationMs.Valid {
		v := durationMs.Int64
		t.DurationMs = &v
	}
	if model.Valid {
		t.Model = model.String
	}
	if promptText.Valid {
		t.PromptText = promptText.String
	}
	if promptChars.Valid {
		v := int(promptChars.Int32)
		t.PromptChars = &v
	}
	if outputChars.Valid {
		v := int(outputChars.Int32)
		t.OutputChars = &v
	}
	if inputTokens.Valid {
		v := inputTokens.Int64
		t.InputTokens = &v
	}
	if outputTokens.Valid {
		v := outputTokens.Int64
		t.OutputTokens = &v
	}
	if headline.Valid {
		t.Headline = headline.String
	}
	if outcomeSummary.Valid {
		t.OutcomeSummary = outcomeSummary.String
	}
	if attentionState.Valid {
		t.AttentionState = attentionState.String
	}
	if attentionReas.Valid {
		t.AttentionReason = attentionReas.String
	}
	if attentionScore.Valid {
		v := attentionScore.Float64
		t.AttentionScore = &v
	}
	if attentionTone.Valid {
		t.AttentionTone = attentionTone.String
	}
	if phase.Valid {
		t.Phase = phase.String
	}
	if phaseConfidence.Valid {
		v := phaseConfidence.Float64
		t.PhaseConfidence = &v
	}
	if phaseSince.Valid {
		v := phaseSince.Time
		t.PhaseSince = &v
	}
	if attentionSignal.Valid {
		t.AttentionSignalsJSON = attentionSignal.String
	}
	return &t, nil
}

func nullableTime(t *time.Time) any {
	if t == nil {
		return nil
	}
	return *t
}

func nullableInt64(v *int64) any {
	if v == nil {
		return nil
	}
	return *v
}

func nullableInt(v *int) any {
	if v == nil {
		return nil
	}
	return *v
}

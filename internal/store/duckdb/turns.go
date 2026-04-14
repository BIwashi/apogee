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

// ListRecentTurns returns up to limit turns ordered by started_at DESC.
func (s *Store) ListRecentTurns(ctx context.Context, limit int) ([]Turn, error) {
	if limit <= 0 {
		limit = 200
	}
	rows, err := s.db.QueryContext(ctx, selectTurn+` ORDER BY started_at DESC LIMIT ?`, limit)
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

// ListSessionTurns returns turns belonging to a session, newest first.
func (s *Store) ListSessionTurns(ctx context.Context, sessionID string, limit int) ([]Turn, error) {
	if limit <= 0 {
		limit = 200
	}
	rows, err := s.db.QueryContext(ctx, selectTurn+` WHERE session_id = ? ORDER BY started_at DESC LIMIT ?`, sessionID, limit)
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
  attention_state, attention_reason, attention_score
FROM turns
`

// rowScanner abstracts *sql.Row / *sql.Rows for scanTurn.
type rowScanner interface {
	Scan(dest ...any) error
}

func scanTurn(r rowScanner) (*Turn, error) {
	var (
		t              Turn
		endedAt        sql.NullTime
		durationMs     sql.NullInt64
		model          sql.NullString
		promptText     sql.NullString
		promptChars    sql.NullInt32
		outputChars    sql.NullInt32
		inputTokens    sql.NullInt64
		outputTokens   sql.NullInt64
		headline       sql.NullString
		outcomeSummary sql.NullString
		attentionState sql.NullString
		attentionReas  sql.NullString
		attentionScore sql.NullFloat64
	)
	if err := r.Scan(
		&t.TurnID, &t.TraceID, &t.SessionID, &t.SourceApp, &t.StartedAt, &endedAt, &durationMs,
		&t.Status, &model, &promptText, &promptChars, &outputChars,
		&t.ToolCallCount, &t.SubagentCount, &t.ErrorCount,
		&inputTokens, &outputTokens,
		&headline, &outcomeSummary,
		&attentionState, &attentionReas, &attentionScore,
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

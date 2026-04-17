package duckdb

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// AgentSummary is the persisted row for one (agent, session) LLM-generated
// label produced by the summarizer's agent worker. SummaryJSON is kept opaque
// at the store layer so we don't import the summarizer package; callers
// unmarshal the blob on their own.
//
// Title and Role are stored as separate columns in addition to the JSON blob
// so the agents catalog query can read them directly without parsing JSON for
// every row.
type AgentSummary struct {
	AgentID                     string         `json:"agent_id"`
	SessionID                   string         `json:"session_id"`
	GeneratedAt                 time.Time      `json:"generated_at"`
	Model                       string         `json:"model"`
	Title                       string         `json:"title"`
	Role                        sql.NullString `json:"-"`
	SummaryJSON                 string         `json:"summary_json"`
	InvocationCountAtGeneration int64          `json:"invocation_count_at_generation"`
}

// UpsertAgentSummary writes or refreshes the summary row for (agent_id,
// session_id). Full replacement — the worker is the only writer.
func (s *Store) UpsertAgentSummary(ctx context.Context, r AgentSummary) error {
	const q = `
INSERT INTO agent_summaries (
  agent_id, session_id, generated_at, model, title, role, summary_json, invocation_count_at_generation
) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT (agent_id, session_id) DO UPDATE SET
  generated_at                   = excluded.generated_at,
  model                          = excluded.model,
  title                          = excluded.title,
  role                           = excluded.role,
  summary_json                   = excluded.summary_json,
  invocation_count_at_generation = excluded.invocation_count_at_generation
`
	_, err := s.db.ExecContext(ctx, q,
		r.AgentID,
		r.SessionID,
		r.GeneratedAt,
		r.Model,
		r.Title,
		r.Role,
		r.SummaryJSON,
		r.InvocationCountAtGeneration,
	)
	if err != nil {
		return fmt.Errorf("upsert agent summary: %w", err)
	}
	return nil
}

// GetAgentSummary returns the summary row for (agentID, sessionID), or
// (_, false, nil) when none has been written yet.
func (s *Store) GetAgentSummary(ctx context.Context, agentID, sessionID string) (AgentSummary, bool, error) {
	const q = `
SELECT agent_id, session_id, generated_at, model, title, role, summary_json, invocation_count_at_generation
FROM agent_summaries
WHERE agent_id = ? AND session_id = ?
`
	var out AgentSummary
	err := s.db.QueryRowContext(ctx, q, agentID, sessionID).Scan(
		&out.AgentID,
		&out.SessionID,
		&out.GeneratedAt,
		&out.Model,
		&out.Title,
		&out.Role,
		&out.SummaryJSON,
		&out.InvocationCountAtGeneration,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return AgentSummary{}, false, nil
		}
		return AgentSummary{}, false, fmt.Errorf("get agent summary: %w", err)
	}
	return out, true, nil
}

// AgentSummaryCandidate is the minimal projection ListAgentSummaryCandidates
// returns: enough for the worker to decide whether the agent needs a fresh
// summary and to dispatch a job.
type AgentSummaryCandidate struct {
	AgentID                string
	SessionID              string
	LastSeen               time.Time
	InvocationCount        int64
	LastSummaryAt          *time.Time
	LastSummaryInvocations *int64
}

// ListAgentSummaryCandidates returns agent (agent_id, session_id) pairs in the
// session that either have no summary yet, or whose invocation_count has grown
// past the last summary's snapshot, or whose summary is older than minAge.
//
// Caller decides what to do with the candidates (typically: enqueue them).
func (s *Store) ListAgentSummaryCandidates(ctx context.Context, sessionID string, minAge time.Duration, limit int) ([]AgentSummaryCandidate, error) {
	if limit <= 0 {
		limit = 50
	}
	cutoff := time.Now().Add(-minAge)
	const q = `
WITH agg AS (
  SELECT
    sp.agent_id,
    sp.session_id,
    MAX(sp.start_time) AS last_seen,
    COUNT(*)           AS invocation_count
  FROM spans sp
  WHERE sp.session_id = ?
    AND sp.agent_id IS NOT NULL AND sp.agent_id <> ''
  GROUP BY sp.agent_id, sp.session_id
)
SELECT
  agg.agent_id,
  agg.session_id,
  agg.last_seen,
  agg.invocation_count,
  s.generated_at,
  s.invocation_count_at_generation
FROM agg
LEFT JOIN agent_summaries s
  ON s.agent_id = agg.agent_id AND s.session_id = agg.session_id
WHERE
  s.generated_at IS NULL
  OR agg.invocation_count > s.invocation_count_at_generation
  OR s.generated_at < ?
ORDER BY agg.last_seen DESC
LIMIT ?
`
	rows, err := s.db.QueryContext(ctx, q, sessionID, cutoff, limit)
	if err != nil {
		return nil, fmt.Errorf("list agent summary candidates: %w", err)
	}
	defer rows.Close()
	var out []AgentSummaryCandidate
	for rows.Next() {
		var (
			c            AgentSummaryCandidate
			lastSummary  sql.NullTime
			lastSummaryN sql.NullInt64
		)
		if err := rows.Scan(&c.AgentID, &c.SessionID, &c.LastSeen, &c.InvocationCount, &lastSummary, &lastSummaryN); err != nil {
			return nil, fmt.Errorf("scan agent summary candidate: %w", err)
		}
		if lastSummary.Valid {
			t := lastSummary.Time
			c.LastSummaryAt = &t
		}
		if lastSummaryN.Valid {
			n := lastSummaryN.Int64
			c.LastSummaryInvocations = &n
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

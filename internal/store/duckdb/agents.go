package duckdb

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"
)

// Agent is an aggregate row over the spans table, keyed by
// (agent_id, agent_kind, session_id). ListRecentAgents derives one row per
// distinct agent the collector has seen, with counters pulled from the
// spans table directly. Used by the /v1/agents/recent endpoint and the
// /agents dashboard page.
type Agent struct {
	AgentID         string         `json:"agent_id"`
	AgentType       string         `json:"agent_type"`
	Kind            string         `json:"kind"`
	ParentAgentID   sql.NullString `json:"-"`
	SessionID       string         `json:"session_id"`
	LastSeen        time.Time      `json:"last_seen"`
	InvocationCount int64          `json:"invocation_count"`
	TotalDurationMs int64          `json:"total_duration_ms"`
	SummaryText     string         `json:"summary_text,omitempty"`
}

// MarshalJSON projects Agent into the on-the-wire shape expected by
// web/app/lib/api-types.ts. parent_agent_id is emitted as `null` when the
// underlying row has no parent.
func (a Agent) MarshalJSON() ([]byte, error) {
	type alias struct {
		AgentID         string    `json:"agent_id"`
		AgentType       string    `json:"agent_type"`
		Kind            string    `json:"kind"`
		ParentAgentID   *string   `json:"parent_agent_id"`
		SessionID       string    `json:"session_id"`
		LastSeen        time.Time `json:"last_seen"`
		InvocationCount int64     `json:"invocation_count"`
		TotalDurationMs int64     `json:"total_duration_ms"`
		SummaryText     string    `json:"summary_text,omitempty"`
	}
	var parent *string
	if a.ParentAgentID.Valid && a.ParentAgentID.String != "" {
		s := a.ParentAgentID.String
		parent = &s
	}
	return json.Marshal(alias{
		AgentID:         a.AgentID,
		AgentType:       a.AgentType,
		Kind:            normaliseAgentKind(a.Kind, a.AgentType),
		ParentAgentID:   parent,
		SessionID:       a.SessionID,
		LastSeen:        a.LastSeen,
		InvocationCount: a.InvocationCount,
		TotalDurationMs: a.TotalDurationMs,
		SummaryText:     a.SummaryText,
	})
}

func normaliseAgentKind(kind, agentType string) string {
	switch kind {
	case "main", "MAIN":
		return "main"
	case "subagent", "SUBAGENT", "sub":
		return "subagent"
	}
	// Fall back to type-based heuristic: main agents carry no type, subagents
	// are named (general-purpose, researcher, etc.).
	if agentType == "" || agentType == "main" {
		return "main"
	}
	return "subagent"
}

// AgentFilter constrains the ListRecentAgents result set. All fields are
// optional — a zero-value filter returns everything.
type AgentFilter struct {
	Since *time.Time
	Until *time.Time
}

// ListRecentAgents returns up to limit agent aggregates ordered by
// last_seen DESC. The result is computed entirely from the spans table;
// it does not require any new schema. The optional filter constrains
// the time window so the TopRibbon "Last 5m" etc. presets work on the
// /agents page.
func (s *Store) ListRecentAgents(ctx context.Context, filter AgentFilter, limit int) ([]Agent, error) {
	if limit <= 0 {
		limit = 100
	}
	if limit > 500 {
		limit = 500
	}

	// Build WHERE clause dynamically so since/until are only applied
	// when the caller provides them.
	where := `sp.agent_id IS NOT NULL AND sp.agent_id <> ''
  AND (s.source_app IS NULL OR s.source_app NOT LIKE '.%')`
	args := make([]any, 0, 3)

	if filter.Since != nil {
		where += ` AND sp.start_time >= ?`
		args = append(args, *filter.Since)
	}
	if filter.Until != nil {
		where += ` AND sp.start_time <= ?`
		args = append(args, *filter.Until)
	}
	args = append(args, limit)

	q := `
SELECT
  sp.agent_id,
  COALESCE(sp.agent_kind, '') AS kind,
  COALESCE(MAX(json_extract_string(sp.attributes_json, '$.claude_code.agent.type')), '') AS agent_type,
  COALESCE(MAX(json_extract_string(sp.attributes_json, '$.claude_code.agent.parent_id')), '') AS parent_agent_id,
  COALESCE(sp.session_id, '') AS session_id,
  MAX(sp.start_time) AS last_seen,
  COUNT(*) AS invocation_count,
  CAST(COALESCE(SUM(sp.duration_ns), 0) / 1000000 AS BIGINT) AS total_duration_ms,
  COALESCE(
    (SELECT t.headline FROM turns t
     WHERE t.session_id = sp.session_id
     AND t.headline IS NOT NULL AND t.headline <> ''
     ORDER BY t.started_at DESC LIMIT 1),
    ''
  ) AS summary_text
FROM spans sp
LEFT JOIN sessions s ON s.session_id = sp.session_id
WHERE ` + where + `
GROUP BY sp.agent_id, sp.agent_kind, sp.session_id
ORDER BY last_seen DESC
LIMIT ?
`
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("list recent agents: %w", err)
	}
	defer rows.Close()

	out := make([]Agent, 0, limit)
	for rows.Next() {
		var (
			a        Agent
			kind     string
			parentID string
		)
		if err := rows.Scan(
			&a.AgentID,
			&kind,
			&a.AgentType,
			&parentID,
			&a.SessionID,
			&a.LastSeen,
			&a.InvocationCount,
			&a.TotalDurationMs,
			&a.SummaryText,
		); err != nil {
			return nil, fmt.Errorf("scan agent row: %w", err)
		}
		a.Kind = kind
		if parentID != "" {
			a.ParentAgentID = sql.NullString{String: parentID, Valid: true}
		}
		out = append(out, a)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate agent rows: %w", err)
	}
	return out, nil
}

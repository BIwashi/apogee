package duckdb

import (
	"context"
	"database/sql"
	"fmt"
)

// AgentToolCount is one row in the tool histogram returned by
// GetAgentDetail. The count is the number of spans whose agent_id matches
// the requested agent id and whose tool_name is set.
type AgentToolCount struct {
	Name  string `json:"name"`
	Count int64  `json:"count"`
}

// AgentDetail bundles every aggregate the cross-cutting AgentDrawer needs
// into a single response payload (PR #36).
//
// The handler invokes one helper per slice rather than a single 500-line
// SQL statement so the stored helpers remain individually testable. None of
// the helpers create any new schema; everything is a read-only projection
// over the existing `spans` and `turns` tables.
type AgentDetail struct {
	Agent      Agent            `json:"agent"`
	Parent     *Agent           `json:"parent"`
	Children   []Agent          `json:"children"`
	Turns      []Turn           `json:"turns"`
	ToolCounts []AgentToolCount `json:"tool_counts"`
}

// GetAgentDetail aggregates the per-agent payload powering the cross-cutting
// AgentDrawer. The function returns nil when no spans carry the requested
// agent id (the agent is unknown to the collector).
func (s *Store) GetAgentDetail(ctx context.Context, agentID string) (*AgentDetail, error) {
	if agentID == "" {
		return nil, nil
	}

	agent, err := s.getAgentRow(ctx, agentID)
	if err != nil {
		return nil, err
	}
	if agent == nil {
		return nil, nil
	}

	parent, err := s.getAgentParent(ctx, *agent)
	if err != nil {
		return nil, err
	}

	children, err := s.listAgentChildren(ctx, agentID)
	if err != nil {
		return nil, err
	}

	turns, err := s.listAgentTurns(ctx, agentID, 20)
	if err != nil {
		return nil, err
	}

	tools, err := s.countAgentTools(ctx, agentID, 20)
	if err != nil {
		return nil, err
	}

	return &AgentDetail{
		Agent:      *agent,
		Parent:     parent,
		Children:   children,
		Turns:      turns,
		ToolCounts: tools,
	}, nil
}

// getAgentRow returns the freshest aggregate row for the given agent id.
// Sessions span the spans table, so the row is keyed by (agent_id) and
// reports the most recent session id the agent was seen in.
func (s *Store) getAgentRow(ctx context.Context, agentID string) (*Agent, error) {
	const q = `
SELECT
  agent_id,
  COALESCE(MAX(agent_kind), '') AS kind,
  COALESCE(MAX(json_extract_string(attributes_json, '$.claude_code.agent.type')), '') AS agent_type,
  COALESCE(MAX(json_extract_string(attributes_json, '$.claude_code.agent.parent_id')), '') AS parent_agent_id,
  COALESCE(MAX(session_id), '') AS session_id,
  MAX(start_time) AS last_seen,
  COUNT(*) AS invocation_count,
  CAST(COALESCE(SUM(duration_ns), 0) / 1000000 AS BIGINT) AS total_duration_ms
FROM spans
WHERE agent_id = ?
GROUP BY agent_id
LIMIT 1
`
	row := s.db.QueryRowContext(ctx, q, agentID)
	var (
		a        Agent
		kind     string
		parentID string
	)
	if err := row.Scan(
		&a.AgentID,
		&kind,
		&a.AgentType,
		&parentID,
		&a.SessionID,
		&a.LastSeen,
		&a.InvocationCount,
		&a.TotalDurationMs,
	); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("get agent row: %w", err)
	}
	a.Kind = kind
	if parentID != "" {
		a.ParentAgentID = sql.NullString{String: parentID, Valid: true}
	}
	return &a, nil
}

func (s *Store) getAgentParent(ctx context.Context, child Agent) (*Agent, error) {
	if !child.ParentAgentID.Valid || child.ParentAgentID.String == "" {
		return nil, nil
	}
	parent, err := s.getAgentRow(ctx, child.ParentAgentID.String)
	if err != nil {
		return nil, err
	}
	return parent, nil
}

// listAgentChildren returns every direct child agent (one row per child
// agent id) whose `claude_code.agent.parent_id` attribute equals the given
// agent id.
func (s *Store) listAgentChildren(ctx context.Context, agentID string) ([]Agent, error) {
	const q = `
SELECT
  agent_id,
  COALESCE(MAX(agent_kind), '') AS kind,
  COALESCE(MAX(json_extract_string(attributes_json, '$.claude_code.agent.type')), '') AS agent_type,
  ? AS parent_agent_id,
  COALESCE(MAX(session_id), '') AS session_id,
  MAX(start_time) AS last_seen,
  COUNT(*) AS invocation_count,
  CAST(COALESCE(SUM(duration_ns), 0) / 1000000 AS BIGINT) AS total_duration_ms
FROM spans
WHERE json_extract_string(attributes_json, '$.claude_code.agent.parent_id') = ?
  AND agent_id IS NOT NULL AND agent_id <> ''
  AND agent_id <> ?
GROUP BY agent_id
ORDER BY last_seen DESC
LIMIT 50
`
	rows, err := s.db.QueryContext(ctx, q, agentID, agentID, agentID)
	if err != nil {
		return nil, fmt.Errorf("list agent children: %w", err)
	}
	defer rows.Close()
	out := make([]Agent, 0)
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
		); err != nil {
			return nil, fmt.Errorf("scan agent child: %w", err)
		}
		a.Kind = kind
		if parentID != "" {
			a.ParentAgentID = sql.NullString{String: parentID, Valid: true}
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// listAgentTurns returns up to limit turns the requested agent participated
// in, joined through the spans table on (agent_id, turn_id). Newest first.
func (s *Store) listAgentTurns(ctx context.Context, agentID string, limit int) ([]Turn, error) {
	if limit <= 0 {
		limit = 20
	}
	q := selectTurn + `
WHERE turn_id IN (
  SELECT DISTINCT turn_id FROM spans
  WHERE agent_id = ? AND turn_id IS NOT NULL AND turn_id <> ''
)
ORDER BY started_at DESC
LIMIT ?
`
	rows, err := s.db.QueryContext(ctx, q, agentID, limit)
	if err != nil {
		return nil, fmt.Errorf("list agent turns: %w", err)
	}
	defer rows.Close()
	out := make([]Turn, 0, limit)
	for rows.Next() {
		t, err := scanTurn(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *t)
	}
	return out, rows.Err()
}

// countAgentTools returns the per-tool span count for the agent, ordered
// most-used first. Spans without a tool_name are excluded.
func (s *Store) countAgentTools(ctx context.Context, agentID string, limit int) ([]AgentToolCount, error) {
	if limit <= 0 {
		limit = 20
	}
	const q = `
SELECT tool_name, COUNT(*) AS n
FROM spans
WHERE agent_id = ? AND tool_name IS NOT NULL AND tool_name <> ''
GROUP BY tool_name
ORDER BY n DESC
LIMIT ?
`
	rows, err := s.db.QueryContext(ctx, q, agentID, limit)
	if err != nil {
		return nil, fmt.Errorf("count agent tools: %w", err)
	}
	defer rows.Close()
	out := make([]AgentToolCount, 0)
	for rows.Next() {
		var row AgentToolCount
		if err := rows.Scan(&row.Name, &row.Count); err != nil {
			return nil, fmt.Errorf("scan agent tool count: %w", err)
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

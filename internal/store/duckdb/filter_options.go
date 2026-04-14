package duckdb

import (
	"context"
	"fmt"
)

// FilterOptions captures the distinct values the dashboard offers as filter
// chips. Empty strings are filtered out by the queries.
type FilterOptions struct {
	SourceApps []string `json:"source_apps"`
	SessionIDs []string `json:"session_ids"`
	HookEvents []string `json:"hook_events"`
	ToolNames  []string `json:"tool_names"`
}

// GetFilterOptions returns the distinct values present in the store.
func (s *Store) GetFilterOptions(ctx context.Context) (*FilterOptions, error) {
	out := &FilterOptions{}

	apps, err := s.distinct(ctx, `SELECT DISTINCT source_app FROM sessions WHERE source_app IS NOT NULL ORDER BY source_app`)
	if err != nil {
		return nil, err
	}
	out.SourceApps = apps

	sessions, err := s.distinct(ctx, `SELECT session_id FROM sessions ORDER BY last_seen_at DESC LIMIT 200`)
	if err != nil {
		return nil, err
	}
	out.SessionIDs = sessions

	hooks, err := s.distinct(ctx, `SELECT DISTINCT hook_event FROM logs WHERE hook_event IS NOT NULL ORDER BY hook_event`)
	if err != nil {
		return nil, err
	}
	out.HookEvents = hooks

	tools, err := s.distinct(ctx, `SELECT DISTINCT tool_name FROM spans WHERE tool_name IS NOT NULL ORDER BY tool_name`)
	if err != nil {
		return nil, err
	}
	out.ToolNames = tools

	return out, nil
}

func (s *Store) distinct(ctx context.Context, q string) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("filter options query: %w", err)
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			return nil, fmt.Errorf("scan filter option: %w", err)
		}
		if v == "" {
			continue
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

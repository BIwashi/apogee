package duckdb

import (
	"context"
	"fmt"
)

// TimeRangeOption is one entry in FilterOptions.TimeRanges. Label is the
// user-facing string; Seconds is the canonical duration in seconds. The
// list is server-stable so the frontend can cache it, but living in the
// API payload means tuning the preset set does not require a rebuild.
type TimeRangeOption struct {
	Label   string `json:"label"`
	Seconds int64  `json:"seconds"`
}

// FilterOptions captures the distinct values the dashboard offers as filter
// chips. Empty strings are filtered out by the queries.
type FilterOptions struct {
	SourceApps []string          `json:"source_apps"`
	SessionIDs []string          `json:"session_ids"`
	HookEvents []string          `json:"hook_events"`
	ToolNames  []string          `json:"tool_names"`
	TimeRanges []TimeRangeOption `json:"time_ranges"`
}

// DefaultTimeRanges mirrors the canonical Datadog-style preset set. Kept as
// a package var so tests can observe the expected shape.
var DefaultTimeRanges = []TimeRangeOption{
	{Label: "Last 5m", Seconds: 5 * 60},
	{Label: "Last 15m", Seconds: 15 * 60},
	{Label: "Last 1h", Seconds: 60 * 60},
	{Label: "Last 4h", Seconds: 4 * 60 * 60},
	{Label: "Last 24h", Seconds: 24 * 60 * 60},
	{Label: "Last 7d", Seconds: 7 * 24 * 60 * 60},
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

	ranges := make([]TimeRangeOption, len(DefaultTimeRanges))
	copy(ranges, DefaultTimeRanges)
	out.TimeRanges = ranges

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

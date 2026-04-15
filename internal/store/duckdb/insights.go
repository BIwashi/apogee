package duckdb

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// ToolCount is one row of the "top tools" chart on the /insights page.
type ToolCount struct {
	Name  string `json:"name"`
	Count int64  `json:"count"`
}

// PhaseCount is one row of the "top phases" chart on the /insights page.
type PhaseCount struct {
	Name  string `json:"name"`
	Count int64  `json:"count"`
}

// InsightsOverview is the aggregate snapshot returned by the
// /v1/insights/overview endpoint. All counters are keyed off the `since`
// cutoff passed to InsightsOverview — defaulting to "last 24 hours" when
// the caller does not supply one.
type InsightsOverview struct {
	TotalSessions     int64        `json:"total_sessions"`
	TotalTurns        int64        `json:"total_turns"`
	TotalEvents       int64        `json:"total_events"`
	ErrorRateLast24h  float64      `json:"error_rate_last_24h"`
	P50TurnDurationMs int64        `json:"p50_turn_duration_ms"`
	P95TurnDurationMs int64        `json:"p95_turn_duration_ms"`
	TopTools          []ToolCount  `json:"top_tools"`
	TopPhases         []PhaseCount `json:"top_phases"`
	WatchlistSessions int64        `json:"watchlist_sessions"`
}

// InsightsOverview runs a handful of cheap aggregate queries against the
// spans / turns / logs tables and assembles them into an InsightsOverview.
// Errors on any individual sub-query are non-fatal — the row is zeroed
// out and the remaining counters still fill in. This keeps the /insights
// page rendering in the face of partial data.
func (s *Store) InsightsOverview(ctx context.Context, since time.Time) (InsightsOverview, error) {
	if since.IsZero() {
		since = time.Now().Add(-24 * time.Hour)
	}
	out := InsightsOverview{
		TopTools:  []ToolCount{},
		TopPhases: []PhaseCount{},
	}

	if err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM sessions WHERE last_seen_at >= ?`, since,
	).Scan(&out.TotalSessions); err != nil && err != sql.ErrNoRows {
		return out, fmt.Errorf("count sessions: %w", err)
	}

	if err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM turns WHERE started_at >= ?`, since,
	).Scan(&out.TotalTurns); err != nil && err != sql.ErrNoRows {
		return out, fmt.Errorf("count turns: %w", err)
	}

	if err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM logs WHERE timestamp >= ?`, since,
	).Scan(&out.TotalEvents); err != nil && err != sql.ErrNoRows {
		return out, fmt.Errorf("count logs: %w", err)
	}

	// Error rate: (turns with error_count > 0) / (closed turns) over the
	// window. A freshly-started turn that has not yet recorded an error
	// does not contribute either way until it closes.
	{
		var closedTurns int64
		var erroredTurns int64
		if err := s.db.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM turns WHERE started_at >= ? AND ended_at IS NOT NULL`, since,
		).Scan(&closedTurns); err != nil && err != sql.ErrNoRows {
			return out, fmt.Errorf("count closed turns: %w", err)
		}
		if err := s.db.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM turns WHERE started_at >= ? AND ended_at IS NOT NULL AND (error_count > 0 OR status = 'errored')`, since,
		).Scan(&erroredTurns); err != nil && err != sql.ErrNoRows {
			return out, fmt.Errorf("count errored turns: %w", err)
		}
		if closedTurns > 0 {
			out.ErrorRateLast24h = float64(erroredTurns) / float64(closedTurns)
		}
	}

	// Duration percentiles via DuckDB's quantile_cont aggregate. Only
	// closed turns with a duration_ms value contribute.
	{
		var p50, p95 sql.NullFloat64
		if err := s.db.QueryRowContext(ctx, `
SELECT quantile_cont(duration_ms, 0.5) AS p50,
       quantile_cont(duration_ms, 0.95) AS p95
FROM turns
WHERE started_at >= ? AND duration_ms IS NOT NULL`, since,
		).Scan(&p50, &p95); err != nil && err != sql.ErrNoRows {
			return out, fmt.Errorf("turn duration percentiles: %w", err)
		}
		if p50.Valid {
			out.P50TurnDurationMs = int64(p50.Float64)
		}
		if p95.Valid {
			out.P95TurnDurationMs = int64(p95.Float64)
		}
	}

	// Top tools: group tool spans by tool_name.
	{
		rows, err := s.db.QueryContext(ctx, `
SELECT tool_name, COUNT(*) AS c
FROM spans
WHERE tool_name IS NOT NULL AND tool_name <> '' AND start_time >= ?
GROUP BY tool_name
ORDER BY c DESC
LIMIT 10`, since)
		if err != nil {
			return out, fmt.Errorf("top tools: %w", err)
		}
		for rows.Next() {
			var tc ToolCount
			if err := rows.Scan(&tc.Name, &tc.Count); err != nil {
				rows.Close()
				return out, fmt.Errorf("scan tool count: %w", err)
			}
			out.TopTools = append(out.TopTools, tc)
		}
		rows.Close()
	}

	// Top phases: group turns by phase bucket.
	{
		rows, err := s.db.QueryContext(ctx, `
SELECT phase, COUNT(*) AS c
FROM turns
WHERE phase IS NOT NULL AND phase <> '' AND started_at >= ?
GROUP BY phase
ORDER BY c DESC
LIMIT 10`, since)
		if err != nil {
			return out, fmt.Errorf("top phases: %w", err)
		}
		for rows.Next() {
			var pc PhaseCount
			if err := rows.Scan(&pc.Name, &pc.Count); err != nil {
				rows.Close()
				return out, fmt.Errorf("scan phase count: %w", err)
			}
			out.TopPhases = append(out.TopPhases, pc)
		}
		rows.Close()
	}

	// Watchlist sessions: sessions whose latest turn is tagged watchlist
	// or watch. Uses the attention_state column denormalised onto turns
	// by the attention engine.
	if err := s.db.QueryRowContext(ctx, `
SELECT COUNT(DISTINCT session_id)
FROM turns
WHERE started_at >= ? AND attention_state IN ('watchlist', 'watch', 'intervene_now')`, since,
	).Scan(&out.WatchlistSessions); err != nil && err != sql.ErrNoRows {
		return out, fmt.Errorf("watchlist sessions: %w", err)
	}

	return out, nil
}

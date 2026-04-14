package duckdb

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"
)

// MetricPoint is the dashboard-facing projection of a single metric_points
// row. Kind mirrors OTel's metric kinds (counter, gauge, histogram).
type MetricPoint struct {
	Timestamp time.Time         `json:"timestamp"`
	Name      string            `json:"name"`
	Kind      string            `json:"kind"`
	Value     float64           `json:"value"`
	Unit      string            `json:"unit,omitempty"`
	Labels    map[string]string `json:"labels,omitempty"`
}

// InsertMetricPoint appends one metric data point. The caller owns the
// timestamp; for live metrics that should be "now", pass time.Now().
func (s *Store) InsertMetricPoint(ctx context.Context, mp MetricPoint) error {
	labelsJSON, err := json.Marshal(coalesceLabels(mp.Labels))
	if err != nil {
		return fmt.Errorf("encode metric labels: %w", err)
	}
	const q = `
INSERT INTO metric_points (timestamp, name, kind, value, unit, labels_json)
VALUES (?, ?, ?, ?, ?, ?)
`
	if _, err := s.db.ExecContext(ctx, q,
		mp.Timestamp,
		mp.Name,
		mp.Kind,
		mp.Value,
		nullString(mp.Unit),
		string(labelsJSON),
	); err != nil {
		return fmt.Errorf("insert metric point: %w", err)
	}
	return nil
}

// MetricSeriesPoint is one bucket in a MetricSeries response.
type MetricSeriesPoint struct {
	At    time.Time `json:"at"`
	Value float64   `json:"value"`
}

// MetricSeriesOptions constrains a GetMetricSeries query. Name is required;
// Window and Step default to 5 minutes and 10 seconds respectively when
// zero. SessionID/SourceApp narrow the series to metric points that carry a
// matching label — the match is performed against labels_json with a LIKE
// filter so callers do not need to maintain a second index.
type MetricSeriesOptions struct {
	Name      string
	Window    time.Duration
	Step      time.Duration
	Kind      string // "gauge" or "counter"; controls fill strategy
	Now       time.Time
	SessionID string
	SourceApp string
}

// GetMetricSeries returns evenly-spaced buckets over [now-window, now]. For
// counters, missing buckets get value 0; for gauges, the previous value is
// carried forward (LOCF). The caller passes the metric kind so the store
// does not have to guess.
func (s *Store) GetMetricSeries(ctx context.Context, opts MetricSeriesOptions) ([]MetricSeriesPoint, error) {
	if opts.Name == "" {
		return nil, fmt.Errorf("metric name is required")
	}
	if opts.Window <= 0 {
		opts.Window = 5 * time.Minute
	}
	if opts.Step <= 0 {
		opts.Step = 10 * time.Second
	}
	now := opts.Now
	if now.IsZero() {
		now = time.Now().UTC()
	}
	start := now.Add(-opts.Window)

	q := `
SELECT time_bucket(INTERVAL (? || ' seconds'), timestamp) AS bucket,
       AVG(value) AS value
FROM metric_points
WHERE name = ? AND timestamp >= ? AND timestamp <= ?
`
	args := []any{}
	stepSeconds := int64(opts.Step / time.Second)
	if stepSeconds <= 0 {
		stepSeconds = 10
	}
	args = append(args, stepSeconds, opts.Name, start, now)
	// Scope by label. Use LIKE against labels_json to avoid a second index;
	// the JSON encoder always quotes the key and value so the fragment is
	// unambiguous. For a scoped query we also require the labels map to
	// contain the key so we do not accidentally match the fleet-wide row.
	if opts.SessionID != "" {
		q += ` AND labels_json LIKE ?`
		args = append(args, `%"session_id":"`+opts.SessionID+`"%`)
	}
	if opts.SourceApp != "" {
		q += ` AND labels_json LIKE ?`
		args = append(args, `%"source_app":"`+opts.SourceApp+`"%`)
	}
	// Fleet query (no scope) excludes per-session rows so the aggregate
	// does not double count. Rows written by the collector at tick time
	// use session_id/source_app keys, so checking for their absence is a
	// cheap proxy for "fleet-wide".
	if opts.SessionID == "" && opts.SourceApp == "" {
		q += ` AND labels_json NOT LIKE '%"session_id":%'`
	}
	q += `
GROUP BY 1
ORDER BY 1 ASC
`
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("metric series query: %w", err)
	}
	defer rows.Close()

	type bucket struct {
		at    time.Time
		value float64
	}
	var stored []bucket
	for rows.Next() {
		var b bucket
		var v sql.NullFloat64
		if err := rows.Scan(&b.at, &v); err != nil {
			return nil, err
		}
		if v.Valid {
			b.value = v.Float64
		}
		stored = append(stored, b)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Fill evenly-spaced buckets for the full window.
	step := opts.Step
	// Align start to the bucket boundary the way time_bucket does.
	alignedStart := start.Truncate(step)
	alignedEnd := now.Truncate(step)
	var out []MetricSeriesPoint
	idx := 0
	var lastVal float64
	for ts := alignedStart; !ts.After(alignedEnd); ts = ts.Add(step) {
		var v float64
		// Counter: fill missing with 0. Gauge: LOCF (carry previous).
		if opts.Kind == "gauge" {
			v = lastVal
		} else {
			v = 0
		}
		if idx < len(stored) && !ts.Before(stored[idx].at) && ts.Before(stored[idx].at.Add(step)) {
			v = stored[idx].value
			lastVal = v
			idx++
		}
		out = append(out, MetricSeriesPoint{At: ts, Value: v})
	}
	return out, nil
}

func coalesceLabels(in map[string]string) map[string]string {
	if in == nil {
		return map[string]string{}
	}
	return in
}

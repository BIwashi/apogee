package duckdb

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// Watchdog severity tiers. The detector maps |z-score| thresholds onto
// these values before inserting a row so the UI can colour the bell and
// drawer without re-running the math.
const (
	WatchdogSeverityInfo     = "info"
	WatchdogSeverityWarning  = "warning"
	WatchdogSeverityCritical = "critical"
)

// ErrWatchdogSignalNotFound is returned by GetWatchdogSignal and
// AckWatchdogSignal when no row matches.
var ErrWatchdogSignalNotFound = errors.New("watchdog signal not found")

// WatchdogSignal is a row in the watchdog_signals table. Nullable columns
// are wrapped in sql.NullXxx types; the HTTP / SSE layers project them
// onto the wire shape.
type WatchdogSignal struct {
	ID             int64        `json:"id"`
	DetectedAt     time.Time    `json:"detected_at"`
	EndedAt        sql.NullTime `json:"-"`
	MetricName     string       `json:"metric_name"`
	LabelsJSON     string       `json:"labels_json"`
	ZScore         float64      `json:"z_score"`
	BaselineMean   float64      `json:"baseline_mean"`
	BaselineStddev float64      `json:"baseline_stddev"`
	WindowValue    float64      `json:"window_value"`
	Severity       string       `json:"severity"`
	Headline       string       `json:"headline"`
	EvidenceJSON   string       `json:"evidence_json"`
	Acknowledged   bool         `json:"acknowledged"`
	AcknowledgedAt sql.NullTime `json:"-"`
}

// WatchdogListFilter narrows ListWatchdogSignals queries.
type WatchdogListFilter struct {
	// OnlyUnacked restricts the result to rows where acknowledged = FALSE.
	OnlyUnacked bool
	// Severity, when non-empty, restricts the result to one tier.
	Severity string
}

const selectWatchdog = `
SELECT
  id, detected_at, ended_at, metric_name, labels_json, z_score,
  baseline_mean, baseline_stddev, window_value, severity, headline,
  evidence_json, acknowledged, acknowledged_at
FROM watchdog_signals
`

// InsertWatchdogSignal persists a fresh anomaly row and returns the row
// with its generated id attached. The caller owns DetectedAt; the rest of
// the nullable columns default to NULL.
func (s *Store) InsertWatchdogSignal(ctx context.Context, sig WatchdogSignal) (WatchdogSignal, error) {
	if sig.LabelsJSON == "" {
		sig.LabelsJSON = "{}"
	}
	if sig.EvidenceJSON == "" {
		sig.EvidenceJSON = "{}"
	}
	if sig.Severity == "" {
		sig.Severity = WatchdogSeverityInfo
	}
	// Validate labels_json so we never store a value that will blow up on
	// read. Round-tripping through encoding/json is cheap.
	var probe map[string]any
	if err := json.Unmarshal([]byte(sig.LabelsJSON), &probe); err != nil {
		return WatchdogSignal{}, fmt.Errorf("watchdog signal labels json: %w", err)
	}
	var evProbe map[string]any
	if err := json.Unmarshal([]byte(sig.EvidenceJSON), &evProbe); err != nil {
		return WatchdogSignal{}, fmt.Errorf("watchdog signal evidence json: %w", err)
	}

	const q = `
INSERT INTO watchdog_signals (
  detected_at, ended_at, metric_name, labels_json, z_score,
  baseline_mean, baseline_stddev, window_value, severity, headline,
  evidence_json, acknowledged, acknowledged_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
RETURNING id
`
	row := s.db.QueryRowContext(ctx, q,
		sig.DetectedAt,
		nullableSQLTime(sig.EndedAt),
		sig.MetricName,
		sig.LabelsJSON,
		sig.ZScore,
		sig.BaselineMean,
		sig.BaselineStddev,
		sig.WindowValue,
		sig.Severity,
		sig.Headline,
		sig.EvidenceJSON,
		sig.Acknowledged,
		nullableSQLTime(sig.AcknowledgedAt),
	)
	if err := row.Scan(&sig.ID); err != nil {
		return WatchdogSignal{}, fmt.Errorf("insert watchdog signal: %w", err)
	}
	return sig, nil
}

// GetWatchdogSignal fetches one row by id. Returns ErrWatchdogSignalNotFound
// when no row matches.
func (s *Store) GetWatchdogSignal(ctx context.Context, id int64) (WatchdogSignal, error) {
	row := s.db.QueryRowContext(ctx, selectWatchdog+` WHERE id = ?`, id)
	sig, err := scanWatchdogSignal(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return WatchdogSignal{}, ErrWatchdogSignalNotFound
		}
		return WatchdogSignal{}, fmt.Errorf("get watchdog signal: %w", err)
	}
	return sig, nil
}

// ListWatchdogSignals returns recent signals newest-first, clamped to
// limit. A zero or negative limit is replaced with 50.
func (s *Store) ListWatchdogSignals(ctx context.Context, filter WatchdogListFilter, limit int) ([]WatchdogSignal, error) {
	if limit <= 0 {
		limit = 50
	}
	q := selectWatchdog + ` WHERE 1=1`
	args := []any{}
	if filter.OnlyUnacked {
		q += ` AND acknowledged = FALSE`
	}
	if filter.Severity != "" {
		q += ` AND severity = ?`
		args = append(args, filter.Severity)
	}
	q += ` ORDER BY detected_at DESC, id DESC LIMIT ?`
	args = append(args, limit)

	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("list watchdog signals: %w", err)
	}
	defer rows.Close()

	var out []WatchdogSignal
	for rows.Next() {
		sig, err := scanWatchdogSignal(rows)
		if err != nil {
			return nil, fmt.Errorf("scan watchdog signal: %w", err)
		}
		out = append(out, sig)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// AckWatchdogSignal flips acknowledged to TRUE and stamps acknowledged_at.
// Returns ErrWatchdogSignalNotFound when no row matches. Idempotent: a
// second ack is a no-op (the columns already carry the ack state).
func (s *Store) AckWatchdogSignal(ctx context.Context, id int64, now time.Time) (WatchdogSignal, error) {
	const q = `
UPDATE watchdog_signals
SET acknowledged = TRUE,
    acknowledged_at = COALESCE(acknowledged_at, ?)
WHERE id = ?
`
	res, err := s.db.ExecContext(ctx, q, now, id)
	if err != nil {
		return WatchdogSignal{}, fmt.Errorf("ack watchdog signal: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return WatchdogSignal{}, fmt.Errorf("ack watchdog signal: rows: %w", err)
	}
	if n == 0 {
		return WatchdogSignal{}, ErrWatchdogSignalNotFound
	}
	return s.GetWatchdogSignal(ctx, id)
}

// LatestOpenWatchdogSpell returns the newest watchdog_signals row for the
// given (metric_name, labels_json) tuple whose ended_at is NULL, or
// (zero, false, nil) when no open spell exists. Used by the detector to
// suppress re-emitting a signal while the metric remains anomalous.
func (s *Store) LatestOpenWatchdogSpell(ctx context.Context, metricName, labelsJSON string) (WatchdogSignal, bool, error) {
	q := selectWatchdog + ` WHERE metric_name = ? AND labels_json = ? AND ended_at IS NULL ORDER BY detected_at DESC LIMIT 1`
	row := s.db.QueryRowContext(ctx, q, metricName, labelsJSON)
	sig, err := scanWatchdogSignal(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return WatchdogSignal{}, false, nil
		}
		return WatchdogSignal{}, false, fmt.Errorf("latest open watchdog spell: %w", err)
	}
	return sig, true, nil
}

// CloseWatchdogSpell stamps ended_at on a signal row, marking the spell
// complete. Called by the detector once the metric returns to baseline.
func (s *Store) CloseWatchdogSpell(ctx context.Context, id int64, endedAt time.Time) error {
	const q = `UPDATE watchdog_signals SET ended_at = ? WHERE id = ? AND ended_at IS NULL`
	if _, err := s.db.ExecContext(ctx, q, endedAt, id); err != nil {
		return fmt.Errorf("close watchdog spell: %w", err)
	}
	return nil
}

// ReadMetricWindow fetches raw (timestamp, value) samples for the given
// (name, labels_json) tuple between the two bounds, oldest first. Used by
// the watchdog worker to build both its baseline and its window.
func (s *Store) ReadMetricWindow(ctx context.Context, name, labelsJSON string, from, to time.Time) ([]MetricSeriesPoint, error) {
	q := `
SELECT timestamp, value
FROM metric_points
WHERE name = ? AND labels_json = ? AND timestamp >= ? AND timestamp <= ?
ORDER BY timestamp ASC
`
	rows, err := s.db.QueryContext(ctx, q, name, labelsJSON, from, to)
	if err != nil {
		return nil, fmt.Errorf("read metric window: %w", err)
	}
	defer rows.Close()

	var out []MetricSeriesPoint
	for rows.Next() {
		var at time.Time
		var v sql.NullFloat64
		if err := rows.Scan(&at, &v); err != nil {
			return nil, err
		}
		out = append(out, MetricSeriesPoint{At: at, Value: nullableFloat(v)})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func scanWatchdogSignal(r rowScanner) (WatchdogSignal, error) {
	var sig WatchdogSignal
	err := r.Scan(
		&sig.ID,
		&sig.DetectedAt,
		&sig.EndedAt,
		&sig.MetricName,
		&sig.LabelsJSON,
		&sig.ZScore,
		&sig.BaselineMean,
		&sig.BaselineStddev,
		&sig.WindowValue,
		&sig.Severity,
		&sig.Headline,
		&sig.EvidenceJSON,
		&sig.Acknowledged,
		&sig.AcknowledgedAt,
	)
	return sig, err
}

func nullableFloat(v sql.NullFloat64) float64 {
	if !v.Valid {
		return 0
	}
	return v.Float64
}

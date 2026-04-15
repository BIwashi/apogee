# Watchdog — statistical anomaly detection

apogee ships with a Datadog-Watchdog-inspired anomaly detector. A
background worker reads `metric_points`, compares the latest 60 s
window against a rolling 24 h baseline, and emits a row to a new
`watchdog_signals` table whenever the window deviates by more than
three standard deviations.

The detector lives in `internal/watchdog/`:

- `zscore.go` — baseline mean / stddev computation and the z-score
  threshold ladder.
- `watchdog.go` — the `Worker` type plus the loop that ticks every
  `Tick` (60 s by default), evaluates every monitored metric, writes
  signals to DuckDB, and broadcasts them on the SSE hub.

The persistence layer is
`internal/store/duckdb/watchdog_signals.go` (CRUD + spell helpers),
the schema lives in `internal/store/duckdb/schema.sql` next to the
existing tables, and the dashboard surfaces signals through
`web/app/components/WatchdogBell.tsx` and
`web/app/components/WatchdogDrawer.tsx`.

## Signal definition

A watchdog signal is a statistically anomalous `metric_points`
datapoint. Computation:

1. Read every sample of the last 60 s for a given
   `(metric_name, labels_json)` tuple — the **window**.
2. Read every sample of the last 24 h for the same tuple — the
   **baseline**.
3. Compute the baseline mean (`μ`) and population standard deviation
   (`σ`). Baselines with fewer than three samples or zero variance
   are flagged degenerate and skipped — no z-score can be computed.
4. For each window sample, compute `z = (x − μ) / σ`.
5. Track the most extreme `|z|` observed in the window. If it exceeds
   the `SignalThreshold` (3.0) the worker emits a signal.

Signals are emitted **once per spell**. A spell ends when the metric
returns to normal — every window sample has `|z| < NormalThreshold`
(1.5) — for at least `NormalForWait` (3 minutes by default). A new
spell can start after that. While a spell is open, the detector
swallows further deviations so the bell does not flash on every tick.

### Severity

| `|z|` band       | Severity   |
|------------------|------------|
| `[3, 5)`         | `info`     |
| `[5, 8)`         | `warning`  |
| `[8, ∞)`         | `critical` |

## Monitored metrics

`DefaultMonitoredMetrics()` ships with the four fleet-wide gauges /
counters written by `internal/metrics/collector.go`. Each entry
carries a `fmt.Sprintf` headline template applied to `(value, mean,
stddev)`:

- `apogee.turns.active` — `Active turns spiked to %.0f (baseline %.1f ± %.1f)`
- `apogee.errors.rate` — `Error rate surge — %.1f/s vs baseline %.2f/s (±%.2f)`
- `apogee.tools.rate` — `Unusual tool activity — %.1f/s vs baseline %.2f/s (±%.2f)`
- `apogee.hitl.pending` — `HITL backlog growing — %.0f pending vs baseline %.1f (±%.2f)`

Operators that want to monitor extra labelled tuples (e.g. one row per
`source_app`) can extend the slice on the `Worker` directly — the
field is exported.

## HTTP surface

- `GET /v1/watchdog/signals?status=unacked&limit=N`
  Returns recent signals newest-first. Query params:
  - `status=unacked` restricts to rows with `acknowledged = FALSE`.
  - `severity=info|warning|critical` restricts to one tier.
  - `limit` clamps the response (default 50, max 200).
- `POST /v1/watchdog/signals/:id/ack`
  Flips `acknowledged` to TRUE and stamps `acknowledged_at`.
  Idempotent — a second ack is a no-op that returns the current row.

The SSE wire emits a single new event type, `watchdog.signal`. The
payload mirrors `internal/sse.WatchdogSnapshot` — labels are decoded
into a flat object and the `evidence` field carries the typed
`{ window, baseline, z }` shape consumed by the drawer's sparkline.

## Sample signal payload

```json
{
  "id": 42,
  "detected_at": "2026-04-15T09:12:33Z",
  "ended_at": null,
  "metric_name": "apogee.tools.rate",
  "labels": {},
  "z_score": 6.32,
  "baseline_mean": 2.10,
  "baseline_stddev": 0.48,
  "window_value": 15.0,
  "severity": "warning",
  "headline": "Unusual tool activity — 15.0/s vs baseline 2.10/s (±0.48)",
  "evidence": {
    "window": [
      { "at": "2026-04-15T09:11:35Z", "value": 2.0 },
      { "at": "2026-04-15T09:12:05Z", "value": 1.0 },
      { "at": "2026-04-15T09:12:30Z", "value": 15.0 }
    ],
    "baseline": { "mean": 2.10, "stddev": 0.48 },
    "z": [-0.21, -2.29, 26.88]
  },
  "acknowledged": false,
  "acknowledged_at": null
}
```

## Dashboard UI

The TopRibbon gains a bell icon between the language picker and the
theme toggle. The icon shows a coloured badge with the unread count.
The badge is `--status-warning` at rest and flips to `--status-critical`
the moment the unread set contains at least one critical signal. When
critical signals are present the bell pulses; the animation is
disabled by `prefers-reduced-motion`.

Clicking the bell opens `WatchdogDrawer`, an `SideDrawer`-backed
overlay at `md` width (480 px). The drawer renders one card per
signal, ordered newest first. Each card shows the severity icon, a
relative timestamp, the headline, the metric name, the labels, a
recharts sparkline of the window samples (with the baseline mean as a
dashed reference line), and an Acknowledge button. The filter chips at
the top of the drawer narrow the list to `Unacked / All / Critical /
Warning`.

## Configuration

Every tunable lives directly on the `Worker` struct so tests can pin
them and operators can override them in code:

| Field            | Default | Purpose                                     |
|------------------|---------|---------------------------------------------|
| `Tick`           | 60 s    | How often the detector runs.                |
| `Window`         | 60 s    | Length of the rolling evaluation window.    |
| `Baseline`       | 24 h    | Length of the historical baseline window.   |
| `NormalForWait`  | 3 min   | Dwell required before closing a spell.      |
| `Metrics`        | 4 entries | Tuples to evaluate per tick.               |
| `Clock`          | `time.Now().UTC()` | Time source — pinned in tests.   |

## Verification

1. `go vet ./... && go build ./... && go test ./... -race -count=1`
2. Boot a collector, let the metric_points writer run for at least a
   minute, then inject a spike (e.g. by POSTing 100 `/v1/events` for a
   single session to make `apogee.tools.rate` deviate). After the next
   tick a signal should appear in `GET /v1/watchdog/signals`.
3. In the web dashboard the bell should show `1`, the drawer should
   list the signal, and the Acknowledge button should drop the badge
   to zero.

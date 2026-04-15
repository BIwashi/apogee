package collector

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/BIwashi/apogee/internal/store/duckdb"
)

// events.go carries the PR #37 event-browser endpoints that power the
// Datadog-style /events page:
//
//   GET /v1/events/facets     — distinct values + counts per facet dimension
//   GET /v1/events/timeseries — stacked-bar histogram buckets (by severity)
//   GET /v1/live/bootstrap    — consolidated first-paint payload for /
//
// Each handler shares a parseLogFilter helper that mirrors the semantics of
// listRecentEvents (so deep-linked URLs work across both views) and adds
// multi-select parsing for source_app / hook_event / severity / session.
//
// Time windows are expressed as `?window=1h` for convenience; the handler
// expands that to Since/Until before hitting DuckDB. Explicit `?since=` /
// `?until=` override the window when supplied.

// parseLogFilter extracts the canonical event-browser filter from the query
// params. Reused by listRecentEvents (future follow-up) as well as the three
// PR #37 handlers below.
func parseLogFilter(r *http.Request) duckdb.LogFilter {
	q := r.URL.Query()
	filter := duckdb.LogFilter{
		Query: q.Get("q"),
	}
	// Singular fields remain for backward compat. They fold into the
	// plural variants inside buildWhere.
	filter.SessionID = q.Get("session_id")
	filter.SourceApp = q.Get("source_app")
	filter.Type = q.Get("type")

	// Multi-select: chi/go-chi query params naturally expose multiple
	// values when the client repeats `?source_app=a&source_app=b`. We
	// also accept comma-separated lists so bookmarklets survive the
	// Datadog-style shareable URLs.
	filter.SourceApps = multiQuery(q, "source_app", "facets.source_app")
	filter.HookEvents = multiQuery(q, "hook_event", "facets.hook_event")
	filter.Severities = multiQuery(q, "severity", "facets.severity_text")
	filter.Sessions = multiQuery(q, "facets.session_id")

	// Time window. If both since/until are explicit, trust them. Otherwise
	// expand ?window= into a trailing range ending at now.
	if raw := q.Get("since"); raw != "" {
		if t, err := time.Parse(time.RFC3339, raw); err == nil {
			filter.Since = t
		}
	}
	if raw := q.Get("until"); raw != "" {
		if t, err := time.Parse(time.RFC3339, raw); err == nil {
			filter.Until = t
		}
	}
	if filter.Since.IsZero() && filter.Until.IsZero() {
		if win := parseWindow(q.Get("window")); win > 0 {
			now := time.Now().UTC()
			filter.Since = now.Add(-win)
			filter.Until = now
		}
	}
	return filter
}

// multiQuery reads a repeated query parameter under any of the provided
// keys and explodes comma-separated values. Empty strings are stripped so
// the store layer does not see "IN ('')" clauses. Duplicates are removed to
// keep the placeholder count tight.
func multiQuery(q map[string][]string, keys ...string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, key := range keys {
		for _, raw := range q[key] {
			for _, v := range strings.Split(raw, ",") {
				v = strings.TrimSpace(v)
				if v == "" || seen[v] {
					continue
				}
				seen[v] = true
				out = append(out, v)
			}
		}
	}
	return out
}

// parseWindow accepts a human-friendly window spec ("15m", "1h", "24h",
// "7d") plus any other value time.ParseDuration understands. It returns 0
// for the empty string or a malformed input.
func parseWindow(raw string) time.Duration {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0
	}
	// time.ParseDuration handles "m"/"h" natively; it does not understand
	// "d" so we normalise the common "7d" / "30d" shorthands first.
	if strings.HasSuffix(raw, "d") {
		if n, err := time.ParseDuration(strings.TrimSuffix(raw, "d") + "h"); err == nil {
			return n * 24
		}
	}
	if d, err := time.ParseDuration(raw); err == nil {
		return d
	}
	return 0
}

// listEventFacets handles GET /v1/events/facets. Returns the distinct
// values + counts per facet dimension matching the filter. The response
// shape matches the FacetPanel TypeScript mirror exactly.
//
//	{
//	  "window": "1h",
//	  "since":  "...",
//	  "until":  "...",
//	  "facets": [ { "key": "source_app", "values": [...] }, ... ]
//	}
//
// When the caller supplies an explicit since/until the "window" field in
// the response mirrors whatever the client passed under ?window= so the
// Datadog-style "15m"/"1h"/"24h" dropdown stays round-trip stable; if no
// window was supplied the field is omitted.
func (s *Server) listEventFacets(w http.ResponseWriter, r *http.Request) {
	filter := parseLogFilter(r)
	facets, err := s.store.EventFacets(r.Context(), filter)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	body := map[string]any{
		"facets": facets,
	}
	if win := r.URL.Query().Get("window"); win != "" {
		body["window"] = win
	}
	if !filter.Since.IsZero() {
		body["since"] = filter.Since
	}
	if !filter.Until.IsZero() {
		body["until"] = filter.Until
	}
	writeJSON(w, http.StatusOK, body)
}

// listEventTimeseries handles GET /v1/events/timeseries. Returns evenly
// spaced buckets with a per-severity breakdown. The `step` param is a
// standard Go duration ("30s", "10m"), defaulting to a window-appropriate
// choice when omitted.
func (s *Server) listEventTimeseries(w http.ResponseWriter, r *http.Request) {
	filter := parseLogFilter(r)

	q := r.URL.Query()
	step := parseWindow(q.Get("step"))
	if step <= 0 {
		step = defaultStepForWindow(filter.Until.Sub(filter.Since))
	}

	buckets, err := s.store.EventTimeseries(r.Context(), filter, step)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	total, err := s.store.CountEvents(r.Context(), filter)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	body := map[string]any{
		"buckets": buckets,
		"step":    step.String(),
		"total":   total,
	}
	if win := q.Get("window"); win != "" {
		body["window"] = win
	}
	if !filter.Since.IsZero() {
		body["since"] = filter.Since
	}
	if !filter.Until.IsZero() {
		body["until"] = filter.Until
	}
	writeJSON(w, http.StatusOK, body)
}

// defaultStepForWindow picks a bucket width proportional to the window
// length so the resulting chart shows ~60-120 bars. Matches the spec:
//
//	≤ 1 min  → 1 s
//	≤ 5 min  → 5 s
//	≤ 1 h    → 30 s
//	≤ 6 h    → 2 min
//	≤ 24 h   → 10 min
//	≤ 7 d    → 1 h
//	otherwise → 6 h
func defaultStepForWindow(win time.Duration) time.Duration {
	switch {
	case win <= 0 || win <= time.Minute:
		return time.Second
	case win <= 5*time.Minute:
		return 5 * time.Second
	case win <= time.Hour:
		return 30 * time.Second
	case win <= 6*time.Hour:
		return 2 * time.Minute
	case win <= 24*time.Hour:
		return 10 * time.Minute
	case win <= 7*24*time.Hour:
		return time.Hour
	default:
		return 6 * time.Hour
	}
}

// getLiveBootstrap handles GET /v1/live/bootstrap — the consolidated first
// paint payload for the `/` Live dashboard. Before PR #37 the landing page
// fired ~7 independent HTTP requests (active turns + attention counts +
// recent events + 4 metric series); in practice the network round-trip
// sequence dominated LCP even on a warm DuckDB. This endpoint coalesces
// them into one.
//
// All four sub-fetches run sequentially in the same request goroutine —
// there is no benefit in parallelising because DuckDB serialises inside
// its own goroutine pool anyway and the queries are already cheap. The
// handler never returns a partial response: any sub-fetch error surfaces
// as 500 so the client's error boundary takes over.
func (s *Server) getLiveBootstrap(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	activeTurns, err := s.store.ListActiveTurnsFiltered(ctx, duckdb.TurnFilter{}, 40)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "active turns: "+err.Error())
		return
	}
	if activeTurns == nil {
		activeTurns = []duckdb.Turn{}
	}

	attentionCounts, err := s.store.CountAttentionFiltered(ctx, duckdb.TurnFilter{}, true)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "attention counts: "+err.Error())
		return
	}

	recentEvents, _, err := s.store.ListRecentLogs(ctx, duckdb.LogFilter{}, 40)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "recent events: "+err.Error())
		return
	}
	if recentEvents == nil {
		recentEvents = []duckdb.LogRow{}
	}

	// Four KPI sparklines — the same names/windows/kinds the existing
	// /v1/metrics/series handler ships with. We keep the window fixed
	// at 5 minutes with a 10 s step so the bootstrap payload stays
	// small; subsequent per-tile refreshes continue to use the
	// individual /v1/metrics/series endpoint when the user is
	// inspecting historical windows.
	metricsBody := map[string][]duckdb.MetricSeriesPoint{}
	for _, series := range []struct {
		key  string
		name string
		kind string
	}{
		{"active_turns", "apogee.turns.active", "gauge"},
		{"tools_rate", "apogee.tools.rate", "counter"},
		{"errors_rate", "apogee.errors.rate", "counter"},
		{"hitl_pending", "apogee.hitl.pending", "gauge"},
	} {
		points, mErr := s.store.GetMetricSeries(ctx, duckdb.MetricSeriesOptions{
			Name:   series.name,
			Window: 5 * time.Minute,
			Step:   10 * time.Second,
			Kind:   series.kind,
		})
		if mErr != nil {
			// Individual metric failures are non-fatal for the bootstrap
			// response — an empty slice is preferable to blowing up the
			// whole first paint. Log via the request logger.
			s.logger.Debug("bootstrap metric", "series", series.key, "err", mErr)
			points = []duckdb.MetricSeriesPoint{}
		}
		if points == nil {
			points = []duckdb.MetricSeriesPoint{}
		}
		metricsBody[series.key] = points
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"recent_turns":  activeTurns,
		"attention":     attentionCounts,
		"recent_events": recentEvents,
		"metrics":       metricsBody,
		"now":           time.Now().UTC(),
	})
}

// Unused cancellation guard: reserved so future handlers in this file can
// share a parent context without importing the package twice.
var _ = context.Background

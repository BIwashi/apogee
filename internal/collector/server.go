package collector

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/BIwashi/apogee/internal/attention"
	"github.com/BIwashi/apogee/internal/hitl"
	"github.com/BIwashi/apogee/internal/ingest"
	"github.com/BIwashi/apogee/internal/interventions"
	"github.com/BIwashi/apogee/internal/metrics"
	"github.com/BIwashi/apogee/internal/sse"
	"github.com/BIwashi/apogee/internal/store/duckdb"
	"github.com/BIwashi/apogee/internal/summarizer"
	"github.com/BIwashi/apogee/internal/telemetry"
	"github.com/BIwashi/apogee/internal/version"
	"github.com/BIwashi/apogee/internal/webassets"
)

// Server is the apogee collector HTTP server. It owns the chi router, the
// reconstructor, and a reference to the store. Use New to construct one.
type Server struct {
	cfg           Config
	store         *duckdb.Store
	reconstructor *ingest.Reconstructor
	hub           *sse.Hub
	router        chi.Router
	httpServer    *http.Server
	logger        *slog.Logger
	metrics       *metrics.Collector
	summarizer    *summarizer.Service
	hitl          *hitl.Service
	interventions *interventions.Service
	telemetry     *telemetry.Provider
	startedAt     time.Time
}

// New constructs a Server backed by an open store. The caller retains
// ownership of the store and is responsible for closing it after Shutdown.
func New(cfg Config, store *duckdb.Store, logger *slog.Logger) *Server {
	if logger == nil {
		logger = slog.Default()
	}
	hub := sse.NewHub(logger)
	rec := ingest.NewReconstructor(store, logger, nil)
	rec.Hub = hub

	// Telemetry: load config (TOML + env), build the tracer provider,
	// and wire its Tracer into the reconstructor. NewTracerProvider
	// returns a usable provider even when export is disabled — the
	// reconstructor's Tracer field tolerates a noop tracer the same
	// way it tolerates a real one.
	telCfg, err := telemetry.LoadConfig("")
	if err != nil {
		logger.Warn("telemetry: config load failed — using defaults", "err", err)
		telCfg = telemetry.Config{ServiceName: "apogee", Protocol: telemetry.ProtocolGRPC, SampleRatio: 1.0}
	}
	telProv, err := telemetry.NewTracerProvider(context.Background(), telCfg, logger)
	if err != nil {
		logger.Warn("telemetry: provider build failed — disabling export", "err", err)
	}
	if telProv != nil {
		rec.Tracer = telProv.Tracer()
	}

	// Wire the attention engine against the store-backed history reader.
	history := &attention.StoreHistory{DB: store}
	rec.Engine = attention.NewEngine(history)
	rec.HistoryWrite = history

	// Wire the summarizer. Config load is best-effort — a missing TOML
	// simply falls through to defaults. When the load fails outright we
	// log and continue with defaults so a bad config file never blocks
	// the collector from starting.
	summarizerCfg, err := summarizer.Load("")
	if err != nil {
		logger.Warn("summarizer: config load failed — using defaults", "err", err)
		summarizerCfg = summarizer.Default()
	}
	summarizerSvc := summarizer.NewService(summarizerCfg, store, hub, logger)
	rec.OnTurnClosed = func(turnID string) {
		summarizerSvc.Enqueue(turnID, summarizer.ReasonTurnClosed)
	}
	rec.OnSessionEnded = func(sessionID string) {
		summarizerSvc.EnqueueRollup(sessionID, summarizer.RollupReasonSessionEnd)
	}

	// Wire the HITL lifecycle owner. The reconstructor pushes hitl.requested
	// broadcasts via OnHITLRequested, the service handles auto-expiration
	// and the response HTTP endpoint.
	hitlSvc := hitl.New(store, hub, hitl.DefaultConfig(), logger)
	hitlSvc.CloseHITLSpan = func(ctx context.Context, ev duckdb.HITLEvent) {
		rec.CloseHITLSpan(ctx, ev)
	}
	rec.OnHITLRequested = func(ev duckdb.HITLEvent) {
		hitlSvc.BroadcastRequested(ev)
	}

	// Wire the interventions lifecycle owner. Submit/cancel flow through
	// the HTTP layer, Claim is driven by the Python hook, and the sweeper
	// ticker is started inside Server.Run. Config load is best-effort —
	// a missing TOML falls through to defaults.
	interventionCfg, err := interventions.Load("")
	if err != nil {
		logger.Warn("interventions: config load failed — using defaults", "err", err)
		interventionCfg = interventions.DefaultConfig()
	}
	interventionSvc := interventions.NewService(interventionCfg, store, hub, logger)
	rec.InterventionsSvc = interventionSvc

	if webassets.IsPlaceholder() {
		logger.Warn(
			"webassets: embedded dashboard is the placeholder stub — run `make web-build` or install a release binary for the full UI",
		)
	}

	s := &Server{
		cfg:           cfg,
		store:         store,
		reconstructor: rec,
		hub:           hub,
		logger:        logger,
		metrics:       metrics.New(store, metrics.DefaultInterval, logger),
		summarizer:    summarizerSvc,
		hitl:          hitlSvc,
		interventions: interventionSvc,
		telemetry:     telProv,
		startedAt:     time.Now(),
	}
	s.router = s.buildRouter()
	return s
}

// Hub exposes the SSE hub for tests.
func (s *Server) Hub() *sse.Hub { return s.hub }

// Reconstructor exposes the reconstructor for tests.
func (s *Server) Reconstructor() *ingest.Reconstructor { return s.reconstructor }

// Router exposes the chi router for tests.
func (s *Server) Router() chi.Router { return s.router }

// StartBackground launches the collector's background workers (metrics
// sampler, summarizer, HITL ticker, intervention sweeper) without binding
// an HTTP listener. It is used by embedding hosts like the Wails desktop
// shell that own their own transport and only need the router plus the
// side-effect goroutines. All workers are scoped to ctx — cancel ctx to
// stop them. StartBackground is non-blocking.
func (s *Server) StartBackground(ctx context.Context) {
	if s.metrics != nil {
		go func() {
			if err := s.metrics.Run(ctx); err != nil {
				s.logger.Debug("metrics collector stopped", "err", err)
			}
		}()
	}
	if s.summarizer != nil {
		s.summarizer.Start(ctx)
	}
	if s.hitl != nil {
		s.hitl.Start(ctx)
	}
	if s.interventions != nil {
		s.interventions.Start(ctx)
	}
}

// StopBackground performs the explicit shutdown steps for the background
// processing started by StartBackground: it stops the summarizer and
// intervention sweeper (both of which own their own worker goroutines
// behind sync.WaitGroup) and flushes the OTel span processor.
//
// Callers must additionally cancel the ctx they passed to StartBackground
// to stop the ctx-scoped goroutines — the metrics sampler and the HITL
// ticker both terminate on ctx.Done rather than through an explicit Stop.
// Safe to call multiple times.
func (s *Server) StopBackground(ctx context.Context) {
	if s.summarizer != nil {
		s.summarizer.Stop()
	}
	if s.interventions != nil {
		s.interventions.Stop()
	}
	if s.telemetry != nil && s.telemetry.Shutdown != nil {
		_ = s.telemetry.Shutdown(ctx)
	}
}

// Run starts the HTTP server and blocks until ctx is cancelled. On cancel the
// server is shut down gracefully with a 5 s deadline.
func (s *Server) Run(ctx context.Context) error {
	s.httpServer = &http.Server{
		Addr:              s.cfg.HTTPAddr,
		Handler:           s.router,
		ReadHeaderTimeout: 5 * time.Second,
	}

	workerCtx, cancelWorkers := context.WithCancel(ctx)
	defer cancelWorkers()
	s.StartBackground(workerCtx)

	errCh := make(chan error, 1)
	go func() {
		s.logger.Info("collector listening", "addr", s.cfg.HTTPAddr)
		err := s.httpServer.ListenAndServe()
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
			return
		}
		errCh <- nil
	}()
	select {
	case <-ctx.Done():
		// ctx is already done, so workerCtx (derived from ctx) has
		// propagated cancellation — explicit cancel here is for
		// symmetry with the errCh branch and to short-circuit the
		// deferred cancelWorkers.
		cancelWorkers()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = s.httpServer.Shutdown(shutdownCtx)
		s.StopBackground(shutdownCtx)
		return nil
	case err := <-errCh:
		// ctx is still live here (ListenAndServe failed on its own),
		// so workerCtx is also still live. Cancel it before calling
		// StopBackground so the metrics sampler and HITL ticker don't
		// keep writing while telemetry is being flushed.
		cancelWorkers()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		s.StopBackground(shutdownCtx)
		return err
	}
}

func (s *Server) buildRouter() chi.Router {
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Recoverer)
	r.Use(s.requestLogger)
	r.Use(s.cors)

	handler := ingest.NewHandler(s.reconstructor, s.logger)

	r.Get("/v1/healthz", s.healthz)
	r.Get("/v1/info", s.getInfo)
	r.Get("/v1/preferences", s.listPreferences)
	r.Patch("/v1/preferences", s.patchPreferences)
	r.Delete("/v1/preferences", s.deletePreferences)
	r.Get("/v1/models", s.listModels)
	r.Get("/v1/telemetry/status", s.telemetryStatus)
	r.Get("/v1/agents/recent", s.listRecentAgents)
	r.Get("/v1/insights/overview", s.getInsightsOverview)
	r.Post("/v1/events", handler.ReceiveEvent)
	r.Get("/v1/sessions/recent", s.listRecentSessions)
	r.Get("/v1/sessions/search", s.searchSessions)
	r.Get("/v1/sessions/{id}", s.getSession)
	r.Get("/v1/sessions/{id}/summary", s.getSessionSummary)
	r.Get("/v1/sessions/{id}/turns", s.listSessionTurns)
	r.Get("/v1/sessions/{id}/logs", s.listSessionLogs)
	r.Get("/v1/sessions/{id}/rollup", s.getSessionRollup)
	r.Post("/v1/sessions/{id}/rollup", s.postSessionRollup)
	r.Post("/v1/sessions/{id}/narrative", s.postSessionNarrative)
	r.Get("/v1/turns/recent", s.listRecentTurns)
	r.Get("/v1/turns/active", s.listActiveTurns)
	r.Get("/v1/turns/{turn_id}", s.getTurn)
	r.Get("/v1/turns/{turn_id}/spans", s.listTurnSpans)
	r.Get("/v1/turns/{turn_id}/logs", s.listTurnLogs)
	r.Get("/v1/turns/{turn_id}/attention", s.getTurnAttention)
	r.Get("/v1/turns/{turn_id}/recap", s.getTurnRecap)
	r.Post("/v1/turns/{turn_id}/recap", s.postTurnRecap)
	r.Get("/v1/attention/counts", s.getAttentionCounts)
	r.Get("/v1/metrics/series", s.getMetricsSeries)
	r.Get("/v1/filter-options", s.getFilterOptions)
	r.Get("/v1/events/stream", s.streamEvents)
	r.Get("/v1/events/recent", s.listRecentEvents)
	r.Get("/v1/events/facets", s.listEventFacets)
	r.Get("/v1/events/timeseries", s.listEventTimeseries)
	r.Get("/v1/live/bootstrap", s.getLiveBootstrap)
	r.Get("/v1/hitl", s.listHITL)
	r.Get("/v1/hitl/{hitl_id}", s.getHITL)
	r.Post("/v1/hitl/{hitl_id}/respond", s.respondHITL)
	r.Get("/v1/sessions/{id}/hitl/pending", s.listPendingHITLBySession)
	r.Get("/v1/turns/{turn_id}/hitl", s.listHITLByTurn)

	// Operator intervention routes.
	r.Post("/v1/interventions", s.submitIntervention)
	r.Get("/v1/interventions/{id}", s.getIntervention)
	r.Post("/v1/interventions/{id}/cancel", s.cancelIntervention)
	r.Post("/v1/interventions/{id}/delivered", s.deliveredIntervention)
	r.Post("/v1/interventions/{id}/consumed", s.consumedIntervention)
	r.Post("/v1/sessions/{id}/interventions/claim", s.claimSessionIntervention)
	r.Get("/v1/sessions/{id}/interventions", s.listSessionInterventions)
	r.Get("/v1/sessions/{id}/interventions/pending", s.listPendingSessionInterventions)
	r.Get("/v1/turns/{turn_id}/interventions", s.listTurnInterventions)

	// Embedded Next.js static export. The SPA handler is mounted as the
	// 404 fallback so every /v1/* route above takes precedence. Anything
	// else — `/`, `/sessions/`, `/session/`, `/turn/`, `/_next/static/*` —
	// is served from the embedded FS, with SPA fallback to `index.html`
	// for routes that do not map to a file.
	spa := spaHandler(webassets.Assets())
	r.NotFound(func(w http.ResponseWriter, req *http.Request) {
		// Guard against API paths accidentally falling through. Returning
		// JSON 404 for /v1/* keeps the API contract clean.
		if strings.HasPrefix(req.URL.Path, "/v1/") {
			writeJSONError(w, http.StatusNotFound, "not found")
			return
		}
		spa.ServeHTTP(w, req)
	})
	return r
}

func (s *Server) healthz(w http.ResponseWriter, r *http.Request) {
	// JSON variant carries a small telemetry summary so operators can
	// see at a glance whether OTLP export is wired up. Curl with
	// `-H 'Accept: application/json'` to opt in. Plain `curl` keeps
	// the historical text/plain shape so existing health probes do
	// not break.
	if !strings.Contains(r.Header.Get("Accept"), "application/json") {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprintln(w, "ok")
		return
	}
	body := map[string]any{
		"ok":            true,
		"otel_enabled":  false,
		"otel_endpoint": "",
		"otel_protocol": "",
	}
	if s.telemetry != nil {
		body["otel_enabled"] = s.telemetry.Enabled
		body["otel_endpoint"] = s.telemetry.Cfg.Endpoint
		body["otel_protocol"] = string(s.telemetry.Cfg.Protocol)
	}
	writeJSON(w, http.StatusOK, body)
}

// telemetryStatus reports the OTel exporter configuration plus the
// running spans-exported counter. Read-only; safe to poll.
func (s *Server) telemetryStatus(w http.ResponseWriter, _ *http.Request) {
	body := map[string]any{
		"enabled":             false,
		"endpoint":            "",
		"protocol":            "",
		"service_name":        "",
		"service_version":     "",
		"service_instance_id": "",
		"sample_ratio":        0.0,
		"spans_exported_total": uint64(0),
	}
	if s.telemetry != nil {
		body["enabled"] = s.telemetry.Enabled
		body["endpoint"] = s.telemetry.Cfg.Endpoint
		body["protocol"] = string(s.telemetry.Cfg.Protocol)
		body["service_name"] = s.telemetry.Cfg.ServiceName
		body["service_version"] = s.telemetry.Cfg.ServiceVersion
		body["service_instance_id"] = s.telemetry.Cfg.ServiceInstanceID
		body["sample_ratio"] = s.telemetry.Cfg.SampleRatio
		if s.telemetry.SpansExported != nil {
			body["spans_exported_total"] = s.telemetry.SpansExported.Load()
		}
	}
	writeJSON(w, http.StatusOK, body)
}

func (s *Server) listRecentSessions(w http.ResponseWriter, r *http.Request) {
	out, err := s.store.ListRecentSessions(r.Context(), 100)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if out == nil {
		out = []duckdb.Session{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"sessions": out})
}

func (s *Server) getSession(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	sess, err := s.store.GetSession(r.Context(), id)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if sess == nil {
		writeJSONError(w, http.StatusNotFound, "session not found")
		return
	}
	writeJSON(w, http.StatusOK, sess)
}

func (s *Server) listSessionTurns(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	out, err := s.store.ListSessionTurns(r.Context(), id, 200)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if out == nil {
		out = []duckdb.Turn{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"turns": out})
}

// parseTurnFilter extracts the canonical filter query params shared by the
// recent/active turns, attention counts, and metrics series endpoints. All
// fields are optional.
func parseTurnFilter(r *http.Request) duckdb.TurnFilter {
	q := r.URL.Query()
	f := duckdb.TurnFilter{
		SessionID: q.Get("session_id"),
		SourceApp: q.Get("source_app"),
	}
	if raw := q.Get("since"); raw != "" {
		if t, err := time.Parse(time.RFC3339, raw); err == nil {
			f.Since = &t
		}
	}
	if raw := q.Get("until"); raw != "" {
		if t, err := time.Parse(time.RFC3339, raw); err == nil {
			f.Until = &t
		}
	}
	return f
}

func (s *Server) listRecentTurns(w http.ResponseWriter, r *http.Request) {
	limit := parseLimit(r, 100, 500)
	filter := parseTurnFilter(r)
	out, err := s.store.ListRecentTurnsFiltered(r.Context(), filter, limit)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if out == nil {
		out = []duckdb.Turn{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"turns": out})
}

func (s *Server) listActiveTurns(w http.ResponseWriter, r *http.Request) {
	limit := parseLimit(r, 200, 500)
	filter := parseTurnFilter(r)
	out, err := s.store.ListActiveTurnsFiltered(r.Context(), filter, limit)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if out == nil {
		out = []duckdb.Turn{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"turns": out})
}

func (s *Server) getAttentionCounts(w http.ResponseWriter, r *http.Request) {
	includeEnded := r.URL.Query().Get("include") == "ended"
	filter := parseTurnFilter(r)
	counts, err := s.store.CountAttentionFiltered(r.Context(), filter, includeEnded)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, counts)
}

func (s *Server) searchSessions(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	limit := parseLimit(r, 50, 200)
	hits, err := s.store.SearchSessions(r.Context(), q.Get("q"), limit)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"sessions": hits})
}

func (s *Server) getSessionSummary(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	sum, err := s.store.GetSessionSummary(r.Context(), id)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if sum == nil {
		writeJSONError(w, http.StatusNotFound, "session not found")
		return
	}
	writeJSON(w, http.StatusOK, sum)
}

func (s *Server) getMetricsSeries(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	name := q.Get("name")
	if name == "" {
		writeJSONError(w, http.StatusBadRequest, "missing metric name")
		return
	}
	window, _ := time.ParseDuration(coalesceQuery(q.Get("window"), "5m"))
	step, _ := time.ParseDuration(coalesceQuery(q.Get("step"), "10s"))
	kind := q.Get("kind")
	if kind == "" {
		kind = "gauge"
	}
	points, err := s.store.GetMetricSeries(r.Context(), duckdb.MetricSeriesOptions{
		Name:      name,
		Window:    window,
		Step:      step,
		Kind:      kind,
		SessionID: q.Get("session_id"),
		SourceApp: q.Get("source_app"),
	})
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	if points == nil {
		points = []duckdb.MetricSeriesPoint{}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"name":   name,
		"window": window.String(),
		"step":   step.String(),
		"kind":   kind,
		"points": points,
	})
}

// parseLimit reads ?limit=N and clamps to [1, max]. Falls back to def when
// the query param is missing or malformed.
func parseLimit(r *http.Request, def, max int) int {
	raw := r.URL.Query().Get("limit")
	if raw == "" {
		return def
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 {
		return def
	}
	if n > max {
		return max
	}
	return n
}

func coalesceQuery(v, fallback string) string {
	if v == "" {
		return fallback
	}
	return v
}

func (s *Server) getTurn(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "turn_id")
	t, err := s.store.GetTurn(r.Context(), id)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if t == nil {
		writeJSONError(w, http.StatusNotFound, "turn not found")
		return
	}
	writeJSON(w, http.StatusOK, t)
}

func (s *Server) listTurnSpans(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "turn_id")
	spans, err := s.store.GetSpansByTurn(r.Context(), id)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if spans == nil {
		spans = []duckdb.SpanRow{}
	}

	// Compute phase segments only when we have a turn row to anchor the
	// timeline against. Missing-turn returns just the spans (and an empty
	// phases slice) so the swim lane degrades gracefully.
	var phases []attention.PhaseSegment
	turn, err := s.store.GetTurn(r.Context(), id)
	if err == nil && turn != nil {
		var endedAt time.Time
		if turn.EndedAt != nil {
			endedAt = *turn.EndedAt
		}
		phases = attention.PhaseSegments(spans, turn.StartedAt, endedAt)
	}
	if phases == nil {
		phases = []attention.PhaseSegment{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"spans": spans, "phases": phases})
}

func (s *Server) listTurnLogs(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "turn_id")
	limit := parseLimit(r, 500, 5000)
	logs, err := s.store.ListLogsByTurn(r.Context(), id, limit)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if logs == nil {
		logs = []duckdb.LogRow{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"logs": logs})
}

// listRecentEvents is the cursor-paginated raw-events browser endpoint that
// powers the `/events` web route. Newest first; pass the previous response's
// `next_before` value as `?before=` to fetch the next page.
//
// Query params (all optional):
//   - limit       — page size, default 50, max 500
//   - before      — exclusive id cursor; rows with id < before are returned
//   - session_id  — restrict to one Claude Code session
//   - source_app  — restrict to one labelled environment
//   - type        — restrict to one hook event name
//
// Response shape:
//
//	{ "events": [...LogRow], "next_before": int|null, "has_more": bool }
//
// `has_more` is true when the page is full — the client uses it to decide
// whether to enable the "Next" button. `next_before` is null when no rows
// were returned at all, otherwise it is the smallest id in the batch.
func (s *Server) listRecentEvents(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	limit := parseLimit(r, 50, 500)
	filter := duckdb.LogFilter{
		SessionID: q.Get("session_id"),
		SourceApp: q.Get("source_app"),
		Type:      q.Get("type"),
	}
	if raw := q.Get("before"); raw != "" {
		if n, err := strconv.ParseInt(raw, 10, 64); err == nil && n > 0 {
			filter.Before = n
		}
	}
	rows, nextCursor, err := s.store.ListRecentLogs(r.Context(), filter, limit)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if rows == nil {
		rows = []duckdb.LogRow{}
	}
	body := map[string]any{
		"events":   rows,
		"has_more": len(rows) == limit,
	}
	if nextCursor > 0 {
		body["next_before"] = nextCursor
	} else {
		body["next_before"] = nil
	}
	writeJSON(w, http.StatusOK, body)
}

func (s *Server) listSessionLogs(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	limit := parseLimit(r, 200, 1000)
	logs, err := s.store.ListLogsBySession(r.Context(), id, limit)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if logs == nil {
		logs = []duckdb.LogRow{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"logs": logs})
}

// getTurnAttention surfaces the engine decision currently stored on the turn
// row. It is a denormalised lookup — UpdateTurnAttention writes to the same
// columns this handler reads from. signals are the JSON-decoded slice.
func (s *Server) getTurnAttention(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "turn_id")
	t, err := s.store.GetTurn(r.Context(), id)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if t == nil {
		writeJSONError(w, http.StatusNotFound, "turn not found")
		return
	}
	var signals []map[string]any
	if t.AttentionSignalsJSON != "" {
		_ = json.Unmarshal([]byte(t.AttentionSignalsJSON), &signals)
	}
	if signals == nil {
		signals = []map[string]any{}
	}
	var updatedAt time.Time
	if t.PhaseSince != nil {
		updatedAt = *t.PhaseSince
	} else {
		updatedAt = t.StartedAt
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"turn_id":    t.TurnID,
		"state":      t.AttentionState,
		"tone":       t.AttentionTone,
		"reason":     t.AttentionReason,
		"score":      t.AttentionScore,
		"phase":      t.Phase,
		"signals":    signals,
		"updated_at": updatedAt,
	})
}

// getTurnRecap returns the stored recap for a turn. When the turn row is
// present but no recap has been generated yet the response is
// {"recap": null}. 404 only when the turn itself is missing.
func (s *Server) getTurnRecap(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "turn_id")
	turn, err := s.store.GetTurn(r.Context(), id)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if turn == nil {
		writeJSONError(w, http.StatusNotFound, "turn not found")
		return
	}
	if turn.RecapJSON == "" {
		writeJSON(w, http.StatusOK, map[string]any{"recap": nil})
		return
	}
	var recap json.RawMessage = json.RawMessage(turn.RecapJSON)
	out := map[string]any{"recap": recap}
	if turn.RecapGeneratedAt != nil {
		out["generated_at"] = *turn.RecapGeneratedAt
	}
	if turn.RecapModel != "" {
		out["model"] = turn.RecapModel
	}
	writeJSON(w, http.StatusOK, out)
}

// postTurnRecap enqueues a manual re-recap for a turn. Returns 202
// immediately; the worker picks up the job and eventually broadcasts a
// turn.updated SSE when the recap lands.
func (s *Server) postTurnRecap(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "turn_id")
	turn, err := s.store.GetTurn(r.Context(), id)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if turn == nil {
		writeJSONError(w, http.StatusNotFound, "turn not found")
		return
	}
	if s.summarizer != nil {
		s.summarizer.Enqueue(id, summarizer.ReasonManual)
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"turn_id": id, "queued": true})
}

// getSessionRollup returns the stored rollup for a session. When the session
// row exists but no rollup has been generated yet the response is
// {"rollup": null}. 404 only when the session itself is missing.
func (s *Server) getSessionRollup(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	sess, err := s.store.GetSession(r.Context(), id)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if sess == nil {
		writeJSONError(w, http.StatusNotFound, "session not found")
		return
	}
	row, ok, err := s.store.GetSessionRollup(r.Context(), id)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !ok {
		writeJSON(w, http.StatusOK, map[string]any{
			"rollup":       nil,
			"generated_at": nil,
			"model":        nil,
		})
		return
	}
	var rollup json.RawMessage = json.RawMessage(row.RollupJSON)
	writeJSON(w, http.StatusOK, map[string]any{
		"rollup":       rollup,
		"generated_at": row.GeneratedAt,
		"model":        row.Model,
	})
}

// postSessionRollup enqueues a manual rollup for a session. Returns 202
// immediately; the rollup worker eventually broadcasts a session.updated
// SSE event when the digest lands.
func (s *Server) postSessionRollup(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	sess, err := s.store.GetSession(r.Context(), id)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if sess == nil {
		writeJSONError(w, http.StatusNotFound, "session not found")
		return
	}
	if s.summarizer != nil {
		s.summarizer.EnqueueRollup(id, summarizer.RollupReasonManual)
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"session_id": id, "enqueued": true})
}

// postSessionNarrative enqueues a manual tier-3 phase narrative refresh
// for a session. Returns 202 immediately; the narrative worker broadcasts
// a session.updated SSE event when the phases land.
func (s *Server) postSessionNarrative(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	sess, err := s.store.GetSession(r.Context(), id)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if sess == nil {
		writeJSONError(w, http.StatusNotFound, "session not found")
		return
	}
	if s.summarizer != nil {
		s.summarizer.EnqueueNarrative(id, summarizer.NarrativeReasonManual)
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"session_id": id, "enqueued": true})
}

func (s *Server) getFilterOptions(w http.ResponseWriter, r *http.Request) {
	opts, err := s.store.GetFilterOptions(r.Context())
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, opts)
}

// hitlSnapshotJSON is a thin wrapper that calls into the SSE projection
// helper so HTTP responses share the exact same JSON shape as broadcast
// payloads.
func hitlSnapshotJSON(ev duckdb.HITLEvent) sse.HITLSnapshot {
	return sse.SnapshotFromHITL(ev)
}

func (s *Server) getHITL(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "hitl_id")
	ev, ok, err := s.store.GetHITL(r.Context(), id)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !ok {
		writeJSONError(w, http.StatusNotFound, "hitl event not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"hitl": hitlSnapshotJSON(ev)})
}

func (s *Server) listHITL(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	limit := parseLimit(r, 100, 500)
	out, err := s.store.ListRecentHITL(r.Context(), duckdb.HITLFilter{
		SessionID: q.Get("session_id"),
		Status:    q.Get("status"),
		Kind:      q.Get("kind"),
	}, limit)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"hitl": projectHITLList(out)})
}

func (s *Server) listPendingHITLBySession(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	out, err := s.store.ListPendingHITLBySession(r.Context(), id)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"hitl": projectHITLList(out)})
}

func (s *Server) listHITLByTurn(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "turn_id")
	out, err := s.store.ListHITLByTurn(r.Context(), id)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"hitl": projectHITLList(out)})
}

func (s *Server) respondHITL(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "hitl_id")
	var body duckdb.HITLResponse
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid json body")
		return
	}
	if body.Decision == "" {
		writeJSONError(w, http.StatusBadRequest, "decision is required")
		return
	}
	if s.hitl == nil {
		writeJSONError(w, http.StatusServiceUnavailable, "hitl service not configured")
		return
	}
	updated, err := s.hitl.Respond(r.Context(), id, body)
	if err != nil {
		switch {
		case errors.Is(err, duckdb.ErrHITLNotFound):
			writeJSONError(w, http.StatusNotFound, "hitl event not found")
		case errors.Is(err, duckdb.ErrHITLAlreadyResponded):
			writeJSONError(w, http.StatusConflict, "hitl event already finalised")
		default:
			writeJSONError(w, http.StatusInternalServerError, err.Error())
		}
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"hitl": hitlSnapshotJSON(updated)})
}

func projectHITLList(rows []duckdb.HITLEvent) []sse.HITLSnapshot {
	out := make([]sse.HITLSnapshot, 0, len(rows))
	for _, row := range rows {
		out = append(out, sse.SnapshotFromHITL(row))
	}
	return out
}

// listRecentAgents returns the aggregate per-agent view used by the
// /agents dashboard page. The shape is `{ "agents": [...] }` to match the
// other list endpoints.
func (s *Server) listRecentAgents(w http.ResponseWriter, r *http.Request) {
	limit := parseLimit(r, 100, 500)
	out, err := s.store.ListRecentAgents(r.Context(), limit)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if out == nil {
		out = []duckdb.Agent{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"agents": out})
}

// getInsightsOverview returns the aggregate block rendered by the
// /insights dashboard page. Uses a 24h rolling window.
func (s *Server) getInsightsOverview(w http.ResponseWriter, r *http.Request) {
	since := time.Now().Add(-24 * time.Hour)
	if raw := r.URL.Query().Get("since"); raw != "" {
		if t, err := time.Parse(time.RFC3339, raw); err == nil {
			since = t
		}
	}
	out, err := s.store.InsightsOverview(r.Context(), since)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, out)
}

// getInfo returns a compact snapshot of the collector's build metadata
// plus the current OTel export state. Used by the /settings page.
func (s *Server) getInfo(w http.ResponseWriter, _ *http.Request) {
	body := map[string]any{
		"name":           "apogee",
		"version":        version.Version,
		"commit":         version.Commit,
		"build_date":     version.BuildDate,
		"otel_enabled":   false,
		"otel_endpoint":  "",
		"collector_addr": s.cfg.HTTPAddr,
		"uptime_seconds": int64(time.Since(s.startedAt).Seconds()),
	}
	if s.telemetry != nil {
		body["otel_enabled"] = s.telemetry.Enabled
		body["otel_endpoint"] = s.telemetry.Cfg.Endpoint
	}
	writeJSON(w, http.StatusOK, body)
}

func (s *Server) requestLogger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
		next.ServeHTTP(ww, r)
		s.logger.Info("http",
			"method", r.Method,
			"path", r.URL.Path,
			"status", ww.Status(),
			"bytes", ww.BytesWritten(),
			"dur", time.Since(start).String(),
		)
	})
}

func (s *Server) cors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PATCH, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func writeJSONError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

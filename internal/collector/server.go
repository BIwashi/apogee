package collector

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/BIwashi/apogee/internal/attention"
	"github.com/BIwashi/apogee/internal/ingest"
	"github.com/BIwashi/apogee/internal/metrics"
	"github.com/BIwashi/apogee/internal/sse"
	"github.com/BIwashi/apogee/internal/store/duckdb"
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

	// Wire the attention engine against the store-backed history reader.
	history := &attention.StoreHistory{DB: store}
	rec.Engine = attention.NewEngine(history)
	rec.HistoryWrite = history

	s := &Server{
		cfg:           cfg,
		store:         store,
		reconstructor: rec,
		hub:           hub,
		logger:        logger,
		metrics:       metrics.New(store, metrics.DefaultInterval, logger),
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

// Run starts the HTTP server and blocks until ctx is cancelled. On cancel the
// server is shut down gracefully with a 5 s deadline.
func (s *Server) Run(ctx context.Context) error {
	s.httpServer = &http.Server{
		Addr:              s.cfg.HTTPAddr,
		Handler:           s.router,
		ReadHeaderTimeout: 5 * time.Second,
	}

	// Start the internal metrics sampler. It writes one batch of rows into
	// metric_points per tick so the dashboard sparklines stay fresh.
	metricsCtx, stopMetrics := context.WithCancel(ctx)
	defer stopMetrics()
	if s.metrics != nil {
		go func() {
			if err := s.metrics.Run(metricsCtx); err != nil {
				s.logger.Debug("metrics collector stopped", "err", err)
			}
		}()
	}

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
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = s.httpServer.Shutdown(shutdownCtx)
		return nil
	case err := <-errCh:
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
	r.Post("/v1/events", handler.ReceiveEvent)
	r.Get("/v1/sessions/recent", s.listRecentSessions)
	r.Get("/v1/sessions/{id}", s.getSession)
	r.Get("/v1/sessions/{id}/turns", s.listSessionTurns)
	r.Get("/v1/sessions/{id}/logs", s.listSessionLogs)
	r.Get("/v1/turns/recent", s.listRecentTurns)
	r.Get("/v1/turns/active", s.listActiveTurns)
	r.Get("/v1/turns/{turn_id}", s.getTurn)
	r.Get("/v1/turns/{turn_id}/spans", s.listTurnSpans)
	r.Get("/v1/turns/{turn_id}/logs", s.listTurnLogs)
	r.Get("/v1/turns/{turn_id}/attention", s.getTurnAttention)
	r.Get("/v1/attention/counts", s.getAttentionCounts)
	r.Get("/v1/metrics/series", s.getMetricsSeries)
	r.Get("/v1/filter-options", s.getFilterOptions)
	r.Get("/v1/events/stream", s.streamEvents)
	return r
}

func (s *Server) healthz(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain")
	w.WriteHeader(http.StatusOK)
	_, _ = fmt.Fprintln(w, "ok")
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

func (s *Server) listRecentTurns(w http.ResponseWriter, r *http.Request) {
	limit := parseLimit(r, 100, 500)
	out, err := s.store.ListRecentTurns(r.Context(), limit)
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
	out, err := s.store.ListActiveTurns(r.Context(), limit)
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
	counts, err := s.store.CountAttention(r.Context(), includeEnded)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, counts)
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
		Name:   name,
		Window: window,
		Step:   step,
		Kind:   kind,
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

func (s *Server) getFilterOptions(w http.ResponseWriter, r *http.Request) {
	opts, err := s.store.GetFilterOptions(r.Context())
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, opts)
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
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
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

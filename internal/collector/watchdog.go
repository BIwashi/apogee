package collector

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/BIwashi/apogee/internal/sse"
	"github.com/BIwashi/apogee/internal/store/duckdb"
)

// listWatchdogSignals serves GET /v1/watchdog/signals. Query params:
//
//	status      — "unacked" to restrict to unacknowledged rows
//	severity    — one of "info" | "warning" | "critical"
//	limit       — 1..200 (default 50)
//
// Response shape mirrors web/app/lib/api-types.ts :: WatchdogListResponse —
// { "signals": [WatchdogSnapshot, ...] }. Rows are ordered newest-first.
func (s *Server) listWatchdogSignals(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	limit := parseLimit(r, 50, 200)
	filter := duckdb.WatchdogListFilter{
		OnlyUnacked: q.Get("status") == "unacked",
		Severity:    q.Get("severity"),
	}
	rows, err := s.store.ListWatchdogSignals(r.Context(), filter, limit)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	out := make([]sse.WatchdogSnapshot, 0, len(rows))
	for _, row := range rows {
		out = append(out, sse.SnapshotFromWatchdog(row))
	}
	writeJSON(w, http.StatusOK, map[string]any{"signals": out})
}

// ackWatchdogSignal serves POST /v1/watchdog/signals/{id}/ack. Idempotent —
// a second ack on the same row is a no-op that returns 200 with the
// current state.
func (s *Server) ackWatchdogSignal(w http.ResponseWriter, r *http.Request) {
	raw := chi.URLParam(r, "id")
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || id <= 0 {
		writeJSONError(w, http.StatusBadRequest, "invalid id")
		return
	}
	updated, err := s.store.AckWatchdogSignal(r.Context(), id, time.Now().UTC())
	if err != nil {
		if errors.Is(err, duckdb.ErrWatchdogSignalNotFound) {
			writeJSONError(w, http.StatusNotFound, "watchdog signal not found")
			return
		}
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"signal": sse.SnapshotFromWatchdog(updated)})
}

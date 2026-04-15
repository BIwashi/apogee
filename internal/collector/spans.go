package collector

import (
	"net/http"

	"github.com/go-chi/chi/v5"
)

// getSpanDetail serves GET /v1/spans/:trace_id/:span_id/detail and powers
// the cross-cutting SpanDrawer introduced in PR #36. Returns the requested
// span plus its parent (nil for trace roots) and direct children so the
// drawer's Parent tab can render click-through navigation without a second
// network round-trip. All three slices come from the existing spans table.
func (s *Server) getSpanDetail(w http.ResponseWriter, r *http.Request) {
	traceID := chi.URLParam(r, "trace_id")
	spanID := chi.URLParam(r, "span_id")
	if traceID == "" || spanID == "" {
		writeJSONError(w, http.StatusBadRequest, "missing trace or span id")
		return
	}
	detail, err := s.store.GetSpanDetail(r.Context(), traceID, spanID)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if detail == nil {
		writeJSONError(w, http.StatusNotFound, "span not found")
		return
	}
	writeJSON(w, http.StatusOK, detail)
}

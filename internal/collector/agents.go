package collector

import (
	"net/http"

	"github.com/go-chi/chi/v5"
)

// getAgentDetail serves GET /v1/agents/:id/detail and powers the
// cross-cutting AgentDrawer introduced in PR #36. The handler delegates to
// the read-only `GetAgentDetail` helper which aggregates the agent row, the
// last 20 turns the agent participated in, a tool histogram, and the agent's
// parent + direct children. All four slices are computed from the existing
// `spans` and `turns` tables; no schema migration is required.
func (s *Server) getAgentDetail(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		writeJSONError(w, http.StatusBadRequest, "missing agent id")
		return
	}
	detail, err := s.store.GetAgentDetail(r.Context(), id)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if detail == nil {
		writeJSONError(w, http.StatusNotFound, "agent not found")
		return
	}
	writeJSON(w, http.StatusOK, detail)
}

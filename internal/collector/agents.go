package collector

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/BIwashi/apogee/internal/summarizer"
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

// summarizeAgent serves POST /v1/agents/:id/summarize and queues the agent
// for an immediate label refresh, bypassing the staleness check. Returns 202
// once the job is enqueued — the actual summary lands asynchronously and the
// frontend re-fetches via SSE-driven SWR.
func (s *Server) summarizeAgent(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		writeJSONError(w, http.StatusBadRequest, "missing agent id")
		return
	}
	// Need the session id to key the summary row. Fetching the agent
	// detail row is the cheapest way to get it.
	detail, err := s.store.GetAgentDetail(r.Context(), id)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if detail == nil {
		writeJSONError(w, http.StatusNotFound, "agent not found")
		return
	}
	if s.summarizer == nil {
		writeJSONError(w, http.StatusServiceUnavailable, "summarizer is disabled")
		return
	}
	s.summarizer.EnqueueAgentSummary(id, detail.Agent.SessionID, summarizer.AgentSummaryReasonManual)
	writeJSON(w, http.StatusAccepted, map[string]string{
		"status":     "queued",
		"agent_id":   id,
		"session_id": detail.Agent.SessionID,
	})
}

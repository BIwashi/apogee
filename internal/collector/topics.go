package collector

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/BIwashi/apogee/internal/store/duckdb"
)

// getSessionTopics serves GET /v1/sessions/:id/topics. Returns the
// per-session topic forest in opened-at chronological order so the
// Mission UI can render one Mission Goal banner per topic.
func (s *Server) getSessionTopics(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		writeJSONError(w, http.StatusBadRequest, "missing session id")
		return
	}
	topics, err := s.store.ListSessionTopics(r.Context(), id)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	// Force a non-nil slice so the JSON encodes [] (not null) on the
	// empty path — keeps the typescript client unconditional.
	if topics == nil {
		topics = []duckdb.SessionTopic{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"topics": topics})
}

// getSessionTopicTransitions serves GET /v1/sessions/:id/topic-transitions.
// Returns the chronological audit trail (oldest first) so the Topics
// tab can render the per-turn classifier-decision table.
func (s *Server) getSessionTopicTransitions(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		writeJSONError(w, http.StatusBadRequest, "missing session id")
		return
	}
	transitions, err := s.store.ListTopicTransitions(r.Context(), id, 0)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if transitions == nil {
		transitions = []duckdb.TopicTransition{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"transitions": transitions})
}

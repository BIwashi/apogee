package collector

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/BIwashi/apogee/internal/interventions"
	"github.com/BIwashi/apogee/internal/sse"
	"github.com/BIwashi/apogee/internal/store/duckdb"
)

// interventionCreateRequest is the JSON body shape accepted by
// POST /v1/interventions. Mirrors web/app/lib/api-types.ts ::
// InterventionCreateRequest.
type interventionCreateRequest struct {
	SessionID    string `json:"session_id"`
	TurnID       string `json:"turn_id,omitempty"`
	OperatorID   string `json:"operator_id,omitempty"`
	Message      string `json:"message"`
	DeliveryMode string `json:"delivery_mode,omitempty"`
	Scope        string `json:"scope,omitempty"`
	Urgency      string `json:"urgency,omitempty"`
	Notes        string `json:"notes,omitempty"`
	TTLSeconds   int    `json:"ttl_seconds,omitempty"`
}

type interventionClaimRequest struct {
	HookEvent string `json:"hook_event"`
	TurnID    string `json:"turn_id,omitempty"`
}

type interventionDeliveredRequest struct {
	HookEvent string `json:"hook_event"`
}

type interventionConsumedRequest struct {
	EventID int64 `json:"event_id"`
}

func interventionSnapshotJSON(iv duckdb.Intervention) sse.InterventionSnapshot {
	return sse.SnapshotFromIntervention(iv)
}

func (s *Server) submitIntervention(w http.ResponseWriter, r *http.Request) {
	if s.interventions == nil {
		writeJSONError(w, http.StatusServiceUnavailable, "intervention service not configured")
		return
	}
	var body interventionCreateRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid json body")
		return
	}
	req := duckdb.InterventionRequest{
		SessionID:    body.SessionID,
		TurnID:       body.TurnID,
		OperatorID:   body.OperatorID,
		Message:      body.Message,
		DeliveryMode: body.DeliveryMode,
		Scope:        body.Scope,
		Urgency:      body.Urgency,
		Notes:        body.Notes,
	}
	if body.TTLSeconds > 0 {
		req.TTL = time.Duration(body.TTLSeconds) * time.Second
	}
	iv, err := s.interventions.Submit(r.Context(), req)
	if err != nil {
		switch {
		case errors.Is(err, interventions.ErrSessionRequired),
			errors.Is(err, interventions.ErrMessageRequired),
			errors.Is(err, interventions.ErrMessageTooLong),
			errors.Is(err, interventions.ErrInvalidDeliveryMode),
			errors.Is(err, interventions.ErrInvalidScope),
			errors.Is(err, interventions.ErrInvalidUrgency):
			writeJSONError(w, http.StatusBadRequest, err.Error())
		default:
			writeJSONError(w, http.StatusInternalServerError, err.Error())
		}
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"intervention": interventionSnapshotJSON(iv)})
}

func (s *Server) getIntervention(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	iv, ok, err := s.store.GetIntervention(r.Context(), id)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !ok {
		writeJSONError(w, http.StatusNotFound, "intervention not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"intervention": interventionSnapshotJSON(iv)})
}

func (s *Server) cancelIntervention(w http.ResponseWriter, r *http.Request) {
	if s.interventions == nil {
		writeJSONError(w, http.StatusServiceUnavailable, "intervention service not configured")
		return
	}
	id := chi.URLParam(r, "id")
	iv, err := s.interventions.Cancel(r.Context(), id)
	if err != nil {
		switch {
		case errors.Is(err, duckdb.ErrInterventionNotFound):
			writeJSONError(w, http.StatusNotFound, "intervention not found")
		case errors.Is(err, duckdb.ErrInterventionImmutable):
			writeJSONError(w, http.StatusConflict, "intervention is in a terminal state")
		default:
			writeJSONError(w, http.StatusInternalServerError, err.Error())
		}
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"intervention": interventionSnapshotJSON(iv)})
}

func (s *Server) claimSessionIntervention(w http.ResponseWriter, r *http.Request) {
	if s.interventions == nil {
		writeJSONError(w, http.StatusServiceUnavailable, "intervention service not configured")
		return
	}
	sessionID := chi.URLParam(r, "id")
	if sessionID == "" {
		writeJSONError(w, http.StatusBadRequest, "session id is required")
		return
	}
	var body interventionClaimRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid json body")
		return
	}
	if body.HookEvent == "" {
		writeJSONError(w, http.StatusBadRequest, "hook_event is required")
		return
	}
	iv, ok, err := s.interventions.Claim(r.Context(), sessionID, body.TurnID, body.HookEvent)
	if err != nil {
		if errors.Is(err, duckdb.ErrInterventionInvalidMode) {
			// Hook event isn't claim-capable — behave like "nothing to claim"
			// so the hook caller never 400s on e.g. PostToolUse.
			w.WriteHeader(http.StatusNoContent)
			return
		}
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !ok {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"intervention": interventionSnapshotJSON(iv)})
}

func (s *Server) deliveredIntervention(w http.ResponseWriter, r *http.Request) {
	if s.interventions == nil {
		writeJSONError(w, http.StatusServiceUnavailable, "intervention service not configured")
		return
	}
	id := chi.URLParam(r, "id")
	var body interventionDeliveredRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid json body")
		return
	}
	iv, err := s.interventions.Delivered(r.Context(), id, body.HookEvent)
	if err != nil {
		switch {
		case errors.Is(err, duckdb.ErrInterventionNotFound):
			writeJSONError(w, http.StatusNotFound, "intervention not found")
		case errors.Is(err, duckdb.ErrInterventionImmutable):
			writeJSONError(w, http.StatusConflict, "intervention is not in claimed state")
		default:
			writeJSONError(w, http.StatusInternalServerError, err.Error())
		}
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"intervention": interventionSnapshotJSON(iv)})
}

func (s *Server) consumedIntervention(w http.ResponseWriter, r *http.Request) {
	if s.interventions == nil {
		writeJSONError(w, http.StatusServiceUnavailable, "intervention service not configured")
		return
	}
	id := chi.URLParam(r, "id")
	var body interventionConsumedRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid json body")
		return
	}
	iv, err := s.interventions.Consumed(r.Context(), id, body.EventID)
	if err != nil {
		switch {
		case errors.Is(err, duckdb.ErrInterventionNotFound):
			writeJSONError(w, http.StatusNotFound, "intervention not found")
		case errors.Is(err, duckdb.ErrInterventionImmutable):
			writeJSONError(w, http.StatusConflict, "intervention is not in delivered state")
		default:
			writeJSONError(w, http.StatusInternalServerError, err.Error())
		}
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"intervention": interventionSnapshotJSON(iv)})
}

func (s *Server) listSessionInterventions(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	limit := parseLimit(r, 200, 500)
	status := r.URL.Query().Get("status")
	rows, err := s.store.ListInterventionsBySession(r.Context(), id, limit)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if status != "" {
		filtered := rows[:0]
		for _, iv := range rows {
			if iv.Status == status {
				filtered = append(filtered, iv)
			}
		}
		rows = filtered
	}
	writeJSON(w, http.StatusOK, map[string]any{"interventions": projectInterventionList(rows)})
}

func (s *Server) listPendingSessionInterventions(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	rows, err := s.store.ListPendingInterventionsBySession(r.Context(), id)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"interventions": projectInterventionList(rows)})
}

func (s *Server) listTurnInterventions(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "turn_id")
	rows, err := s.store.ListPendingInterventionsByTurn(r.Context(), id)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"interventions": projectInterventionList(rows)})
}

func projectInterventionList(rows []duckdb.Intervention) []sse.InterventionSnapshot {
	out := make([]sse.InterventionSnapshot, 0, len(rows))
	for _, row := range rows {
		out = append(out, sse.SnapshotFromIntervention(row))
	}
	return out
}

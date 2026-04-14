package ingest

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
)

// Handler is the HTTP entry point for inbound hook events.
type Handler struct {
	Reconstructor *Reconstructor
	Logger        *slog.Logger
}

// NewHandler constructs a Handler.
func NewHandler(rec *Reconstructor, logger *slog.Logger) *Handler {
	if logger == nil {
		logger = slog.Default()
	}
	return &Handler{Reconstructor: rec, Logger: logger}
}

// ReceiveEvent implements POST /v1/events. It accepts either a single
// HookEvent JSON object or a JSON array of HookEvent values and applies each
// through the reconstructor in order. Returns 202 on success with the number
// of events that were accepted.
func (h *Handler) ReceiveEvent(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
	body, err := io.ReadAll(io.LimitReader(r.Body, 4<<20)) // 4 MiB cap
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "read body: "+err.Error())
		return
	}
	events, err := DecodeHookEvents(body)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "decode body: "+err.Error())
		return
	}
	for i := range events {
		ev := events[i]
		if err := h.Reconstructor.Apply(r.Context(), &ev); err != nil {
			var verr *validationError
			if errors.As(err, &verr) {
				writeJSONError(w, http.StatusBadRequest, err.Error())
				return
			}
			switch err.Error() {
			case
				"event: source_app is required",
				"event: session_id is required",
				"event: hook_event_type is required",
				"event: timestamp must be positive ms-since-epoch":
				writeJSONError(w, http.StatusBadRequest, err.Error())
				return
			}
			h.Logger.Error("apply event", "err", err, "event", ev.HookEventType)
			writeJSONError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	writeJSON(w, http.StatusAccepted, map[string]any{
		"status":   "accepted",
		"accepted": len(events),
	})
}

type validationError struct{ msg string }

func (v *validationError) Error() string { return v.msg }

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func writeJSONError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

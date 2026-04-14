package collector

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/BIwashi/apogee/internal/sse"
)

const (
	sseHeartbeatInterval = 15 * time.Second
	sseInitialTurnsLimit = 100
	sseInitialSessions   = 50
)

// streamEvents is the GET /v1/events/stream handler. It upgrades the
// connection to an SSE stream, pushes a synthetic `initial` event with the
// 100 most recent turns and 50 most recent sessions, then relays every event
// the hub publishes until the client disconnects. Optional query params
// ?session_id and ?source_app narrow the stream (additive AND filter).
func (s *Server) streamEvents(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)

	filter := sse.Filter{
		SessionID: r.URL.Query().Get("session_id"),
		SourceApp: r.URL.Query().Get("source_app"),
	}
	sub := s.hub.Subscribe(filter)
	defer s.hub.Unsubscribe(sub)

	ctx := r.Context()

	// Hydrate the client with recent rows from the store so it can render
	// immediately, without a second REST round-trip.
	if err := s.writeInitial(ctx, w, flusher); err != nil {
		s.logger.Debug("sse write initial", "err", err)
		return
	}

	heartbeat := time.NewTicker(sseHeartbeatInterval)
	defer heartbeat.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case ev, ok := <-sub.C():
			if !ok {
				return
			}
			if err := writeSSEEvent(w, ev); err != nil {
				s.logger.Debug("sse write event", "err", err)
				return
			}
			flusher.Flush()
		case <-heartbeat.C:
			if _, err := fmt.Fprint(w, ": keepalive\n\n"); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

// writeInitial pulls the hydration data from the store and writes a single
// synthetic `initial` event frame to w.
func (s *Server) writeInitial(ctx context.Context, w http.ResponseWriter, flusher http.Flusher) error {
	turns, err := s.store.ListRecentTurns(ctx, sseInitialTurnsLimit)
	if err != nil {
		return fmt.Errorf("list recent turns: %w", err)
	}
	sessions, err := s.store.ListRecentSessions(ctx, sseInitialSessions)
	if err != nil {
		return fmt.Errorf("list recent sessions: %w", err)
	}
	ev := sse.NewInitialEvent(time.Now(), turns, sessions)
	if err := writeSSEEvent(w, ev); err != nil {
		return err
	}
	flusher.Flush()
	return nil
}

// writeSSEEvent serialises ev as a single `data: <json>\n\n` frame.
func writeSSEEvent(w http.ResponseWriter, ev sse.Event) error {
	payload, err := json.Marshal(ev)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(w, "data: %s\n\n", payload)
	return err
}

// Package sse is the apogee collector's in-process fan-out hub. It takes
// reconstructed domain events from the ingest layer and broadcasts them to
// every subscribed HTTP client so the live dashboard stays in sync with
// DuckDB without polling.
package sse

import (
	"encoding/json"
	"time"

	"github.com/BIwashi/apogee/internal/otel"
	"github.com/BIwashi/apogee/internal/store/duckdb"
)

// Event type constants. These are the strings that appear on the wire under
// the top-level `type` key. Keep this list aligned with the TypeScript client
// in web/app/lib/api-types.ts.
const (
	EventTypeInitial        = "initial"
	EventTypeTurnStarted    = "turn.started"
	EventTypeTurnUpdated    = "turn.updated"
	EventTypeTurnEnded      = "turn.ended"
	EventTypeSpanInserted   = "span.inserted"
	EventTypeSpanUpdated    = "span.updated"
	EventTypeSessionUpdated = "session.updated"
)

// Event is the broadcast wire message shape. Every SSE frame on the stream
// decodes to exactly this envelope.
type Event struct {
	Type string          `json:"type"`
	At   time.Time       `json:"at"`
	Data json.RawMessage `json:"data"`
}

// TurnPayload wraps a full turn snapshot for the `turn.*` event types.
type TurnPayload struct {
	Turn duckdb.Turn `json:"turn"`
}

// SpanPayload wraps a full span snapshot for the `span.*` event types.
type SpanPayload struct {
	Span duckdb.SpanRow `json:"span"`
}

// SessionPayload wraps a full session snapshot for `session.updated`.
type SessionPayload struct {
	Session duckdb.Session `json:"session"`
}

// InitialPayload is the synthetic bootstrap event pushed to every new
// subscriber before any live events flow. It lets the dashboard hydrate
// without a second round-trip against the REST endpoints.
type InitialPayload struct {
	RecentTurns    []duckdb.Turn    `json:"recent_turns"`
	RecentSessions []duckdb.Session `json:"recent_sessions"`
}

// NewTurnEvent builds an Event for a turn lifecycle change. kind must be one
// of the EventTypeTurn* constants.
func NewTurnEvent(kind string, now time.Time, turn duckdb.Turn) Event {
	data, _ := json.Marshal(TurnPayload{Turn: turn})
	return Event{Type: kind, At: now, Data: data}
}

// NewSpanEvent builds an Event for a span insert or update.
func NewSpanEvent(kind string, now time.Time, span duckdb.SpanRow) Event {
	data, _ := json.Marshal(SpanPayload{Span: span})
	return Event{Type: kind, At: now, Data: data}
}

// NewSessionEvent builds an Event for a session upsert.
func NewSessionEvent(now time.Time, sess duckdb.Session) Event {
	data, _ := json.Marshal(SessionPayload{Session: sess})
	return Event{Type: EventTypeSessionUpdated, At: now, Data: data}
}

// SpanRowFromOTel projects an in-memory otel.Span onto the duckdb.SpanRow
// shape the dashboard consumes. The reconstructor already has the span in
// memory after a successful write, so we avoid a DuckDB round-trip.
func SpanRowFromOTel(sp *otel.Span) duckdb.SpanRow {
	row := duckdb.SpanRow{
		TraceID:       string(sp.TraceID),
		SpanID:        string(sp.SpanID),
		ParentSpanID:  string(sp.ParentSpanID),
		Name:          sp.Name,
		Kind:          string(sp.Kind),
		StartTime:     sp.StartTime,
		EndTime:       sp.EndTime,
		StatusCode:    string(sp.StatusCode),
		StatusMessage: sp.StatusMessage,
		ServiceName:   sp.ServiceName,
		SessionID:     sp.SessionID,
		TurnID:        sp.TurnID,
		AgentID:       sp.AgentID,
		AgentKind:     sp.AgentKind,
		ToolName:      sp.ToolName,
		ToolUseID:     sp.ToolUseID,
		MCPServer:     sp.MCPServer,
		MCPTool:       sp.MCPTool,
		HookEvent:     sp.HookEvent,
		Attributes:    sp.Attributes,
	}
	if sp.EndTime != nil {
		dur := sp.DurationNanos()
		row.DurationNs = &dur
	}
	if len(sp.Events) > 0 {
		events := make([]any, 0, len(sp.Events))
		for _, e := range sp.Events {
			events = append(events, e)
		}
		row.Events = events
	}
	return row
}

// NewInitialEvent builds the synthetic bootstrap event.
func NewInitialEvent(now time.Time, turns []duckdb.Turn, sessions []duckdb.Session) Event {
	if turns == nil {
		turns = []duckdb.Turn{}
	}
	if sessions == nil {
		sessions = []duckdb.Session{}
	}
	data, _ := json.Marshal(InitialPayload{RecentTurns: turns, RecentSessions: sessions})
	return Event{Type: EventTypeInitial, At: now, Data: data}
}

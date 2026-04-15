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
	EventTypeInitial           = "initial"
	EventTypeTurnStarted       = "turn.started"
	EventTypeTurnUpdated       = "turn.updated"
	EventTypeTurnEnded         = "turn.ended"
	EventTypeSpanInserted      = "span.inserted"
	EventTypeSpanUpdated       = "span.updated"
	EventTypeSessionUpdated    = "session.updated"
	EventHITLRequested         = "hitl.requested"
	EventHITLResponded         = "hitl.responded"
	EventHITLExpired           = "hitl.expired"
	EventInterventionSubmitted = "intervention.submitted"
	EventInterventionClaimed   = "intervention.claimed"
	EventInterventionDelivered = "intervention.delivered"
	EventInterventionConsumed  = "intervention.consumed"
	EventInterventionExpired   = "intervention.expired"
	EventInterventionCancelled = "intervention.cancelled"
	// EventWatchdogSignal fires when the background anomaly detector
	// writes a new watchdog_signals row. Payload is WatchdogPayload.
	EventWatchdogSignal = "watchdog.signal"
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

// HITLSnapshot is the flat JSON projection of a HITLEvent row. Pointer
// fields are emitted as null when unset so the TypeScript client can rely
// on a stable schema without having to inspect database null wrappers.
type HITLSnapshot struct {
	HitlID         string         `json:"hitl_id"`
	SpanID         string         `json:"span_id"`
	TraceID        string         `json:"trace_id"`
	SessionID      string         `json:"session_id"`
	TurnID         string         `json:"turn_id"`
	Kind           string         `json:"kind"`
	Status         string         `json:"status"`
	RequestedAt    time.Time      `json:"requested_at"`
	RespondedAt    *time.Time     `json:"responded_at"`
	Question       string         `json:"question"`
	Suggestions    []string       `json:"suggestions"`
	Context        map[string]any `json:"context"`
	Decision       *string        `json:"decision"`
	ReasonCategory *string        `json:"reason_category"`
	OperatorNote   *string        `json:"operator_note"`
	ResumeMode     *string        `json:"resume_mode"`
	OperatorID     *string        `json:"operator_id"`
}

// HITLPayload is the SSE wrapper for HITL lifecycle events.
type HITLPayload struct {
	HITL HITLSnapshot `json:"hitl"`
}

// SnapshotFromHITL projects a stored row onto its wire shape, decoding the
// JSON-encoded suggestions and context fields and unwrapping nullable
// columns. Decoding errors are tolerated — the field defaults to its zero
// value rather than failing the broadcast.
func SnapshotFromHITL(ev duckdb.HITLEvent) HITLSnapshot {
	snap := HITLSnapshot{
		HitlID:      ev.HitlID,
		SpanID:      ev.SpanID,
		TraceID:     ev.TraceID,
		SessionID:   ev.SessionID,
		TurnID:      ev.TurnID,
		Kind:        ev.Kind,
		Status:      ev.Status,
		RequestedAt: ev.RequestedAt,
		Question:    ev.Question,
		Suggestions: []string{},
		Context:     map[string]any{},
	}
	if ev.RespondedAt.Valid {
		t := ev.RespondedAt.Time
		snap.RespondedAt = &t
	}
	if ev.SuggestionsJSON != "" {
		var parsed []string
		if err := json.Unmarshal([]byte(ev.SuggestionsJSON), &parsed); err == nil && parsed != nil {
			snap.Suggestions = parsed
		}
	}
	if ev.ContextJSON != "" {
		var parsed map[string]any
		if err := json.Unmarshal([]byte(ev.ContextJSON), &parsed); err == nil && parsed != nil {
			snap.Context = parsed
		}
	}
	if ev.Decision.Valid {
		v := ev.Decision.String
		snap.Decision = &v
	}
	if ev.ReasonCategory.Valid {
		v := ev.ReasonCategory.String
		snap.ReasonCategory = &v
	}
	if ev.OperatorNote.Valid {
		v := ev.OperatorNote.String
		snap.OperatorNote = &v
	}
	if ev.ResumeMode.Valid {
		v := ev.ResumeMode.String
		snap.ResumeMode = &v
	}
	if ev.OperatorID.Valid {
		v := ev.OperatorID.String
		snap.OperatorID = &v
	}
	return snap
}

// NewHITLEvent builds an SSE Event for one of the hitl.* lifecycle types.
func NewHITLEvent(kind string, now time.Time, ev duckdb.HITLEvent) Event {
	data, _ := json.Marshal(HITLPayload{HITL: SnapshotFromHITL(ev)})
	return Event{Type: kind, At: now, Data: data}
}

// InterventionSnapshot is the flat JSON projection of an intervention row.
// Keeps in lockstep with web/app/lib/api-types.ts :: Intervention.
type InterventionSnapshot struct {
	InterventionID  string     `json:"intervention_id"`
	SessionID       string     `json:"session_id"`
	TurnID          string     `json:"turn_id,omitempty"`
	OperatorID      string     `json:"operator_id,omitempty"`
	CreatedAt       time.Time  `json:"created_at"`
	ClaimedAt       *time.Time `json:"claimed_at,omitempty"`
	DeliveredAt     *time.Time `json:"delivered_at,omitempty"`
	ConsumedAt      *time.Time `json:"consumed_at,omitempty"`
	ExpiredAt       *time.Time `json:"expired_at,omitempty"`
	CancelledAt     *time.Time `json:"cancelled_at,omitempty"`
	AutoExpireAt    time.Time  `json:"auto_expire_at"`
	Message         string     `json:"message"`
	DeliveryMode    string     `json:"delivery_mode"`
	Scope           string     `json:"scope"`
	Urgency         string     `json:"urgency"`
	Status          string     `json:"status"`
	DeliveredVia    string     `json:"delivered_via,omitempty"`
	ConsumedEventID int64      `json:"consumed_event_id,omitempty"`
	Notes           string     `json:"notes,omitempty"`
}

// InterventionPayload is the SSE wrapper for intervention lifecycle events.
type InterventionPayload struct {
	Intervention InterventionSnapshot `json:"intervention"`
}

// SnapshotFromIntervention projects a stored row onto its wire shape,
// unwrapping nullable columns and zeroing fields that were not set.
func SnapshotFromIntervention(iv duckdb.Intervention) InterventionSnapshot {
	snap := InterventionSnapshot{
		InterventionID: iv.InterventionID,
		SessionID:      iv.SessionID,
		CreatedAt:      iv.CreatedAt,
		AutoExpireAt:   iv.AutoExpireAt,
		Message:        iv.Message,
		DeliveryMode:   iv.DeliveryMode,
		Scope:          iv.Scope,
		Urgency:        iv.Urgency,
		Status:         iv.Status,
	}
	if iv.TurnID.Valid {
		snap.TurnID = iv.TurnID.String
	}
	if iv.OperatorID.Valid {
		snap.OperatorID = iv.OperatorID.String
	}
	if iv.ClaimedAt.Valid {
		t := iv.ClaimedAt.Time
		snap.ClaimedAt = &t
	}
	if iv.DeliveredAt.Valid {
		t := iv.DeliveredAt.Time
		snap.DeliveredAt = &t
	}
	if iv.ConsumedAt.Valid {
		t := iv.ConsumedAt.Time
		snap.ConsumedAt = &t
	}
	if iv.ExpiredAt.Valid {
		t := iv.ExpiredAt.Time
		snap.ExpiredAt = &t
	}
	if iv.CancelledAt.Valid {
		t := iv.CancelledAt.Time
		snap.CancelledAt = &t
	}
	if iv.DeliveredVia.Valid {
		snap.DeliveredVia = iv.DeliveredVia.String
	}
	if iv.ConsumedEventID.Valid {
		snap.ConsumedEventID = iv.ConsumedEventID.Int64
	}
	if iv.Notes.Valid {
		snap.Notes = iv.Notes.String
	}
	return snap
}

// NewInterventionEvent builds an SSE Event for one of the intervention.*
// lifecycle types.
func NewInterventionEvent(kind string, now time.Time, iv duckdb.Intervention) Event {
	data, _ := json.Marshal(InterventionPayload{Intervention: SnapshotFromIntervention(iv)})
	return Event{Type: kind, At: now, Data: data}
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

// WatchdogSnapshot is the flat wire projection of a duckdb.WatchdogSignal.
// Nullable columns are unwrapped to pointers, labels are decoded from
// labels_json so the web client does not have to parse it again, and
// evidence_json is passed through as RawMessage so the UI can consume
// the typed payload directly.
type WatchdogSnapshot struct {
	ID             int64             `json:"id"`
	DetectedAt     time.Time         `json:"detected_at"`
	EndedAt        *time.Time        `json:"ended_at"`
	MetricName     string            `json:"metric_name"`
	Labels         map[string]string `json:"labels"`
	ZScore         float64           `json:"z_score"`
	BaselineMean   float64           `json:"baseline_mean"`
	BaselineStddev float64           `json:"baseline_stddev"`
	WindowValue    float64           `json:"window_value"`
	Severity       string            `json:"severity"`
	Headline       string            `json:"headline"`
	Evidence       json.RawMessage   `json:"evidence"`
	Acknowledged   bool              `json:"acknowledged"`
	AcknowledgedAt *time.Time        `json:"acknowledged_at"`
}

// WatchdogPayload is the SSE wrapper for watchdog.* events.
type WatchdogPayload struct {
	Signal WatchdogSnapshot `json:"signal"`
}

// SnapshotFromWatchdog projects a stored row onto its wire shape. It
// decodes labels_json into a map so the web client can render the label
// chips without running JSON.parse, and wraps evidence_json as a raw
// message so the typed payload rides through unchanged.
func SnapshotFromWatchdog(sig duckdb.WatchdogSignal) WatchdogSnapshot {
	snap := WatchdogSnapshot{
		ID:             sig.ID,
		DetectedAt:     sig.DetectedAt,
		MetricName:     sig.MetricName,
		Labels:         map[string]string{},
		ZScore:         sig.ZScore,
		BaselineMean:   sig.BaselineMean,
		BaselineStddev: sig.BaselineStddev,
		WindowValue:    sig.WindowValue,
		Severity:       sig.Severity,
		Headline:       sig.Headline,
		Acknowledged:   sig.Acknowledged,
	}
	if sig.LabelsJSON != "" {
		var parsed map[string]string
		if err := json.Unmarshal([]byte(sig.LabelsJSON), &parsed); err == nil && parsed != nil {
			snap.Labels = parsed
		}
	}
	if sig.EvidenceJSON != "" {
		snap.Evidence = json.RawMessage(sig.EvidenceJSON)
	} else {
		snap.Evidence = json.RawMessage("{}")
	}
	if sig.EndedAt.Valid {
		t := sig.EndedAt.Time
		snap.EndedAt = &t
	}
	if sig.AcknowledgedAt.Valid {
		t := sig.AcknowledgedAt.Time
		snap.AcknowledgedAt = &t
	}
	return snap
}

// NewWatchdogEvent builds an SSE Event for a freshly emitted watchdog
// signal. kind is fixed to EventWatchdogSignal — there is only one
// watchdog event type on the wire.
func NewWatchdogEvent(now time.Time, sig duckdb.WatchdogSignal) Event {
	data, _ := json.Marshal(WatchdogPayload{Signal: SnapshotFromWatchdog(sig)})
	return Event{Type: EventWatchdogSignal, At: now, Data: data}
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

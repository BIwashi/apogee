package sse

import (
	"encoding/json"
	"log/slog"
	"sync"
	"sync/atomic"
)

// subscriberBufferSize is the per-subscriber channel capacity. A slow client
// that can't drain this fast enough gets individual events dropped (with a
// debug log line) rather than blocking the producer.
const subscriberBufferSize = 64

// Filter narrows the events a subscriber receives. Both fields are optional;
// empty values mean "match anything". Filters are additive: every non-empty
// field must match (logical AND).
type Filter struct {
	SessionID string
	SourceApp string
}

// match returns true if ev satisfies the filter. Because Event.Data is a raw
// JSON blob we accept a second argument so callers can surface the relevant
// fields without re-parsing the JSON twice.
func (f Filter) match(tags eventTags) bool {
	if f.SessionID != "" && f.SessionID != tags.SessionID {
		return false
	}
	if f.SourceApp != "" && f.SourceApp != tags.SourceApp {
		return false
	}
	return true
}

// eventTags is the minimal set of fields a subscriber needs to filter on.
// The hub extracts these once at Broadcast time and reuses them for every
// subscriber so filtering stays O(subscribers).
type eventTags struct {
	SessionID string
	SourceApp string
}

// Subscription is a handle returned from Subscribe. Callers read from C and
// call the returned unsubscribe func when they are done.
type Subscription struct {
	id     uint64
	ch     chan Event
	filter Filter
}

// C exposes the read side of the subscription channel.
func (s *Subscription) C() <-chan Event { return s.ch }

// ID returns the internal subscriber id. Useful for log correlation.
func (s *Subscription) ID() uint64 { return s.id }

// Hub is the in-process SSE fan-out. The zero value is not usable; construct
// one with NewHub. Hub is safe for concurrent use.
type Hub struct {
	mu     sync.RWMutex
	subs   map[uint64]*Subscription
	nextID atomic.Uint64
	logger *slog.Logger

	// Metrics — exposed for tests and future /debug endpoints.
	dropped atomic.Uint64
}

// NewHub constructs a new Hub. logger may be nil (a discard logger is used).
func NewHub(logger *slog.Logger) *Hub {
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(discardWriter{}, &slog.HandlerOptions{Level: slog.LevelError}))
	}
	return &Hub{
		subs:   make(map[uint64]*Subscription),
		logger: logger,
	}
}

// Subscribe registers a new subscriber with the given filter and returns its
// handle. The caller is responsible for calling Unsubscribe on the same Hub
// when the subscriber goes away.
func (h *Hub) Subscribe(filter Filter) *Subscription {
	sub := &Subscription{
		id:     h.nextID.Add(1),
		ch:     make(chan Event, subscriberBufferSize),
		filter: filter,
	}
	h.mu.Lock()
	h.subs[sub.id] = sub
	h.mu.Unlock()
	return sub
}

// Unsubscribe removes a subscription and closes its channel. Safe to call
// multiple times.
func (h *Hub) Unsubscribe(sub *Subscription) {
	if sub == nil {
		return
	}
	h.mu.Lock()
	if _, ok := h.subs[sub.id]; ok {
		delete(h.subs, sub.id)
		close(sub.ch)
	}
	h.mu.Unlock()
}

// Broadcast sends ev to every subscriber whose filter matches. Delivery is
// non-blocking: if a subscriber's channel is full the event is dropped for
// that subscriber (and the dropped counter is incremented) rather than
// stalling the producer.
func (h *Hub) Broadcast(ev Event) {
	tags := extractTags(ev)
	h.mu.RLock()
	defer h.mu.RUnlock()
	for _, sub := range h.subs {
		if !sub.filter.match(tags) {
			continue
		}
		select {
		case sub.ch <- ev:
		default:
			h.dropped.Add(1)
			h.logger.Debug("sse: dropped event for slow subscriber",
				"subscriber", sub.id,
				"type", ev.Type,
			)
		}
	}
}

// Subscribers returns the current number of active subscribers.
func (h *Hub) Subscribers() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.subs)
}

// Dropped returns the cumulative count of events dropped due to slow
// consumers since the hub was created.
func (h *Hub) Dropped() uint64 {
	return h.dropped.Load()
}

// extractTags inspects the event payload for filterable fields. It is safe to
// call on any Event value; missing fields decode to empty strings.
func extractTags(ev Event) eventTags {
	var tags eventTags
	if len(ev.Data) == 0 {
		return tags
	}
	// Every payload shape embeds session_id and source_app at a known depth,
	// but the keys live under different parents (turn / span / session). We
	// decode into a permissive envelope and walk the first match.
	var env struct {
		Turn *struct {
			SessionID string `json:"session_id"`
			SourceApp string `json:"source_app"`
		} `json:"turn"`
		Span *struct {
			SessionID string `json:"session_id"`
		} `json:"span"`
		Session *struct {
			SessionID string `json:"session_id"`
			SourceApp string `json:"source_app"`
		} `json:"session"`
		HITL *struct {
			SessionID string `json:"session_id"`
		} `json:"hitl"`
	}
	if err := json.Unmarshal(ev.Data, &env); err != nil {
		return tags
	}
	switch {
	case env.Turn != nil:
		tags.SessionID = env.Turn.SessionID
		tags.SourceApp = env.Turn.SourceApp
	case env.Session != nil:
		tags.SessionID = env.Session.SessionID
		tags.SourceApp = env.Session.SourceApp
	case env.Span != nil:
		tags.SessionID = env.Span.SessionID
	case env.HITL != nil:
		tags.SessionID = env.HITL.SessionID
	}
	return tags
}

type discardWriter struct{}

func (discardWriter) Write(p []byte) (int, error) { return len(p), nil }

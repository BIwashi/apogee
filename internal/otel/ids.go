// Package otel defines OpenTelemetry-shaped data structures and id helpers
// used by the apogee collector. The collector does not import the official
// OTel SDK yet (PR #8 wires OTLP export); this package keeps the in-process
// representation independent of that dependency.
package otel

import (
	"crypto/rand"
	"encoding/hex"
	"sync"
	"time"
)

// TraceID is a 16-byte OpenTelemetry trace identifier rendered as 32 lower-case
// hexadecimal characters.
type TraceID string

// SpanID is an 8-byte OpenTelemetry span identifier rendered as 16 lower-case
// hexadecimal characters.
type SpanID string

// NewTraceID returns a fresh random TraceID.
func NewTraceID() TraceID {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		// crypto/rand failure is fatal in any reasonable runtime; fall back to
		// a deterministic but non-zero value so callers never see "" trace ids.
		return TraceID(hex.EncodeToString(fallbackBytes(16)))
	}
	return TraceID(hex.EncodeToString(b[:]))
}

// NewSpanID returns a fresh random SpanID.
func NewSpanID() SpanID {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return SpanID(hex.EncodeToString(fallbackBytes(8)))
	}
	return SpanID(hex.EncodeToString(b[:]))
}

// NewHITLID returns a fresh HITL request id of the form "hitl-<8 hex>".
// The prefix keeps the id self-describing in logs and audit trails.
func NewHITLID() string {
	var b [4]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "hitl-" + hex.EncodeToString(fallbackBytes(4))
	}
	return "hitl-" + hex.EncodeToString(b[:])
}

// NewTurnID returns a UUIDv7-like identifier suitable for the apogee turn id.
// We don't pull in google/uuid for this single use; the format is
// 8-4-4-4-12 hex characters with a millisecond timestamp prefix so ids sort
// chronologically.
func NewTurnID() string {
	return newUUIDv7(time.Now())
}

// NewTurnIDAt is the test seam for NewTurnID.
func NewTurnIDAt(t time.Time) string { return newUUIDv7(t) }

func newUUIDv7(t time.Time) string {
	var b [16]byte
	ms := uint64(t.UnixMilli())
	b[0] = byte(ms >> 40)
	b[1] = byte(ms >> 32)
	b[2] = byte(ms >> 24)
	b[3] = byte(ms >> 16)
	b[4] = byte(ms >> 8)
	b[5] = byte(ms)
	if _, err := rand.Read(b[6:]); err != nil {
		copy(b[6:], fallbackBytes(10))
	}
	// Set version (7) and variant (RFC 4122).
	b[6] = (b[6] & 0x0f) | 0x70
	b[8] = (b[8] & 0x3f) | 0x80
	h := hex.EncodeToString(b[:])
	return h[0:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:32]
}

var (
	fallbackMu      sync.Mutex
	fallbackCounter uint64
)

// fallbackBytes is only invoked when crypto/rand fails. It returns a
// deterministic but unique byte sequence so we never propagate empty ids.
func fallbackBytes(n int) []byte {
	fallbackMu.Lock()
	defer fallbackMu.Unlock()
	out := make([]byte, n)
	for i := 0; i < n; i++ {
		fallbackCounter++
		out[i] = byte(fallbackCounter)
	}
	return out
}

package otel

import (
	"encoding/hex"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestNewTraceID(t *testing.T) {
	id := NewTraceID()
	require.Len(t, string(id), 32)
	_, err := hex.DecodeString(string(id))
	require.NoError(t, err)
	require.NotEqual(t, NewTraceID(), id, "trace ids must be unique")
}

func TestNewSpanID(t *testing.T) {
	id := NewSpanID()
	require.Len(t, string(id), 16)
	_, err := hex.DecodeString(string(id))
	require.NoError(t, err)
}

func TestNewTurnIDFormat(t *testing.T) {
	id := NewTurnIDAt(time.Unix(1700000000, 0))
	require.Len(t, id, 36)
	require.Equal(t, byte('-'), id[8])
	require.Equal(t, byte('-'), id[13])
	require.Equal(t, byte('-'), id[18])
	require.Equal(t, byte('-'), id[23])
	// Version 7 nibble.
	require.Equal(t, byte('7'), id[14])
}

func TestSpanAttributesJSON(t *testing.T) {
	s := &Span{}
	require.Equal(t, "{}", s.AttributesJSON())
	require.Equal(t, "[]", s.EventsJSON())

	now := time.Now()
	end := now.Add(2 * time.Second)
	s = &Span{
		StartTime:  now,
		EndTime:    &end,
		Attributes: map[string]any{"k": "v"},
		Events:     []SpanEvent{{Name: "x", Time: now}},
	}
	require.Equal(t, int64(2_000_000_000), s.DurationNanos())
	require.Contains(t, s.AttributesJSON(), `"k":"v"`)
	require.Contains(t, s.EventsJSON(), `"name":"x"`)
}

func TestLogRecordAttributesJSON(t *testing.T) {
	l := &LogRecord{}
	require.Equal(t, "{}", l.AttributesJSON())
	l.Attributes = map[string]any{"foo": 1}
	require.Contains(t, l.AttributesJSON(), `"foo":1`)
}

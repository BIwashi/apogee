package duckdb

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/BIwashi/apogee/internal/otel"
)

// TestEventFacets verifies that the Datadog-style facet panel sees the
// correct per-dimension distinct values + counts when the store holds a
// mixed workload across source apps, hook events, and severities.
func TestEventFacets(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	now := time.Now().UTC().Truncate(time.Millisecond)
	require.NoError(t, s.UpsertSession(ctx, Session{
		SessionID: "sess-a", SourceApp: "apogee", StartedAt: now, LastSeenAt: now,
	}))
	require.NoError(t, s.UpsertSession(ctx, Session{
		SessionID: "sess-b", SourceApp: "other", StartedAt: now, LastSeenAt: now,
	}))

	// 20 rows for apogee + PreToolUse, 10 for other + PostToolUse, 5 errors.
	for i := 0; i < 20; i++ {
		require.NoError(t, s.InsertLog(ctx, &otel.LogRecord{
			Timestamp:    now.Add(time.Duration(i) * time.Second),
			SeverityText: "INFO",
			Body:         fmt.Sprintf("apogee %d", i),
			SessionID:    "sess-a",
			HookEvent:    "PreToolUse",
			SourceApp:    "apogee",
		}))
	}
	for i := 0; i < 10; i++ {
		require.NoError(t, s.InsertLog(ctx, &otel.LogRecord{
			Timestamp:    now.Add(time.Duration(i) * time.Second),
			SeverityText: "INFO",
			Body:         fmt.Sprintf("other %d", i),
			SessionID:    "sess-b",
			HookEvent:    "PostToolUse",
			SourceApp:    "other",
		}))
	}
	for i := 0; i < 5; i++ {
		require.NoError(t, s.InsertLog(ctx, &otel.LogRecord{
			Timestamp:    now.Add(time.Duration(i) * time.Second),
			SeverityText: "ERROR",
			Body:         fmt.Sprintf("boom %d", i),
			SessionID:    "sess-a",
			HookEvent:    "PostToolUseFailure",
			SourceApp:    "apogee",
		}))
	}

	// Unfiltered: every dimension should appear with the expected counts.
	facets, err := s.EventFacets(ctx, LogFilter{})
	require.NoError(t, err)
	byKey := map[string]map[string]int64{}
	for _, dim := range facets {
		byKey[dim.Key] = map[string]int64{}
		for _, v := range dim.Values {
			byKey[dim.Key][v.Value] = v.Count
		}
	}
	require.EqualValues(t, 25, byKey["source_app"]["apogee"])
	require.EqualValues(t, 10, byKey["source_app"]["other"])
	require.EqualValues(t, 20, byKey["hook_event"]["PreToolUse"])
	require.EqualValues(t, 10, byKey["hook_event"]["PostToolUse"])
	require.EqualValues(t, 5, byKey["hook_event"]["PostToolUseFailure"])
	require.EqualValues(t, 30, byKey["severity_text"]["INFO"])
	require.EqualValues(t, 5, byKey["severity_text"]["ERROR"])
	require.EqualValues(t, 25, byKey["session_id"]["sess-a"])
	require.EqualValues(t, 10, byKey["session_id"]["sess-b"])

	// Filtered on source_app=apogee: should narrow every dimension.
	facets, err = s.EventFacets(ctx, LogFilter{SourceApps: []string{"apogee"}})
	require.NoError(t, err)
	byKey = map[string]map[string]int64{}
	for _, dim := range facets {
		byKey[dim.Key] = map[string]int64{}
		for _, v := range dim.Values {
			byKey[dim.Key][v.Value] = v.Count
		}
	}
	require.EqualValues(t, 25, byKey["source_app"]["apogee"])
	_, ok := byKey["source_app"]["other"]
	require.False(t, ok)
	require.EqualValues(t, 5, byKey["severity_text"]["ERROR"])
	require.EqualValues(t, 20, byKey["severity_text"]["INFO"])

	// Multi-select severity: INFO + ERROR = everything.
	facets, err = s.EventFacets(ctx, LogFilter{Severities: []string{"info", "error"}})
	require.NoError(t, err)
	for _, dim := range facets {
		if dim.Key != "source_app" {
			continue
		}
		got := int64(0)
		for _, v := range dim.Values {
			got += v.Count
		}
		require.EqualValues(t, 35, got)
	}
}

// TestEventTimeseries asserts EventTimeseries bucketises events by the
// requested step and breaks totals down by severity.
func TestEventTimeseries(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	base := time.Now().UTC().Truncate(time.Minute)
	require.NoError(t, s.UpsertSession(ctx, Session{
		SessionID: "sess", SourceApp: "apogee", StartedAt: base, LastSeenAt: base,
	}))

	// 60 events over 60 seconds: 10 per 10-second bucket. Every 5th event
	// is an error so each bucket sees two errors and eight infos.
	for i := 0; i < 60; i++ {
		sev := "INFO"
		if i%5 == 0 {
			sev = "ERROR"
		}
		require.NoError(t, s.InsertLog(ctx, &otel.LogRecord{
			Timestamp:    base.Add(time.Duration(i) * time.Second),
			SeverityText: sev,
			Body:         fmt.Sprintf("msg %d", i),
			SessionID:    "sess",
			HookEvent:    "PreToolUse",
			SourceApp:    "apogee",
		}))
	}

	buckets, err := s.EventTimeseries(ctx, LogFilter{
		Since: base.Add(-time.Second),
		Until: base.Add(61 * time.Second),
	}, 10*time.Second)
	require.NoError(t, err)
	require.NotEmpty(t, buckets)

	total := int64(0)
	errors := int64(0)
	for _, b := range buckets {
		total += b.Total
		errors += b.BySeverity["error"]
	}
	require.EqualValues(t, 60, total)
	require.EqualValues(t, 12, errors)
}

// TestCountEvents covers the lightweight helper that powers the "N
// events found" header over the histogram.
func TestCountEvents(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	now := time.Now().UTC()
	require.NoError(t, s.UpsertSession(ctx, Session{
		SessionID: "c", StartedAt: now, LastSeenAt: now,
	}))
	for i := 0; i < 7; i++ {
		require.NoError(t, s.InsertLog(ctx, &otel.LogRecord{
			Timestamp:    now.Add(time.Duration(i) * time.Second),
			SeverityText: "INFO",
			Body:         "x",
			SessionID:    "c",
			HookEvent:    "PreToolUse",
			SourceApp:    "apogee",
		}))
	}
	n, err := s.CountEvents(ctx, LogFilter{})
	require.NoError(t, err)
	require.EqualValues(t, 7, n)

	n, err = s.CountEvents(ctx, LogFilter{SourceApps: []string{"nope"}})
	require.NoError(t, err)
	require.EqualValues(t, 0, n)
}

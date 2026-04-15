package duckdb

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/BIwashi/apogee/internal/otel"
)

// TestListRecentLogsCursorPagination — exercise the cursor-paginated
// `ListRecentLogs` helper that backs the `/v1/events/recent` endpoint and
// the `/events` web route. Inserts 200 rows, walks the table in batches of
// 50 until the cursor is exhausted, and asserts: no duplicates, strict
// monotonically-decreasing id order across pages, and exact total recovery.
func TestListRecentLogsCursorPagination(t *testing.T) {
	ctx := t.Context()
	s := newTestStore(t)

	// Pre-seed the parent rows the schema's foreign-key intent assumes.
	// (logs has no enforced FK in DuckDB but it keeps the test realistic.)
	now := time.Now().UTC().Truncate(time.Millisecond)
	require.NoError(t, s.UpsertSession(ctx, Session{
		SessionID:  "sess-pg",
		SourceApp:  "demo-app",
		StartedAt:  now,
		LastSeenAt: now,
	}))

	// Insert 200 logs. Two source apps and two hook events so the test
	// can also exercise filtering.
	const total = 200
	for i := 0; i < total; i++ {
		hookEvent := "PreToolUse"
		sourceApp := "demo-app"
		if i%2 == 0 {
			hookEvent = "PostToolUse"
		}
		if i%4 == 0 {
			sourceApp = "other-app"
		}
		require.NoError(t, s.InsertLog(ctx, &otel.LogRecord{
			Timestamp:    now.Add(time.Duration(i) * time.Millisecond),
			SeverityText: "INFO",
			Body:         fmt.Sprintf("log #%d", i),
			SessionID:    "sess-pg",
			TurnID:       "turn-pg",
			HookEvent:    hookEvent,
			SourceApp:    sourceApp,
		}))
	}

	// Walk the table in batches of 50. Track every id seen so we can
	// assert no duplicates across pages.
	seen := map[int64]bool{}
	var lastID int64
	cursor := int64(0)
	pageSize := 50
	totalSeen := 0
	for page := 0; page < 10; page++ {
		rows, nextCursor, err := s.ListRecentLogs(ctx, LogFilter{Before: cursor}, pageSize)
		require.NoError(t, err)
		if len(rows) == 0 {
			break
		}
		require.LessOrEqual(t, len(rows), pageSize)
		for i, row := range rows {
			require.False(t, seen[row.ID], "duplicate id %d on page %d", row.ID, page)
			seen[row.ID] = true
			// Strict descending order within a page.
			if i > 0 {
				require.Less(t, row.ID, rows[i-1].ID,
					"page %d row %d not strictly descending", page, i)
			}
			// Strict descending order across pages (monotone).
			if lastID != 0 {
				require.Less(t, row.ID, lastID,
					"page %d first row not strictly less than previous page tail", page)
			}
			lastID = row.ID
		}
		totalSeen += len(rows)
		// Stop if the page was short — exhausted.
		if len(rows) < pageSize {
			break
		}
		cursor = nextCursor
	}
	require.Equal(t, total, totalSeen, "should have walked exactly %d rows", total)

	// Filter by source_app — only ~50 rows (every 4th) match.
	filtered, _, err := s.ListRecentLogs(ctx, LogFilter{SourceApp: "other-app"}, 500)
	require.NoError(t, err)
	require.Equal(t, total/4, len(filtered))
	for _, row := range filtered {
		require.Equal(t, "other-app", row.SourceApp)
	}

	// Filter by hook event — half the rows.
	filtered, _, err = s.ListRecentLogs(ctx, LogFilter{Type: "PreToolUse"}, 500)
	require.NoError(t, err)
	require.Equal(t, total/2, len(filtered))
	for _, row := range filtered {
		require.Equal(t, "PreToolUse", row.HookEvent)
	}

	// Filter by session id — every row matches.
	filtered, _, err = s.ListRecentLogs(ctx, LogFilter{SessionID: "sess-pg"}, 500)
	require.NoError(t, err)
	require.Equal(t, total, len(filtered))
}

// TestListRecentLogsEmpty — empty table returns no rows and a zero cursor.
func TestListRecentLogsEmpty(t *testing.T) {
	ctx := t.Context()
	s := newTestStore(t)
	rows, cursor, err := s.ListRecentLogs(ctx, LogFilter{}, 50)
	require.NoError(t, err)
	require.Empty(t, rows)
	require.Equal(t, int64(0), cursor)
}

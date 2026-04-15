package summarizer

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/BIwashi/apogee/internal/sse"
	"github.com/BIwashi/apogee/internal/store/duckdb"
)

// seedClosedTurns inserts n closed turns into the given session so the
// rollup worker has something to digest.
func seedClosedTurns(t *testing.T, store *duckdb.Store, sessionID string, n int) {
	t.Helper()
	ctx := t.Context()
	now := time.Now().UTC().Truncate(time.Millisecond).Add(-time.Hour)

	require.NoError(t, store.UpsertSession(ctx, duckdb.Session{
		SessionID:  sessionID,
		SourceApp:  "apogee-test",
		StartedAt:  now,
		LastSeenAt: now.Add(time.Duration(n) * time.Minute),
	}))

	for i := 0; i < n; i++ {
		started := now.Add(time.Duration(i) * time.Minute)
		ended := started.Add(30 * time.Second)
		dur := int64(30_000)
		turnID := "turn-" + sessionID + "-" + string(rune('a'+i))
		require.NoError(t, store.InsertTurn(ctx, duckdb.Turn{
			TurnID:     turnID,
			TraceID:    "trace-" + turnID,
			SessionID:  sessionID,
			SourceApp:  "apogee-test",
			StartedAt:  started,
			Status:     "running",
			PromptText: "do thing #" + string(rune('a'+i)),
			Headline:   "headline-" + string(rune('a'+i)),
		}))
		require.NoError(t, store.UpdateTurnStatus(ctx, turnID, "completed", &ended, &dur, 1, 0, 0))
	}
}

func TestRollupWorkerProcessesJob(t *testing.T) {
	type tc struct {
		name        string
		closedTurns int
		response    string
		wantWritten bool
	}
	cases := []tc{
		{
			name:        "happy-path",
			closedTurns: 3,
			response:    `{"headline":"Refactored auth flow","narrative":"Worked through the refactor.","highlights":["a","b","c"],"patterns":["test runs"],"open_threads":[]}`,
			wantWritten: true,
		},
		{
			name:        "skips-single-turn",
			closedTurns: 1,
			response:    `{"headline":"x","narrative":"y","highlights":["a","b","c"],"patterns":[],"open_threads":[]}`,
			wantWritten: false,
		},
		{
			name:        "missing-headline-fails-gracefully",
			closedTurns: 3,
			response:    `{"narrative":"y","highlights":[]}`,
			wantWritten: false,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ctx := t.Context()
			store, err := duckdb.Open(ctx, ":memory:")
			require.NoError(t, err)
			defer store.Close()

			sessionID := "sess-" + c.name
			seedClosedTurns(t, store, sessionID, c.closedTurns)

			fake := &FakeRunner{Responder: func(model, prompt string) (string, error) {
				return c.response, nil
			}}
			hub := sse.NewHub(nil)
			cfg := Default()
			cfg.RollupSchedulerEnabled = false
			cfg.QueueSize = 4
			w := NewRollupWorker(cfg, fake, store, hub, nil)
			w.Start(ctx)
			defer w.Stop()

			w.Enqueue(sessionID, RollupReasonManual)

			deadline := time.Now().Add(2 * time.Second)
			var got bool
			for time.Now().Before(deadline) {
				row, ok, err := store.GetSessionRollup(ctx, sessionID)
				require.NoError(t, err)
				if ok && row.RollupJSON != "" {
					got = true
					require.Equal(t, "claude-sonnet-4-6", row.Model)
					require.Equal(t, c.closedTurns, row.TurnCount)
					break
				}
				time.Sleep(20 * time.Millisecond)
			}
			require.Equal(t, c.wantWritten, got)
		})
	}
}

func TestParseRollup(t *testing.T) {
	t.Run("strips-fences", func(t *testing.T) {
		raw := "```json\n{\"headline\":\"x\",\"narrative\":\"\",\"highlights\":[\"a\"],\"patterns\":[],\"open_threads\":[]}\n```"
		r, err := ParseRollup(raw)
		require.NoError(t, err)
		require.Equal(t, "x", r.Headline)
		require.Equal(t, []string{"a"}, r.Highlights)
	})
	t.Run("rejects-empty", func(t *testing.T) {
		_, err := ParseRollup("")
		require.Error(t, err)
	})
	t.Run("rejects-missing-headline", func(t *testing.T) {
		_, err := ParseRollup(`{"narrative":"x"}`)
		require.Error(t, err)
	})
}

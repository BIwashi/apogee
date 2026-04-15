package summarizer

import (
	"context"
	"encoding/json"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/BIwashi/apogee/internal/sse"
	"github.com/BIwashi/apogee/internal/store/duckdb"
)

// seedRollupRow inserts a minimal tier-2 rollup row so narrative process()
// has something to merge into.
func seedRollupRow(t *testing.T, store *duckdb.Store, sessionID string, turnCount int) {
	t.Helper()
	ctx := context.Background()
	blob, err := json.Marshal(Rollup{
		Headline:    "Session rollup",
		Narrative:   "Worked through multiple turns.",
		Highlights:  []string{"a", "b"},
		GeneratedAt: time.Now().UTC().Add(-time.Minute),
		Model:       "claude-sonnet-4-6",
		TurnCount:   turnCount,
	})
	require.NoError(t, err)
	require.NoError(t, store.UpsertSessionRollup(ctx, duckdb.SessionRollup{
		SessionID:   sessionID,
		GeneratedAt: time.Now().UTC().Add(-time.Minute),
		Model:       "claude-sonnet-4-6",
		TurnCount:   turnCount,
		RollupJSON:  string(blob),
	}))
}

func TestBuildNarrativePrompt_IncludesTurnHeadlines(t *testing.T) {
	prompt := BuildNarrativePrompt(NarrativePromptInput{
		SessionID: "sess-1",
		SourceApp: "apogee-test",
		Turns: []NarrativeTurn{
			{
				Index:    0,
				TurnID:   "turn-a",
				Headline: "Implemented the daemon core",
				Outcome:  "success",
				KeySteps: []string{"added launchd unit", "added systemd unit"},
			},
			{
				Index:    1,
				TurnID:   "turn-b",
				Headline: "Created PR and pushed",
				Outcome:  "success",
				KeySteps: []string{"gh pr create"},
			},
		},
		Rollup: Rollup{Headline: "Big rollup", Narrative: "Rolled everything up."},
	}, Defaults())

	require.Contains(t, prompt, "Implemented the daemon core")
	require.Contains(t, prompt, "Created PR and pushed")
	require.Contains(t, prompt, "added launchd unit")
	require.Contains(t, prompt, "Big rollup")
	require.Contains(t, prompt, "type NarrativeResponse")
	require.Contains(t, prompt, "Output ONLY the JSON object.")
}

func TestBuildNarrativePrompt_Japanese(t *testing.T) {
	prompt := BuildNarrativePrompt(NarrativePromptInput{
		SessionID: "sess-1",
		Turns:     []NarrativeTurn{{Index: 0, TurnID: "t-a", Headline: "x"}},
	}, Preferences{Language: LanguageJA})
	// Schema stays English.
	require.Contains(t, prompt, "type NarrativeResponse")
	// Prose flips to Japanese.
	require.Contains(t, prompt, "フェーズ")
	require.Contains(t, prompt, "JSON オブジェクトのみを出力してください")
}

func TestBuildNarrativePrompt_UserSystemPromptPrepended(t *testing.T) {
	prompt := BuildNarrativePrompt(NarrativePromptInput{
		SessionID: "sess-1",
		Turns:     []NarrativeTurn{{Index: 0, Headline: "x"}},
	}, Preferences{NarrativeSystemPrompt: "Focus on the daemon subsystem."})
	require.Contains(t, prompt, "User system prompt")
	require.Contains(t, prompt, "Focus on the daemon subsystem.")
}

func TestParseNarrativeResponse_HappyPath(t *testing.T) {
	raw := `{
	  "phases": [
	    {
	      "headline": "Implemented the daemon core",
	      "narrative": "Added the launchd + systemd units and wired the service.",
	      "key_steps": ["added launchd unit", "added systemd unit", "tests green"],
	      "kind": "implement",
	      "first_turn_index": 0,
	      "last_turn_index": 1
	    },
	    {
	      "headline": "Pushed and opened PR",
	      "narrative": "Created a branch, pushed, and opened the PR.",
	      "key_steps": ["git push", "gh pr create"],
	      "kind": "commit",
	      "first_turn_index": 2,
	      "last_turn_index": 2
	    }
	  ]
	}`
	phases, err := ParseNarrativeResponse(raw, 3)
	require.NoError(t, err)
	require.Len(t, phases, 2)
	require.Equal(t, "Implemented the daemon core", phases[0].Headline)
	require.Equal(t, "implement", phases[0].Kind)
	require.Equal(t, 0, phases[0].FirstTurnIndex)
	require.Equal(t, 1, phases[0].LastTurnIndex)
	require.Equal(t, "commit", phases[1].Kind)
	require.Equal(t, 2, phases[1].FirstTurnIndex)
	require.Equal(t, 2, phases[1].LastTurnIndex)
}

func TestParseNarrativeResponse_InvalidJSON(t *testing.T) {
	t.Run("strips-fences", func(t *testing.T) {
		raw := "```json\n{\"phases\":[{\"headline\":\"x\",\"narrative\":\"n\",\"key_steps\":[\"a\",\"b\"],\"kind\":\"implement\",\"first_turn_index\":0,\"last_turn_index\":1}]}\n```"
		phases, err := ParseNarrativeResponse(raw, 2)
		require.NoError(t, err)
		require.Len(t, phases, 1)
	})
	t.Run("rejects-empty", func(t *testing.T) {
		_, err := ParseNarrativeResponse("", 3)
		require.Error(t, err)
	})
	t.Run("rejects-truly-broken", func(t *testing.T) {
		_, err := ParseNarrativeResponse("not json at all", 3)
		require.Error(t, err)
	})
	t.Run("rejects-missing-headline", func(t *testing.T) {
		raw := `{"phases":[{"headline":"","key_steps":["a","b"],"kind":"implement","first_turn_index":0,"last_turn_index":0}]}`
		_, err := ParseNarrativeResponse(raw, 1)
		require.Error(t, err)
	})
	t.Run("rejects-coverage-gap", func(t *testing.T) {
		raw := `{"phases":[
		  {"headline":"a","key_steps":["x","y"],"kind":"implement","first_turn_index":0,"last_turn_index":0},
		  {"headline":"b","key_steps":["x","y"],"kind":"commit","first_turn_index":2,"last_turn_index":2}
		]}`
		_, err := ParseNarrativeResponse(raw, 3)
		require.Error(t, err)
	})
	t.Run("unknown-kind-falls-back-to-other", func(t *testing.T) {
		raw := `{"phases":[{"headline":"h","key_steps":["a","b"],"kind":"vibes","first_turn_index":0,"last_turn_index":1}]}`
		phases, err := ParseNarrativeResponse(raw, 2)
		require.NoError(t, err)
		require.Equal(t, PhaseKindOther, phases[0].Kind)
	})
}

// TestParseNarrativeForecast covers the optional forecast[] field the
// narrative prompt now asks for alongside phases[]. The parser is
// deliberately tolerant: a missing or empty forecast yields an empty
// slice and a nil error, and a single malformed entry is dropped
// silently rather than failing the whole tier-3 run.
func TestParseNarrativeForecast(t *testing.T) {
	t.Run("happy-path", func(t *testing.T) {
		raw := `{
		  "phases": [
		    {"headline":"x","key_steps":["a","b"],"kind":"implement","first_turn_index":0,"last_turn_index":0}
		  ],
		  "forecast": [
		    {"kind":"test","headline":"Run the full test suite","rationale":"after touching the worker tests"},
		    {"kind":"commit","headline":"Push the fix","rationale":"close the loop"}
		  ]
		}`
		forecast, err := ParseNarrativeForecast(raw)
		require.NoError(t, err)
		require.Len(t, forecast, 2)
		require.Equal(t, "test", forecast[0].Kind)
		require.Equal(t, "Run the full test suite", forecast[0].Headline)
		require.Equal(t, "after touching the worker tests", forecast[0].Rationale)
		require.Equal(t, "commit", forecast[1].Kind)
	})
	t.Run("empty-when-missing", func(t *testing.T) {
		raw := `{"phases":[{"headline":"x","key_steps":["a","b"],"kind":"implement","first_turn_index":0,"last_turn_index":0}]}`
		forecast, err := ParseNarrativeForecast(raw)
		require.NoError(t, err)
		require.Empty(t, forecast)
	})
	t.Run("drops-empty-headline", func(t *testing.T) {
		raw := `{
		  "phases":[],
		  "forecast":[
		    {"kind":"test","headline":""},
		    {"kind":"commit","headline":"Push the fix"}
		  ]
		}`
		forecast, err := ParseNarrativeForecast(raw)
		require.NoError(t, err)
		require.Len(t, forecast, 1)
		require.Equal(t, "Push the fix", forecast[0].Headline)
	})
	t.Run("unknown-kind-falls-back-to-other", func(t *testing.T) {
		raw := `{"phases":[],"forecast":[{"kind":"vibes","headline":"Ship it"}]}`
		forecast, err := ParseNarrativeForecast(raw)
		require.NoError(t, err)
		require.Len(t, forecast, 1)
		require.Equal(t, PhaseKindOther, forecast[0].Kind)
	})
	t.Run("caps-at-three", func(t *testing.T) {
		raw := `{"phases":[],"forecast":[
		  {"kind":"test","headline":"one"},
		  {"kind":"test","headline":"two"},
		  {"kind":"test","headline":"three"},
		  {"kind":"test","headline":"four"},
		  {"kind":"test","headline":"five"}
		]}`
		forecast, err := ParseNarrativeForecast(raw)
		require.NoError(t, err)
		require.Len(t, forecast, 3)
	})
	t.Run("handles-fenced-json", func(t *testing.T) {
		raw := "```json\n{\"phases\":[],\"forecast\":[{\"kind\":\"test\",\"headline\":\"one\"}]}\n```"
		forecast, err := ParseNarrativeForecast(raw)
		require.NoError(t, err)
		require.Len(t, forecast, 1)
	})
}

func TestNarrativeWorker_SkipsShortSessions(t *testing.T) {
	ctx := context.Background()
	store, err := duckdb.Open(ctx, ":memory:")
	require.NoError(t, err)
	defer store.Close()

	sessionID := "sess-short"
	seedClosedTurns(t, store, sessionID, 1)
	seedRollupRow(t, store, sessionID, 1)

	var calls atomic.Int32
	fake := &FakeRunner{Responder: func(_, _ string) (string, error) {
		calls.Add(1)
		return `{"phases":[]}`, nil
	}}
	cfg := Default()
	cfg.RollupSchedulerEnabled = false
	cfg.QueueSize = 4
	hub := sse.NewHub(nil)
	w := NewNarrativeWorker(cfg, fake, store, hub, nil)
	w.Start(ctx)
	defer w.Stop()

	w.Enqueue(sessionID, NarrativeReasonManual)
	// Give the worker a moment to process.
	time.Sleep(150 * time.Millisecond)

	require.Equal(t, int32(0), calls.Load(), "short session → runner must not be called")
}

func TestNarrativeWorker_WritesPhasesToRollupBlob(t *testing.T) {
	ctx := context.Background()
	store, err := duckdb.Open(ctx, ":memory:")
	require.NoError(t, err)
	defer store.Close()

	sessionID := "sess-happy"
	seedClosedTurns(t, store, sessionID, 3)
	seedRollupRow(t, store, sessionID, 3)

	// Canned response grouping 3 turns into 2 phases.
	resp := `{"phases":[
	  {"headline":"Implemented the daemon core","narrative":"Did the thing.","key_steps":["a","b","c"],"kind":"implement","first_turn_index":0,"last_turn_index":1},
	  {"headline":"Opened PR","narrative":"Pushed it.","key_steps":["x","y"],"kind":"commit","first_turn_index":2,"last_turn_index":2}
	]}`
	fake := &FakeRunner{Responder: func(_, _ string) (string, error) {
		return resp, nil
	}}
	cfg := Default()
	cfg.RollupSchedulerEnabled = false
	cfg.QueueSize = 4
	hub := sse.NewHub(nil)
	w := NewNarrativeWorker(cfg, fake, store, hub, nil)
	w.Start(ctx)
	defer w.Stop()

	w.Enqueue(sessionID, NarrativeReasonManual)

	deadline := time.Now().Add(2 * time.Second)
	var got Rollup
	for time.Now().Before(deadline) {
		row, ok, err := store.GetSessionRollup(ctx, sessionID)
		require.NoError(t, err)
		if ok && row.RollupJSON != "" {
			var r Rollup
			require.NoError(t, json.Unmarshal([]byte(row.RollupJSON), &r))
			if len(r.Phases) == 2 {
				got = r
				break
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	require.Len(t, got.Phases, 2)
	require.Equal(t, "Implemented the daemon core", got.Phases[0].Headline)
	require.Equal(t, 2, got.Phases[0].TurnCount)
	require.Equal(t, 1, got.Phases[1].TurnCount)
	require.Equal(t, "implement", got.Phases[0].Kind)
	require.Equal(t, "commit", got.Phases[1].Kind)
	require.NotZero(t, got.NarrativeGeneratedAt)
	require.Equal(t, "claude-sonnet-4-6", got.NarrativeModel)
	// Tier-2 fields are preserved.
	require.Equal(t, "Session rollup", got.Headline)
}

func TestNarrativeWorker_ChainsFromRollup(t *testing.T) {
	ctx := context.Background()
	store, err := duckdb.Open(ctx, ":memory:")
	require.NoError(t, err)
	defer store.Close()

	sessionID := "sess-chain"
	seedClosedTurns(t, store, sessionID, 3)

	// The tier-2 runner writes a rollup, which should chain into the
	// tier-3 narrative worker via the service callback.
	rollupResp := `{"headline":"x","narrative":"y","highlights":["a","b","c"],"patterns":[],"open_threads":[]}`
	narrativeResp := `{"phases":[{"headline":"one phase","narrative":"all of it","key_steps":["a","b"],"kind":"implement","first_turn_index":0,"last_turn_index":2}]}`
	fake := &FakeRunner{Responder: func(_, prompt string) (string, error) {
		// Tier-2 and tier-3 share the same FakeRunner; differentiate by
		// a string that only appears in the tier-3 prompt builder.
		if strings.Contains(prompt, "NarrativeResponse") {
			return narrativeResp, nil
		}
		return rollupResp, nil
	}}

	cfg := Default()
	cfg.RollupSchedulerEnabled = false
	cfg.QueueSize = 8
	hub := sse.NewHub(nil)
	svc := NewServiceWithRunner(cfg, fake, store, hub, nil)
	svc.SetPreferencesReader(NewStaticPreferencesReader(Defaults()))
	svc.Start(ctx)
	defer svc.Stop()

	svc.EnqueueRollup(sessionID, RollupReasonManual)

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		row, ok, err := store.GetSessionRollup(ctx, sessionID)
		require.NoError(t, err)
		if ok && row.RollupJSON != "" {
			var r Rollup
			require.NoError(t, json.Unmarshal([]byte(row.RollupJSON), &r))
			if len(r.Phases) == 1 {
				require.Equal(t, "one phase", r.Phases[0].Headline)
				return
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("narrative worker never chained off the rollup")
}

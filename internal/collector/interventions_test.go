package collector

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/BIwashi/apogee/internal/ingest"
	"github.com/BIwashi/apogee/internal/interventions"
	"github.com/BIwashi/apogee/internal/sse"
	"github.com/BIwashi/apogee/internal/store/duckdb"
)

// postJSON is a tiny helper that POSTs a value as JSON and returns the
// decoded response body.
func postJSON(t *testing.T, url string, body any) *http.Response {
	t.Helper()
	raw, err := json.Marshal(body)
	require.NoError(t, err)
	resp, err := http.Post(url, "application/json", bytes.NewReader(raw))
	require.NoError(t, err)
	return resp
}

func postHook(t *testing.T, tsURL string, ev ingest.HookEvent) {
	t.Helper()
	raw, err := json.Marshal(ev)
	require.NoError(t, err)
	resp, err := http.Post(tsURL+"/v1/events", "application/json", bytes.NewReader(raw))
	require.NoError(t, err)
	require.Equal(t, http.StatusAccepted, resp.StatusCode, "hook %s", ev.HookEventType)
	resp.Body.Close()
}

func readInterventionPayload(t *testing.T, body io.Reader) map[string]any {
	t.Helper()
	var wrapper struct {
		Intervention map[string]any `json:"intervention"`
	}
	require.NoError(t, json.NewDecoder(body).Decode(&wrapper))
	return wrapper.Intervention
}

func TestIntegrationInterventionLifecycle(t *testing.T) {
	srv, ts := newTestServer(t)

	// Bootstrap a turn with SessionStart + UserPromptSubmit + PreToolUse.
	sessionID := "sess-intervene-1"
	base := time.Now().UnixMilli()
	postHook(t, ts.URL, ingest.HookEvent{
		SourceApp: "demo", SessionID: sessionID,
		HookEventType: "SessionStart", Timestamp: base,
	})
	postHook(t, ts.URL, ingest.HookEvent{
		SourceApp: "demo", SessionID: sessionID,
		HookEventType: "UserPromptSubmit", Timestamp: base + 1,
		Prompt: "do a thing",
	})
	postHook(t, ts.URL, ingest.HookEvent{
		SourceApp: "demo", SessionID: sessionID,
		HookEventType: "PreToolUse", Timestamp: base + 2,
		ToolName: "Bash", ToolUseID: "tu-1",
	})

	// Discover the newly-created turn id for this session.
	resp, err := http.Get(ts.URL + "/v1/sessions/" + sessionID + "/turns")
	require.NoError(t, err)
	var tr struct {
		Turns []struct {
			TurnID string `json:"turn_id"`
		} `json:"turns"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&tr))
	resp.Body.Close()
	require.Len(t, tr.Turns, 1)
	turnID := tr.Turns[0].TurnID

	// Subscribe to SSE broadcasts so we can assert ordering.
	sub := srv.Hub().Subscribe(sse.Filter{SessionID: sessionID})
	defer srv.Hub().Unsubscribe(sub)

	// Submit an intervention.
	submitResp := postJSON(t, ts.URL+"/v1/interventions", map[string]any{
		"session_id":    sessionID,
		"turn_id":       turnID,
		"message":       "stop and reconsider your approach",
		"delivery_mode": "interrupt",
		"scope":         "this_turn",
		"urgency":       "normal",
	})
	defer submitResp.Body.Close()
	require.Equal(t, http.StatusCreated, submitResp.StatusCode)
	submitted := readInterventionPayload(t, submitResp.Body)
	interventionID := submitted["intervention_id"].(string)
	require.NotEmpty(t, interventionID)
	require.Equal(t, "queued", submitted["status"])

	// Pending listing returns exactly one row.
	resp, err = http.Get(ts.URL + "/v1/sessions/" + sessionID + "/interventions/pending")
	require.NoError(t, err)
	var pending struct {
		Interventions []map[string]any `json:"interventions"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&pending))
	resp.Body.Close()
	require.Len(t, pending.Interventions, 1)

	// Simulate the hook claiming it.
	claimResp := postJSON(t, ts.URL+"/v1/sessions/"+sessionID+"/interventions/claim", map[string]any{
		"hook_event": "PreToolUse",
		"turn_id":    turnID,
	})
	require.Equal(t, http.StatusOK, claimResp.StatusCode)
	claimed := readInterventionPayload(t, claimResp.Body)
	claimResp.Body.Close()
	require.Equal(t, "claimed", claimed["status"])

	// Hook reports delivery.
	delResp := postJSON(t, ts.URL+"/v1/interventions/"+interventionID+"/delivered", map[string]any{
		"hook_event": "PreToolUse",
	})
	require.Equal(t, http.StatusOK, delResp.StatusCode)
	delivered := readInterventionPayload(t, delResp.Body)
	delResp.Body.Close()
	require.Equal(t, "delivered", delivered["status"])

	// A downstream PostToolUse event: the reconstructor should flip the
	// intervention to consumed via its ObservePostHookConsumption hook.
	postHook(t, ts.URL, ingest.HookEvent{
		SourceApp: "demo", SessionID: sessionID,
		HookEventType: "PostToolUse", Timestamp: base + 3,
		ToolName: "Bash", ToolUseID: "tu-1",
	})

	// Poll the intervention until its status flips to consumed.
	require.Eventually(t, func() bool {
		r, err := http.Get(ts.URL + "/v1/interventions/" + interventionID)
		if err != nil {
			return false
		}
		defer r.Body.Close()
		if r.StatusCode != http.StatusOK {
			return false
		}
		iv := readInterventionPayload(t, r.Body)
		return iv["status"] == "consumed"
	}, 2*time.Second, 20*time.Millisecond)

	// Pending listing is now empty.
	resp, err = http.Get(ts.URL + "/v1/sessions/" + sessionID + "/interventions/pending")
	require.NoError(t, err)
	pending.Interventions = nil
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&pending))
	resp.Body.Close()
	require.Len(t, pending.Interventions, 0)

	// SSE ordering invariant: submitted → claimed → delivered → consumed.
	var seen []string
	deadline := time.After(500 * time.Millisecond)
loop:
	for {
		select {
		case ev := <-sub.C():
			seen = append(seen, ev.Type)
			if ev.Type == sse.EventInterventionConsumed {
				break loop
			}
		case <-deadline:
			break loop
		}
	}
	// Filter down to intervention events so span/turn chatter does not
	// corrupt the check.
	var ivSeen []string
	for _, tt := range seen {
		switch tt {
		case sse.EventInterventionSubmitted, sse.EventInterventionClaimed,
			sse.EventInterventionDelivered, sse.EventInterventionConsumed:
			ivSeen = append(ivSeen, tt)
		}
	}
	require.Equal(t, []string{
		sse.EventInterventionSubmitted,
		sse.EventInterventionClaimed,
		sse.EventInterventionDelivered,
		sse.EventInterventionConsumed,
	}, ivSeen)
}

func TestIntegrationInterventionClaimConcurrentRace(t *testing.T) {
	_, ts := newTestServer(t)

	sessionID := "sess-race"
	base := time.Now().UnixMilli()
	postHook(t, ts.URL, ingest.HookEvent{
		SourceApp: "demo", SessionID: sessionID,
		HookEventType: "SessionStart", Timestamp: base,
	})
	postHook(t, ts.URL, ingest.HookEvent{
		SourceApp: "demo", SessionID: sessionID,
		HookEventType: "UserPromptSubmit", Timestamp: base + 1,
	})

	submit := postJSON(t, ts.URL+"/v1/interventions", map[string]any{
		"session_id":    sessionID,
		"message":       "single winner",
		"delivery_mode": "interrupt",
		"scope":         "this_session",
		"urgency":       "high",
	})
	require.Equal(t, http.StatusCreated, submit.StatusCode)
	submit.Body.Close()

	const workers = 20
	var winners int64
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			resp := postJSON(t, ts.URL+"/v1/sessions/"+sessionID+"/interventions/claim", map[string]any{
				"hook_event": "PreToolUse",
				"turn_id":    "turn-race",
			})
			defer resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				atomic.AddInt64(&winners, 1)
			} else if resp.StatusCode != http.StatusNoContent {
				t.Errorf("unexpected status: %d", resp.StatusCode)
			}
		}()
	}
	close(start)
	wg.Wait()
	require.EqualValues(t, 1, atomic.LoadInt64(&winners))
}

func TestIntegrationInterventionAutoExpire(t *testing.T) {
	// Fresh in-memory store + a fast-expiring service to avoid sleeping
	// past the sweeper default.
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	store, err := duckdb.Open(ctx, ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	hub := sse.NewHub(nil)
	svc := interventions.NewService(interventions.Config{
		AutoExpireTTL:     100 * time.Millisecond,
		SweepInterval:     20 * time.Millisecond,
		BothFallbackAfter: time.Second,
		MaxMessageChars:   4096,
	}, store, hub, nil)
	svc.Start(ctx)
	defer svc.Stop()

	iv, err := svc.Submit(ctx, duckdb.InterventionRequest{
		SessionID:    "sess-expire",
		Message:      "will expire",
		DeliveryMode: duckdb.InterventionModeInterrupt,
		Scope:        duckdb.InterventionScopeSession,
		Urgency:      duckdb.InterventionUrgencyNormal,
		TTL:          100 * time.Millisecond,
	})
	require.NoError(t, err)

	require.Eventually(t, func() bool {
		row, ok, err := store.GetIntervention(ctx, iv.InterventionID)
		if err != nil || !ok {
			return false
		}
		return row.Status == duckdb.InterventionStatusExpired
	}, 2*time.Second, 20*time.Millisecond)
}

func TestIntegrationInterventionCancel(t *testing.T) {
	_, ts := newTestServer(t)

	sessionID := "sess-cancel"
	base := time.Now().UnixMilli()
	postHook(t, ts.URL, ingest.HookEvent{
		SourceApp: "demo", SessionID: sessionID,
		HookEventType: "SessionStart", Timestamp: base,
	})
	postHook(t, ts.URL, ingest.HookEvent{
		SourceApp: "demo", SessionID: sessionID,
		HookEventType: "UserPromptSubmit", Timestamp: base + 1,
	})

	submit := postJSON(t, ts.URL+"/v1/interventions", map[string]any{
		"session_id":    sessionID,
		"message":       "cancel me",
		"delivery_mode": "interrupt",
		"scope":         "this_session",
		"urgency":       "normal",
	})
	require.Equal(t, http.StatusCreated, submit.StatusCode)
	submitted := readInterventionPayload(t, submit.Body)
	submit.Body.Close()
	id := submitted["intervention_id"].(string)

	// Cancel.
	cancel := postJSON(t, ts.URL+"/v1/interventions/"+id+"/cancel", map[string]any{})
	require.Equal(t, http.StatusOK, cancel.StatusCode)
	cancel.Body.Close()

	// Second cancel returns 409.
	again := postJSON(t, ts.URL+"/v1/interventions/"+id+"/cancel", map[string]any{})
	require.Equal(t, http.StatusConflict, again.StatusCode)
	again.Body.Close()

	// Claim now returns 204.
	claim := postJSON(t, ts.URL+"/v1/sessions/"+sessionID+"/interventions/claim", map[string]any{
		"hook_event": "PreToolUse",
		"turn_id":    "turn-nothing",
	})
	require.Equal(t, http.StatusNoContent, claim.StatusCode)
	claim.Body.Close()
}

func TestIntegrationInterventionValidation(t *testing.T) {
	_, ts := newTestServer(t)

	cases := []map[string]any{
		{"session_id": "", "message": "x", "delivery_mode": "interrupt", "scope": "this_turn", "urgency": "normal"},
		{"session_id": "sess-val", "message": "", "delivery_mode": "interrupt", "scope": "this_turn", "urgency": "normal"},
		{"session_id": "sess-val", "message": "x", "delivery_mode": "nope", "scope": "this_turn", "urgency": "normal"},
		{"session_id": "sess-val", "message": "x", "delivery_mode": "interrupt", "scope": "nope", "urgency": "normal"},
		{"session_id": "sess-val", "message": "x", "delivery_mode": "interrupt", "scope": "this_turn", "urgency": "nope"},
	}
	for i, body := range cases {
		resp := postJSON(t, ts.URL+"/v1/interventions", body)
		require.Equal(t, http.StatusBadRequest, resp.StatusCode, "case %d", i)
		resp.Body.Close()
	}
}

func TestIntegrationInterventionNotFoundPaths(t *testing.T) {
	_, ts := newTestServer(t)

	resp, err := http.Get(ts.URL + "/v1/interventions/missing")
	require.NoError(t, err)
	require.Equal(t, http.StatusNotFound, resp.StatusCode)
	resp.Body.Close()

	cancel := postJSON(t, ts.URL+"/v1/interventions/missing/cancel", map[string]any{})
	require.Equal(t, http.StatusNotFound, cancel.StatusCode)
	cancel.Body.Close()
}

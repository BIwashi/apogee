package sse

import (
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/BIwashi/apogee/internal/store/duckdb"
)

func makeTurnEvent(t *testing.T, sessionID, sourceApp string) Event {
	t.Helper()
	turn := duckdb.Turn{
		TurnID:    "turn-" + sessionID,
		SessionID: sessionID,
		SourceApp: sourceApp,
		StartedAt: time.Unix(0, 0),
		Status:    "running",
	}
	return NewTurnEvent(EventTypeTurnStarted, time.Unix(0, 0), turn)
}

func TestHub(t *testing.T) {
	t.Run("broadcast to multiple subscribers", func(t *testing.T) {
		hub := NewHub(nil)
		a := hub.Subscribe(Filter{})
		b := hub.Subscribe(Filter{})
		c := hub.Subscribe(Filter{})
		defer hub.Unsubscribe(a)
		defer hub.Unsubscribe(b)
		defer hub.Unsubscribe(c)

		require.Equal(t, 3, hub.Subscribers())
		ev := makeTurnEvent(t, "sess-1", "demo")
		hub.Broadcast(ev)

		for _, sub := range []*Subscription{a, b, c} {
			select {
			case got := <-sub.C():
				require.Equal(t, EventTypeTurnStarted, got.Type)
				var payload TurnPayload
				require.NoError(t, json.Unmarshal(got.Data, &payload))
				require.Equal(t, "sess-1", payload.Turn.SessionID)
			case <-time.After(time.Second):
				t.Fatalf("subscriber %d did not receive broadcast", sub.ID())
			}
		}
	})

	t.Run("unsubscribe closes channel and stops delivery", func(t *testing.T) {
		hub := NewHub(nil)
		sub := hub.Subscribe(Filter{})
		require.Equal(t, 1, hub.Subscribers())
		hub.Unsubscribe(sub)
		require.Equal(t, 0, hub.Subscribers())

		_, ok := <-sub.C()
		require.False(t, ok, "channel must be closed after unsubscribe")

		// Second call is a no-op.
		hub.Unsubscribe(sub)

		// Broadcast with no subscribers should not panic or block.
		hub.Broadcast(makeTurnEvent(t, "sess-1", "demo"))
	})

	t.Run("filter by session id", func(t *testing.T) {
		hub := NewHub(nil)
		scoped := hub.Subscribe(Filter{SessionID: "sess-1"})
		wild := hub.Subscribe(Filter{})
		defer hub.Unsubscribe(scoped)
		defer hub.Unsubscribe(wild)

		hub.Broadcast(makeTurnEvent(t, "sess-2", "demo"))
		hub.Broadcast(makeTurnEvent(t, "sess-1", "demo"))

		// wild gets both, scoped only gets sess-1.
		select {
		case got := <-scoped.C():
			var payload TurnPayload
			require.NoError(t, json.Unmarshal(got.Data, &payload))
			require.Equal(t, "sess-1", payload.Turn.SessionID)
		case <-time.After(time.Second):
			t.Fatalf("scoped subscriber timed out")
		}
		select {
		case <-scoped.C():
			t.Fatalf("scoped subscriber should not have received sess-2")
		case <-time.After(50 * time.Millisecond):
		}

		// Drain wild.
		var received []string
		for i := 0; i < 2; i++ {
			select {
			case got := <-wild.C():
				var payload TurnPayload
				require.NoError(t, json.Unmarshal(got.Data, &payload))
				received = append(received, payload.Turn.SessionID)
			case <-time.After(time.Second):
				t.Fatalf("wild subscriber timed out")
			}
		}
		require.ElementsMatch(t, []string{"sess-1", "sess-2"}, received)
	})

	t.Run("slow consumer drops events without blocking", func(t *testing.T) {
		hub := NewHub(nil)
		sub := hub.Subscribe(Filter{})
		defer hub.Unsubscribe(sub)

		// Hammer far past the buffer so some events must be dropped.
		const total = subscriberBufferSize * 4
		var wg sync.WaitGroup
		wg.Add(1)
		done := make(chan struct{})
		go func() {
			defer wg.Done()
			for i := 0; i < total; i++ {
				hub.Broadcast(makeTurnEvent(t, "sess-1", "demo"))
			}
			close(done)
		}()

		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Fatalf("producer was blocked by slow consumer")
		}
		wg.Wait()

		require.Greater(t, hub.Dropped(), uint64(0), "expected some drops under back-pressure")

		// Drain whatever the buffer captured — should never exceed cap.
		drained := 0
	drain:
		for {
			select {
			case <-sub.C():
				drained++
			default:
				break drain
			}
		}
		require.LessOrEqual(t, drained, subscriberBufferSize)
	})
}

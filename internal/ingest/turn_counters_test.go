package ingest

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestTurnCounterDebouncerCoalesces verifies the core coalescing
// guarantee: N rapid schedules within the debounce window collapse to a
// single flush carrying the latest tuple.
func TestTurnCounterDebouncerCoalesces(t *testing.T) {
	var flushes atomic.Int32
	var mu sync.Mutex
	var last pendingTurnUpdate
	flush := func(_ context.Context, u pendingTurnUpdate) {
		flushes.Add(1)
		mu.Lock()
		last = u
		mu.Unlock()
	}
	d := newTurnCounterDebouncer(flush, 40*time.Millisecond)
	ctx := context.Background()

	for i := 0; i < 50; i++ {
		d.schedule(ctx, pendingTurnUpdate{turnID: "t1", tools: i + 1})
		time.Sleep(time.Millisecond)
	}
	time.Sleep(150 * time.Millisecond)
	require.Equal(t, int32(1), flushes.Load(), "expected exactly one flush after coalescing")
	mu.Lock()
	require.Equal(t, 50, last.tools, "flushed update should carry latest counters")
	mu.Unlock()
}

// TestTurnCounterDebouncerCancelTurn asserts cancelTurn drops the
// pending entry so closeTurn's terminal UpdateTurnStatus is not
// clobbered by a stale running-state flush.
func TestTurnCounterDebouncerCancelTurn(t *testing.T) {
	var flushes atomic.Int32
	flush := func(_ context.Context, _ pendingTurnUpdate) {
		flushes.Add(1)
	}
	d := newTurnCounterDebouncer(flush, 40*time.Millisecond)
	ctx := context.Background()

	d.schedule(ctx, pendingTurnUpdate{turnID: "t2", tools: 3})
	d.cancelTurn("t2")
	time.Sleep(80 * time.Millisecond)
	require.Equal(t, int32(0), flushes.Load())
}

// TestTurnCounterDebouncerStopFlushes asserts Stop drains pending
// entries synchronously — ingest tests rely on this when tearing down
// the reconstructor in the middle of a turn.
func TestTurnCounterDebouncerStopFlushes(t *testing.T) {
	var flushes atomic.Int32
	flush := func(_ context.Context, _ pendingTurnUpdate) {
		flushes.Add(1)
	}
	d := newTurnCounterDebouncer(flush, time.Second)
	ctx := context.Background()
	d.schedule(ctx, pendingTurnUpdate{turnID: "t3", tools: 1})
	d.schedule(ctx, pendingTurnUpdate{turnID: "t4", tools: 1})
	d.Stop(ctx)
	require.Equal(t, int32(2), flushes.Load())
}

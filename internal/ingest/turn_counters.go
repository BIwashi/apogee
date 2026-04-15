package ingest

import (
	"context"
	"sync"
	"time"
)

// turnCounterDebounce is the coalescing window the reconstructor uses for
// per-turn counter writes. A tool-heavy turn (50+ Bash/Edit calls in
// rapid succession) would otherwise produce one DuckDB UPDATE and one SSE
// broadcast per tool call; we quiet the storm by letting the latest
// counter tuple linger for this long before it actually hits the store.
//
// The value has to be smaller than the dashboard's 2 s active-turns poll
// so operators never feel the lag; 250 ms is well below the threshold
// where a human would notice a stale counter.
const turnCounterDebounce = 250 * time.Millisecond

// pendingTurnUpdate is the snapshot the debouncer holds between schedule
// calls. Only the counter fields change — status, started_at, etc. are
// handled by the full update path inside closeTurn.
type pendingTurnUpdate struct {
	turnID    string
	sessionID string
	tools     int
	subagents int
	errors    int
}

// turnCounterDebouncer coalesces flushes of pendingTurnUpdate values on a
// per-turn basis. Each live turn has at most one in-flight timer; when
// schedule is called again before the timer fires the entry is replaced
// in-place so only the latest tuple survives to the flush.
//
// Stop drains every pending entry synchronously so the reconstructor can
// be torn down without losing the tail of an active turn's counters.
type turnCounterDebouncer struct {
	mu       sync.Mutex
	pending  map[string]*debouncerEntry
	flush    func(ctx context.Context, update pendingTurnUpdate)
	window   time.Duration
	stopped  bool
	stopOnce sync.Once
}

type debouncerEntry struct {
	update pendingTurnUpdate
	ctx    context.Context
	timer  *time.Timer
}

// newTurnCounterDebouncer builds a debouncer backed by flush and the
// supplied coalescing window. flush is always called from a timer
// goroutine (never from schedule itself), so the caller may safely block
// on DuckDB without back-pressuring ingest.
func newTurnCounterDebouncer(flush func(ctx context.Context, update pendingTurnUpdate), window time.Duration) *turnCounterDebouncer {
	if window <= 0 {
		window = turnCounterDebounce
	}
	return &turnCounterDebouncer{
		pending: map[string]*debouncerEntry{},
		flush:   flush,
		window:  window,
	}
}

// schedule registers (or replaces) the pending update for update.turnID.
// It returns immediately; the flush timer fires after the debounce
// window of quiet.
func (d *turnCounterDebouncer) schedule(ctx context.Context, update pendingTurnUpdate) {
	if update.turnID == "" {
		return
	}
	d.mu.Lock()
	if d.stopped {
		d.mu.Unlock()
		// On a stopped debouncer we short-circuit straight to the
		// flush path so tests that tear down the reconstructor don't
		// silently drop counter writes.
		d.flush(ctx, update)
		return
	}
	entry, ok := d.pending[update.turnID]
	if ok {
		entry.update = update
		entry.ctx = ctx
		// Stop + Reset is the canonical "debounce" pattern. We do not
		// care about the return value: if the timer already fired, the
		// goroutine will find the entry missing and early-exit.
		if !entry.timer.Stop() {
			// Drain the channel if the timer had already fired — but
			// time.Timer.C is not buffered in a way we can drain here.
			// Instead we accept the small risk that an already-fired
			// timer causes an extra flush by noting that the in-flight
			// goroutine will synchronise on d.mu below.
		}
		entry.timer.Reset(d.window)
		d.mu.Unlock()
		return
	}
	entry = &debouncerEntry{update: update, ctx: ctx}
	entry.timer = time.AfterFunc(d.window, func() {
		d.fire(update.turnID)
	})
	d.pending[update.turnID] = entry
	d.mu.Unlock()
}

// fire is the timer callback. It removes the entry for turnID from the
// pending map and invokes flush.
func (d *turnCounterDebouncer) fire(turnID string) {
	d.mu.Lock()
	entry, ok := d.pending[turnID]
	if !ok {
		d.mu.Unlock()
		return
	}
	delete(d.pending, turnID)
	d.mu.Unlock()
	d.flush(entry.ctx, entry.update)
}

// cancelTurn drops any pending entry for the given turn id. Called by
// closeTurn so the terminal UpdateTurnStatus write is not clobbered by a
// debounced "running" write firing after closeTurn returns.
func (d *turnCounterDebouncer) cancelTurn(turnID string) {
	if turnID == "" {
		return
	}
	d.mu.Lock()
	if entry, ok := d.pending[turnID]; ok {
		entry.timer.Stop()
		delete(d.pending, turnID)
	}
	d.mu.Unlock()
}

// cancelSession drops every pending entry whose session matches
// sessionID. The reconstructor calls this from handleSessionEnd so a
// scheduled flush does not fire against a turn the store no longer
// recognises.
func (d *turnCounterDebouncer) cancelSession(sessionID string) {
	if sessionID == "" {
		return
	}
	d.mu.Lock()
	for id, entry := range d.pending {
		if entry.update.sessionID == sessionID {
			entry.timer.Stop()
			delete(d.pending, id)
		}
	}
	d.mu.Unlock()
}

// Flush drains every pending entry synchronously on the calling
// goroutine. Used by Stop and by tests that need deterministic writes
// before asserting on the store.
func (d *turnCounterDebouncer) Flush(ctx context.Context) {
	d.mu.Lock()
	entries := make([]*debouncerEntry, 0, len(d.pending))
	for id, entry := range d.pending {
		if entry.timer != nil {
			entry.timer.Stop()
		}
		entries = append(entries, entry)
		delete(d.pending, id)
	}
	d.mu.Unlock()
	for _, entry := range entries {
		useCtx := entry.ctx
		if useCtx == nil {
			useCtx = ctx
		}
		d.flush(useCtx, entry.update)
	}
}

// Stop drains every pending entry and marks the debouncer closed so
// subsequent schedule calls fall through to a direct flush.
func (d *turnCounterDebouncer) Stop(ctx context.Context) {
	d.stopOnce.Do(func() {
		d.Flush(ctx)
		d.mu.Lock()
		d.stopped = true
		d.mu.Unlock()
	})
}

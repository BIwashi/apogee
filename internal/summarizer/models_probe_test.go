package summarizer

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"
)

// countingRunner is a Runner stub that records the maximum number of
// concurrent Run calls it sees and returns canned responses per model.
type countingRunner struct {
	responses map[string]countingResult
	inflight  int64
	maxSeen   int64
	started   int64
}

type countingResult struct {
	out   string
	err   error
	delay time.Duration
}

func (c *countingRunner) Run(ctx context.Context, model, _ string) (string, error) {
	inflight := atomic.AddInt64(&c.inflight, 1)
	atomic.AddInt64(&c.started, 1)
	defer atomic.AddInt64(&c.inflight, -1)
	for {
		cur := atomic.LoadInt64(&c.maxSeen)
		if inflight <= cur {
			break
		}
		if atomic.CompareAndSwapInt64(&c.maxSeen, cur, inflight) {
			break
		}
	}
	res, ok := c.responses[model]
	if !ok {
		return "", fmt.Errorf("no canned response for %q", model)
	}
	if res.delay > 0 {
		select {
		case <-time.After(res.delay):
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}
	return res.out, res.err
}

func TestProbe_MixedResults(t *testing.T) {
	// Map each current catalog entry to a deterministic outcome.
	runner := &countingRunner{responses: map[string]countingResult{}}
	for _, m := range KnownModels {
		if m.Status != StatusCurrent {
			continue
		}
		switch m.Alias {
		case "claude-haiku-4-5":
			runner.responses[m.Alias] = countingResult{out: "ok"}
		case "claude-sonnet-4-6":
			runner.responses[m.Alias] = countingResult{out: "ok"}
		case "claude-opus-4-6":
			runner.responses[m.Alias] = countingResult{err: errors.New("model unavailable")}
		default:
			runner.responses[m.Alias] = countingResult{out: "ok"}
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	got := Probe(ctx, runner, nil)

	if !got["claude-haiku-4-5"] {
		t.Errorf("probe should mark claude-haiku-4-5 available")
	}
	if !got["claude-sonnet-4-6"] {
		t.Errorf("probe should mark claude-sonnet-4-6 available")
	}
	if got["claude-opus-4-6"] {
		t.Errorf("probe should mark claude-opus-4-6 unavailable")
	}
	// Legacy entries are NOT probed; they must be absent from the map.
	if _, ok := got["claude-haiku-3-5"]; ok {
		t.Errorf("legacy alias claude-haiku-3-5 should not appear in probe result")
	}
	if _, ok := got["claude-sonnet-3-7"]; ok {
		t.Errorf("legacy alias claude-sonnet-3-7 should not appear in probe result")
	}
}

func TestProbe_ConcurrencyCap(t *testing.T) {
	// Build a synthetic catalog of 8 "current" models with a slow
	// runner so we can observe the concurrency cap. We temporarily
	// swap KnownModels, run Probe, and restore — the package-level
	// slice is the authoritative input so the test has to touch it.
	original := KnownModels
	t.Cleanup(func() { KnownModels = original })

	synthetic := make([]ModelInfo, 0, 8)
	for i := 0; i < 8; i++ {
		synthetic = append(synthetic, ModelInfo{
			Alias:       fmt.Sprintf("claude-sonnet-test-%d", i),
			Family:      "sonnet",
			Generation:  fmt.Sprintf("test-%d", i),
			Display:     fmt.Sprintf("Test %d", i),
			Tier:        1,
			Recommended: []ModelUseCase{UseCaseRecap},
			Status:      StatusCurrent,
		})
	}
	KnownModels = synthetic

	runner := &countingRunner{responses: map[string]countingResult{}}
	for _, m := range synthetic {
		runner.responses[m.Alias] = countingResult{out: "ok", delay: 80 * time.Millisecond}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	got := Probe(ctx, runner, nil)
	if len(got) != 8 {
		t.Errorf("expected 8 probe results, got %d", len(got))
	}
	if runner.started != 8 {
		t.Errorf("expected 8 runner calls, got %d", runner.started)
	}
	if runner.maxSeen > probeConcurrency {
		t.Errorf("probe concurrency cap breached: saw %d inflight, cap=%d", runner.maxSeen, probeConcurrency)
	}
}

func TestProbe_NilRunnerYieldsEmptyMap(t *testing.T) {
	got := Probe(context.Background(), nil, nil)
	if len(got) != 0 {
		t.Errorf("nil runner should yield empty map, got %v", got)
	}
}

func TestProbe_TimeoutMarksUnavailable(t *testing.T) {
	// Use a synthetic catalog of one entry with a delay longer than
	// the per-model probe timeout so the probe context expires.
	original := KnownModels
	t.Cleanup(func() { KnownModels = original })
	KnownModels = []ModelInfo{{
		Alias:       "claude-sonnet-slow",
		Family:      "sonnet",
		Generation:  "slow",
		Display:     "Slow",
		Tier:        1,
		Recommended: []ModelUseCase{UseCaseRecap},
		Status:      StatusCurrent,
	}}
	runner := &countingRunner{
		responses: map[string]countingResult{
			"claude-sonnet-slow": {out: "ok", delay: probePerModelTimeout + 2*time.Second},
		},
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	got := Probe(ctx, runner, nil)
	if got["claude-sonnet-slow"] {
		t.Errorf("slow model should be marked unavailable")
	}
}

// Compile-time assertion that countingRunner satisfies Runner.
var _ Runner = (*countingRunner)(nil)

package summarizer

import (
	"context"
	"io"
	"log/slog"
	"sync"
	"time"
)

// probeConcurrency caps how many probe goroutines are inflight at once.
// Keep it low: each probe shells out to `claude` and we do not want to
// saturate a laptop when the catalog grows.
const probeConcurrency = 4

// probePerModelTimeout bounds a single `claude --model <alias>` call.
// The probe is best-effort — a slow model is treated as unavailable so
// the wizard does not stall on boot.
const probePerModelTimeout = 5 * time.Second

// probePrompt is the trivial input sent to the CLI during a probe.
// Anything parseable works; we just need the CLI to accept the
// --model alias and return a non-error envelope.
const probePrompt = "ping"

// Probe runs `claude -p "ping" --model <alias> --max-turns 1 --output-format=json`
// against every StatusCurrent KnownModel entry in parallel with a 5-second
// per-model timeout. Returns a map[alias]bool: true if the CLI accepted
// the alias and returned a valid response, false if it errored.
//
// Legacy models are NOT probed — they stay in the catalog as explicit
// fallbacks the user can choose, but the probe only exercises
// StatusCurrent entries to keep the cost bounded.
//
// Every probe error is logged at DEBUG with the alias and a trimmed
// error message so operators can see why a specific model got filtered
// out. A nil runner or nil logger is tolerated.
func Probe(ctx context.Context, runner Runner, logger *slog.Logger) map[string]bool {
	out := map[string]bool{}
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelError}))
	}
	if runner == nil {
		logger.Debug("model probe: skipped — no runner configured")
		return out
	}

	// Collect the aliases we are going to exercise up front so we
	// don't iterate the package-level slice under the result lock.
	var aliases []string
	for _, m := range KnownModels {
		if m.Status == StatusCurrent {
			aliases = append(aliases, m.Alias)
		}
	}
	if len(aliases) == 0 {
		return out
	}

	sem := make(chan struct{}, probeConcurrency)
	var mu sync.Mutex
	var wg sync.WaitGroup

	for _, alias := range aliases {
		alias := alias
		sem <- struct{}{}
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() { <-sem }()

			probeCtx, cancel := context.WithTimeout(ctx, probePerModelTimeout)
			defer cancel()

			_, err := runner.Run(probeCtx, alias, probePrompt)
			ok := err == nil
			mu.Lock()
			out[alias] = ok
			mu.Unlock()

			if err != nil {
				logger.Debug("model probe: unavailable",
					"alias", alias,
					"err", truncate(err.Error(), 160),
				)
				return
			}
			logger.Debug("model probe: available", "alias", alias)
		}()
	}
	wg.Wait()
	return out
}

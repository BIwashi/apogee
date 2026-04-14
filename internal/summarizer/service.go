package summarizer

import (
	"context"
	"log/slog"

	"github.com/BIwashi/apogee/internal/sse"
	"github.com/BIwashi/apogee/internal/store/duckdb"
)

// Service is the top-level handle the collector wires into its HTTP
// server. One Service per apogee process. Service owns the Runner and
// Worker lifecycles.
type Service struct {
	cfg    Config
	runner Runner
	worker *Worker
	logger *slog.Logger
}

// NewService wires the Config-derived Runner (a CLIRunner) into a Worker.
// Callers may override the runner via NewServiceWithRunner — useful for
// tests that need deterministic output.
func NewService(cfg Config, store *duckdb.Store, hub *sse.Hub, logger *slog.Logger) *Service {
	runner := NewCLIRunner(cfg.CLIPath, cfg.Timeout, logger)
	return NewServiceWithRunner(cfg, runner, store, hub, logger)
}

// NewServiceWithRunner is NewService with an explicit runner.
func NewServiceWithRunner(cfg Config, runner Runner, store *duckdb.Store, hub *sse.Hub, logger *slog.Logger) *Service {
	worker := NewWorker(cfg, runner, store, hub, logger)
	return &Service{cfg: cfg, runner: runner, worker: worker, logger: logger}
}

// Config returns the service's immutable configuration snapshot.
func (s *Service) Config() Config { return s.cfg }

// Worker exposes the underlying worker for advanced tests.
func (s *Service) Worker() *Worker { return s.worker }

// Start spawns the worker goroutines. No-op when Enabled is false.
func (s *Service) Start(ctx context.Context) {
	if s == nil || !s.cfg.Enabled {
		return
	}
	s.logger.Info("summarizer: starting",
		"model", s.cfg.RecapModel,
		"concurrency", s.cfg.Concurrency,
		"cli", s.cfg.CLIPath,
	)
	s.worker.Start(ctx)
}

// Stop drains the queue and waits for inflight jobs.
func (s *Service) Stop() {
	if s == nil || !s.cfg.Enabled {
		return
	}
	s.worker.Stop()
}

// Enqueue is the public API the reconstructor hook uses. Non-blocking.
func (s *Service) Enqueue(turnID, reason string) {
	if s == nil || !s.cfg.Enabled {
		return
	}
	s.worker.Enqueue(turnID, reason)
}

package summarizer

import (
	"context"
	"io"
	"log/slog"
	"time"

	"github.com/BIwashi/apogee/internal/sse"
	"github.com/BIwashi/apogee/internal/store/duckdb"
)

// Service is the top-level handle the collector wires into its HTTP
// server. One Service per apogee process. Service owns the Runner and
// Worker lifecycles for both tiers (per-turn recap and per-session rollup).
type Service struct {
	cfg          Config
	runner       Runner
	worker       *Worker
	rollup       *RollupWorker
	narrative    *NarrativeWorker
	liveStatus   *LiveStatusWorker
	agentSummary *AgentSummaryWorker
	store        *duckdb.Store
	hub          *sse.Hub
	logger       *slog.Logger
	prefs        PreferencesReader
	stopSch      context.CancelFunc
}

// NewService wires the Config-derived Runner (a CLIRunner) into both the
// turn recap worker and the session rollup worker. Callers may override the
// runner via NewServiceWithRunner — useful for tests that need deterministic
// output.
func NewService(cfg Config, store *duckdb.Store, hub *sse.Hub, logger *slog.Logger) *Service {
	runner := NewCLIRunner(cfg.CLIPath, cfg.Timeout, logger)
	return NewServiceWithRunner(cfg, runner, store, hub, logger)
}

// NewServiceWithRunner is NewService with an explicit runner.
func NewServiceWithRunner(cfg Config, runner Runner, store *duckdb.Store, hub *sse.Hub, logger *slog.Logger) *Service {
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelError}))
	}
	worker := NewWorker(cfg, runner, store, hub, logger)
	rollup := NewRollupWorker(cfg, runner, store, hub, logger)
	narrative := NewNarrativeWorker(cfg, runner, store, hub, logger)
	liveStatus := NewLiveStatusWorker(cfg, runner, store, hub, logger)
	agentSummary := NewAgentSummaryWorker(cfg, runner, store, hub, logger)
	// Default preferences reader pulls from the DuckDB user_preferences
	// table so language / system prompt / model overrides applied via
	// the /v1/preferences API or the /settings page take effect on the
	// next job without a restart. Store-less callers (tests) can swap
	// it via SetPreferencesReader.
	prefs := PreferencesReader(NewStaticPreferencesReader(Defaults()))
	if store != nil {
		prefs = NewDuckDBPreferencesReader(store)
	}
	worker.SetPreferencesReader(prefs)
	rollup.SetPreferencesReader(prefs)
	narrative.SetPreferencesReader(prefs)
	agentSummary.SetPreferencesReader(prefs)
	// Chain tier-2 → tier-3: after a rollup lands the narrative worker
	// is enqueued with reason "session_rollup" so phases are computed
	// immediately. Tier-2 → agent-summary: every agent in the session
	// gets refreshed in parallel.
	rollup.SetOnRollupWritten(func(sessionID string) {
		narrative.Enqueue(sessionID, NarrativeReasonSessionRollup)
		agentSummary.EnqueueSession(context.Background(), sessionID, AgentSummaryReasonSessionRollup)
	})
	return &Service{
		cfg:          cfg,
		runner:       runner,
		worker:       worker,
		rollup:       rollup,
		narrative:    narrative,
		liveStatus:   liveStatus,
		agentSummary: agentSummary,
		store:        store,
		hub:          hub,
		logger:       logger,
		prefs:        prefs,
	}
}

// SetPreferencesReader overrides the default DuckDB-backed reader. Useful
// for tests that need deterministic prompt content. nil is a no-op.
func (s *Service) SetPreferencesReader(r PreferencesReader) {
	if s == nil || r == nil {
		return
	}
	s.prefs = r
	if s.worker != nil {
		s.worker.SetPreferencesReader(r)
	}
	if s.rollup != nil {
		s.rollup.SetPreferencesReader(r)
	}
	if s.narrative != nil {
		s.narrative.SetPreferencesReader(r)
	}
	if s.agentSummary != nil {
		s.agentSummary.SetPreferencesReader(r)
	}
}

// PreferencesReader exposes the current reader for advanced tests.
func (s *Service) PreferencesReader() PreferencesReader { return s.prefs }

// Config returns the service's immutable configuration snapshot.
func (s *Service) Config() Config { return s.cfg }

// Worker exposes the underlying turn recap worker for advanced tests.
func (s *Service) Worker() *Worker { return s.worker }

// Rollup exposes the underlying rollup worker for advanced tests.
func (s *Service) Rollup() *RollupWorker { return s.rollup }

// Narrative exposes the underlying narrative worker for advanced tests.
func (s *Service) Narrative() *NarrativeWorker { return s.narrative }

// LiveStatus exposes the underlying live-status worker for advanced tests.
func (s *Service) LiveStatus() *LiveStatusWorker { return s.liveStatus }

// AgentSummary exposes the underlying agent-summary worker for advanced tests.
func (s *Service) AgentSummary() *AgentSummaryWorker { return s.agentSummary }

// Runner exposes the underlying CLI runner so the /v1/models handler
// can reuse it for the model probe. nil when the service was not
// constructed.
func (s *Service) Runner() Runner {
	if s == nil {
		return nil
	}
	return s.runner
}

// SetAvailability fans the latest model availability snapshot out to
// every worker so the next scheduled job picks the right default.
// Safe to call from any goroutine.
func (s *Service) SetAvailability(avail map[string]bool) {
	if s == nil {
		return
	}
	if s.worker != nil {
		s.worker.SetAvailability(avail)
	}
	if s.rollup != nil {
		s.rollup.SetAvailability(avail)
	}
	if s.narrative != nil {
		s.narrative.SetAvailability(avail)
	}
	if s.liveStatus != nil {
		s.liveStatus.SetAvailability(avail)
	}
	if s.agentSummary != nil {
		s.agentSummary.SetAvailability(avail)
	}
}

// Start spawns both worker pools and the rollup scheduler. No-op when
// Enabled is false.
func (s *Service) Start(ctx context.Context) {
	if s == nil || !s.cfg.Enabled {
		return
	}
	s.logger.Info("summarizer: starting",
		"recap_model", s.cfg.RecapModel,
		"rollup_model", s.cfg.RollupModel,
		"concurrency", s.cfg.Concurrency,
		"cli", s.cfg.CLIPath,
	)
	s.worker.Start(ctx)
	s.rollup.Start(ctx)
	s.narrative.Start(ctx)
	s.liveStatus.Start(ctx)
	s.agentSummary.Start(ctx)

	if s.cfg.RollupSchedulerEnabled {
		schedCtx, cancel := context.WithCancel(ctx)
		s.stopSch = cancel
		go s.runRollupScheduler(schedCtx)
	}
}

// Stop drains the queues and waits for inflight jobs.
func (s *Service) Stop() {
	if s == nil || !s.cfg.Enabled {
		return
	}
	if s.stopSch != nil {
		s.stopSch()
	}
	s.worker.Stop()
	s.rollup.Stop()
	s.narrative.Stop()
	s.liveStatus.Stop()
	s.agentSummary.Stop()
}

// EnqueueLiveStatus is the public API the reconstructor hook calls
// when a span lands on a live session. Non-blocking.
func (s *Service) EnqueueLiveStatus(sessionID, reason string) {
	if s == nil || !s.cfg.Enabled {
		return
	}
	s.liveStatus.Enqueue(sessionID, reason)
}

// EnqueueNarrative is the public API for tier-3 phase digests. Non-blocking.
func (s *Service) EnqueueNarrative(sessionID, reason string) {
	if s == nil || !s.cfg.Enabled {
		return
	}
	s.narrative.Enqueue(sessionID, reason)
}

// Enqueue is the public API the reconstructor hook uses for per-turn recaps.
// Non-blocking.
func (s *Service) Enqueue(turnID, reason string) {
	if s == nil || !s.cfg.Enabled {
		return
	}
	s.worker.Enqueue(turnID, reason)
}

// EnqueueRollup is the public API for per-session digests. Non-blocking.
func (s *Service) EnqueueRollup(sessionID, reason string) {
	if s == nil || !s.cfg.Enabled {
		return
	}
	s.rollup.Enqueue(sessionID, reason)
}

// EnqueueAgentSummary is the public API for one-off agent label refreshes.
// Non-blocking.
func (s *Service) EnqueueAgentSummary(agentID, sessionID, reason string) {
	if s == nil || !s.cfg.Enabled {
		return
	}
	s.agentSummary.Enqueue(agentID, sessionID, reason)
}

// EnqueueAgentSummariesForSession fans out to every agent in the session that
// either has no summary or whose summary is stale. Non-blocking — the actual
// fan-out is synchronous because it queries the candidate list inline, but
// the per-agent jobs are async.
func (s *Service) EnqueueAgentSummariesForSession(ctx context.Context, sessionID, reason string) {
	if s == nil || !s.cfg.Enabled {
		return
	}
	s.agentSummary.EnqueueSession(ctx, sessionID, reason)
}

// rollupSchedulerInterval is the wall-clock cadence at which the background
// scheduler scans for sessions needing a fresh rollup.
const rollupSchedulerInterval = time.Hour

// rollupSchedulerBatch caps how many sessions one tick will enqueue.
const rollupSchedulerBatch = 5

// runRollupScheduler is the background loop that picks up to N stale
// sessions per tick and enqueues them for a rollup. It runs forever until
// ctx is cancelled.
func (s *Service) runRollupScheduler(ctx context.Context) {
	ticker := time.NewTicker(rollupSchedulerInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.scheduleRollupBatch(ctx)
		}
	}
}

func (s *Service) scheduleRollupBatch(ctx context.Context) {
	candidates, err := s.store.ListRollupCandidates(ctx, 2, rollupStaleness, rollupSchedulerBatch)
	if err != nil {
		s.logger.Warn("rollup scheduler: list candidates", "err", err)
		return
	}
	if len(candidates) == 0 {
		return
	}
	s.logger.Info("rollup scheduler: enqueueing batch", "count", len(candidates))
	for _, c := range candidates {
		s.EnqueueRollup(c.SessionID, RollupReasonScheduled)
	}
}

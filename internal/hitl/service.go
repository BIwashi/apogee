// Package hitl owns the apogee collector's structured Human-In-The-Loop
// lifecycle. It wraps the duckdb store with auto-expiration ticking and SSE
// fan-out so the rest of the collector can think about HITL as discrete
// rows with state transitions instead of free-form span attributes.
package hitl

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/BIwashi/apogee/internal/sse"
	"github.com/BIwashi/apogee/internal/store/duckdb"
)

// Config is the immutable tunable set the service reads at construction
// time. Zero values fall back to safe defaults.
type Config struct {
	// AutoExpireSeconds bounds how long a pending HITL row may live before
	// the expiration ticker flips it to status=expired. 0 disables the
	// kill switch.
	AutoExpireSeconds int
	// TickInterval is how often the expiration loop wakes up.
	TickInterval time.Duration
}

// DefaultConfig returns the production defaults: a 5 minute kill switch
// and a 10 second tick.
func DefaultConfig() Config {
	return Config{
		AutoExpireSeconds: 300,
		TickInterval:      10 * time.Second,
	}
}

// Service binds the duckdb store, the SSE hub, and the expiration ticker
// into one cohesive HITL lifecycle owner. The zero value is not usable;
// construct via New.
type Service struct {
	store  *duckdb.Store
	hub    *sse.Hub
	cfg    Config
	clock  func() time.Time
	logger *slog.Logger

	// CloseHITLSpan is an optional callback the reconstructor wires in so
	// the service can close the matching open HITL span when a response
	// (or expiration) lands. Without this hook the service still updates
	// the typed row but the span tree stays open until the turn closes.
	CloseHITLSpan func(ctx context.Context, ev duckdb.HITLEvent)
}

// New returns a fresh service. logger may be nil (a discard logger is
// installed). clock defaults to time.Now.
func New(store *duckdb.Store, hub *sse.Hub, cfg Config, logger *slog.Logger) *Service {
	if logger == nil {
		logger = slog.Default()
	}
	if cfg.TickInterval <= 0 {
		cfg.TickInterval = DefaultConfig().TickInterval
	}
	return &Service{
		store:  store,
		hub:    hub,
		cfg:    cfg,
		clock:  time.Now,
		logger: logger,
	}
}

// SetClock overrides the wall-clock source used by the service. Intended
// for tests that need to pin "now".
func (s *Service) SetClock(c func() time.Time) {
	if c != nil {
		s.clock = c
	}
}

// Config returns the service's immutable config snapshot.
func (s *Service) Config() Config { return s.cfg }

// Start spawns the expiration ticker. The goroutine exits when ctx is
// cancelled. Calling Start more than once is allowed but does not stack
// — only the most recent context governs lifecycle. No-op when the kill
// switch is disabled.
func (s *Service) Start(ctx context.Context) {
	if s == nil || s.store == nil {
		return
	}
	if s.cfg.AutoExpireSeconds <= 0 {
		return
	}
	go s.runExpirationLoop(ctx)
}

func (s *Service) runExpirationLoop(ctx context.Context) {
	ticker := time.NewTicker(s.cfg.TickInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.expireOnce(ctx)
		}
	}
}

// ExpireOnce runs one pass of the expiration sweep. Exposed for tests so
// they do not have to wait for the ticker.
func (s *Service) ExpireOnce(ctx context.Context) { s.expireOnce(ctx) }

func (s *Service) expireOnce(ctx context.Context) {
	if s.cfg.AutoExpireSeconds <= 0 {
		return
	}
	now := s.clock()
	cutoff := now.Add(-time.Duration(s.cfg.AutoExpireSeconds) * time.Second)
	candidates, err := s.store.ListExpiredCandidates(ctx, cutoff, 100)
	if err != nil {
		s.logger.Debug("hitl: list expired candidates", "err", err)
		return
	}
	for _, ev := range candidates {
		if err := s.store.ExpireHITL(ctx, ev.HitlID, now); err != nil {
			s.logger.Debug("hitl: expire", "hitl_id", ev.HitlID, "err", err)
			continue
		}
		updated, ok, err := s.store.GetHITL(ctx, ev.HitlID)
		if err != nil || !ok {
			continue
		}
		if s.CloseHITLSpan != nil {
			s.CloseHITLSpan(ctx, updated)
		}
		s.broadcast(sse.EventHITLExpired, updated)
	}
}

// Respond is the public API the HTTP handler calls when an operator
// submits a response. It writes the row, closes the span (via the
// reconstructor callback), and broadcasts hitl.responded.
func (s *Service) Respond(ctx context.Context, hitlID string, resp duckdb.HITLResponse) (duckdb.HITLEvent, error) {
	if hitlID == "" {
		return duckdb.HITLEvent{}, errors.New("hitl: hitl_id required")
	}
	if resp.Decision == "" {
		return duckdb.HITLEvent{}, errors.New("hitl: decision required")
	}
	now := s.clock()
	ev, err := s.store.RespondHITL(ctx, hitlID, resp, now)
	if err != nil {
		return duckdb.HITLEvent{}, err
	}
	if s.CloseHITLSpan != nil {
		s.CloseHITLSpan(ctx, ev)
	}
	s.broadcast(sse.EventHITLResponded, ev)
	return ev, nil
}

// BroadcastRequested fan-outs a hitl.requested event. Called by the
// reconstructor immediately after it inserts a fresh pending row.
func (s *Service) BroadcastRequested(ev duckdb.HITLEvent) {
	s.broadcast(sse.EventHITLRequested, ev)
}

func (s *Service) broadcast(kind string, ev duckdb.HITLEvent) {
	if s.hub == nil {
		return
	}
	s.hub.Broadcast(sse.NewHITLEvent(kind, s.clock(), ev))
}

// Package interventions owns the apogee collector's operator-initiated
// intervention lifecycle. It wraps the duckdb store with auto-expiration
// ticking and SSE fan-out so the rest of the collector can think about
// interventions as discrete rows with state transitions.
package interventions

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/BIwashi/apogee/internal/sse"
	"github.com/BIwashi/apogee/internal/store/duckdb"
)

// Config is the immutable tunable set the service reads at construction time.
type Config struct {
	// AutoExpireTTL is the default time-to-live applied when
	// InterventionRequest.TTL is zero. Also used as the upper bound of the
	// background sweeper's "still-pending" window.
	AutoExpireTTL time.Duration
	// SweepInterval is how often the expiration loop wakes up.
	SweepInterval time.Duration
	// BothFallbackAfter is the window after which a "both"-mode intervention
	// falls back to the UserPromptSubmit delivery path. Currently tracked
	// for documentation; the store's selection rules already allow either
	// path to claim a "both" row immediately.
	BothFallbackAfter time.Duration
	// MaxMessageChars enforces an upper bound on the operator-supplied
	// message. 0 disables the limit.
	MaxMessageChars int
}

// DefaultConfig returns production defaults.
func DefaultConfig() Config {
	return Config{
		AutoExpireTTL:     10 * time.Minute,
		SweepInterval:     15 * time.Second,
		BothFallbackAfter: 60 * time.Second,
		MaxMessageChars:   4096,
	}
}

// Service binds the duckdb store, the SSE hub, and the expiration ticker
// into one cohesive intervention lifecycle owner.
type Service struct {
	store  *duckdb.Store
	hub    *sse.Hub
	cfg    Config
	clock  func() time.Time
	logger *slog.Logger

	mu       sync.Mutex
	stopFn   context.CancelFunc
	running  bool
	stoppedC chan struct{}
}

// Validation errors returned by Submit.
var (
	ErrMessageRequired     = errors.New("intervention: message is required")
	ErrMessageTooLong      = errors.New("intervention: message exceeds max length")
	ErrInvalidDeliveryMode = errors.New("intervention: invalid delivery_mode")
	ErrInvalidScope        = errors.New("intervention: invalid scope")
	ErrInvalidUrgency      = errors.New("intervention: invalid urgency")
	ErrSessionRequired     = errors.New("intervention: session_id is required")
)

// NewService builds a Service. logger may be nil. clock defaults to time.Now.
func NewService(cfg Config, store *duckdb.Store, hub *sse.Hub, logger *slog.Logger) *Service {
	if logger == nil {
		logger = slog.Default()
	}
	if cfg.AutoExpireTTL <= 0 {
		cfg.AutoExpireTTL = DefaultConfig().AutoExpireTTL
	}
	if cfg.SweepInterval <= 0 {
		cfg.SweepInterval = DefaultConfig().SweepInterval
	}
	if cfg.BothFallbackAfter <= 0 {
		cfg.BothFallbackAfter = DefaultConfig().BothFallbackAfter
	}
	if cfg.MaxMessageChars == 0 {
		cfg.MaxMessageChars = DefaultConfig().MaxMessageChars
	}
	return &Service{
		store:  store,
		hub:    hub,
		cfg:    cfg,
		clock:  time.Now,
		logger: logger,
	}
}

// Config returns the service's immutable config snapshot.
func (s *Service) Config() Config { return s.cfg }

// SetClock overrides the wall-clock source. Intended for tests.
func (s *Service) SetClock(c func() time.Time) {
	if c != nil {
		s.clock = c
	}
}

// Start spawns the expiration sweeper. Calling Start more than once is a
// no-op until Stop has been called.
func (s *Service) Start(ctx context.Context) {
	if s == nil || s.store == nil {
		return
	}
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return
	}
	loopCtx, cancel := context.WithCancel(ctx)
	s.stopFn = cancel
	s.running = true
	s.stoppedC = make(chan struct{})
	s.mu.Unlock()

	go func() {
		defer close(s.stoppedC)
		s.runSweepLoop(loopCtx)
	}()
}

// Stop signals the sweeper to exit and waits for it to drain.
func (s *Service) Stop() {
	if s == nil {
		return
	}
	s.mu.Lock()
	if !s.running {
		s.mu.Unlock()
		return
	}
	stop := s.stopFn
	stopped := s.stoppedC
	s.running = false
	s.mu.Unlock()
	if stop != nil {
		stop()
	}
	if stopped != nil {
		<-stopped
	}
}

func (s *Service) runSweepLoop(ctx context.Context) {
	ticker := time.NewTicker(s.cfg.SweepInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.sweepOnce(ctx)
		}
	}
}

// SweepOnce runs one pass of the expiration sweep. Exposed for tests so they
// do not have to wait for the ticker.
func (s *Service) SweepOnce(ctx context.Context) { s.sweepOnce(ctx) }

func (s *Service) sweepOnce(ctx context.Context) {
	now := s.clock()
	candidates, err := s.store.ListInterventionsToAutoExpire(ctx, now, 100)
	if err != nil {
		s.logger.Debug("interventions: list expire candidates", "err", err)
		return
	}
	for _, iv := range candidates {
		updated, err := s.store.ExpireIntervention(ctx, iv.InterventionID)
		if err != nil {
			s.logger.Debug("interventions: expire", "id", iv.InterventionID, "err", err)
			continue
		}
		s.broadcast(sse.EventInterventionExpired, updated)
	}
}

// Submit validates the operator's request, persists it, and broadcasts a
// submitted event.
func (s *Service) Submit(ctx context.Context, req duckdb.InterventionRequest) (duckdb.Intervention, error) {
	if s == nil || s.store == nil {
		return duckdb.Intervention{}, errors.New("intervention service not configured")
	}
	if req.SessionID == "" {
		return duckdb.Intervention{}, ErrSessionRequired
	}
	req.Message = strings.TrimSpace(req.Message)
	if req.Message == "" {
		return duckdb.Intervention{}, ErrMessageRequired
	}
	if s.cfg.MaxMessageChars > 0 && len(req.Message) > s.cfg.MaxMessageChars {
		return duckdb.Intervention{}, fmt.Errorf("%w (max %d)", ErrMessageTooLong, s.cfg.MaxMessageChars)
	}
	if req.DeliveryMode == "" {
		req.DeliveryMode = duckdb.InterventionModeInterrupt
	}
	if !validDeliveryMode(req.DeliveryMode) {
		return duckdb.Intervention{}, ErrInvalidDeliveryMode
	}
	if req.Scope == "" {
		req.Scope = duckdb.InterventionScopeTurn
	}
	if !validScope(req.Scope) {
		return duckdb.Intervention{}, ErrInvalidScope
	}
	if req.Urgency == "" {
		req.Urgency = duckdb.InterventionUrgencyNormal
	}
	if !validUrgency(req.Urgency) {
		return duckdb.Intervention{}, ErrInvalidUrgency
	}
	// this_session scope must not carry a turn id.
	if req.Scope == duckdb.InterventionScopeSession {
		req.TurnID = ""
	}
	if req.TTL == 0 {
		req.TTL = s.cfg.AutoExpireTTL
	}
	iv, err := s.store.InsertIntervention(ctx, req)
	if err != nil {
		return duckdb.Intervention{}, err
	}
	s.broadcast(sse.EventInterventionSubmitted, iv)
	return iv, nil
}

// Cancel marks a queued/claimed intervention as cancelled.
func (s *Service) Cancel(ctx context.Context, id string) (duckdb.Intervention, error) {
	iv, err := s.store.CancelIntervention(ctx, id)
	if err != nil {
		return duckdb.Intervention{}, err
	}
	s.broadcast(sse.EventInterventionCancelled, iv)
	return iv, nil
}

// Claim runs the atomic claim primitive and broadcasts when one was taken.
// Returns ok=false when nothing matched the hook.
func (s *Service) Claim(ctx context.Context, sessionID, turnID, hookEvent string) (duckdb.Intervention, bool, error) {
	iv, ok, err := s.store.ClaimNextIntervention(ctx, sessionID, turnID, hookEvent)
	if err != nil || !ok {
		return iv, ok, err
	}
	s.broadcast(sse.EventInterventionClaimed, iv)
	return iv, true, nil
}

// Delivered flips a claimed intervention to delivered.
func (s *Service) Delivered(ctx context.Context, id, hookEvent string) (duckdb.Intervention, error) {
	iv, err := s.store.MarkInterventionDelivered(ctx, id, hookEvent)
	if err != nil {
		return duckdb.Intervention{}, err
	}
	s.broadcast(sse.EventInterventionDelivered, iv)
	return iv, nil
}

// Consumed flips a delivered intervention to consumed.
func (s *Service) Consumed(ctx context.Context, id string, logEventID int64) (duckdb.Intervention, error) {
	iv, err := s.store.MarkInterventionConsumed(ctx, id, logEventID)
	if err != nil {
		return duckdb.Intervention{}, err
	}
	s.broadcast(sse.EventInterventionConsumed, iv)
	return iv, nil
}

// ExpireForTurn is called by the reconstructor when a turn closes. All
// pending interventions whose turn_id matches are flipped to expired.
func (s *Service) ExpireForTurn(ctx context.Context, turnID string) error {
	if s == nil || s.store == nil || turnID == "" {
		return nil
	}
	rows, err := s.store.ListPendingInterventionsByTurn(ctx, turnID)
	if err != nil {
		return err
	}
	for _, iv := range rows {
		updated, err := s.store.ExpireIntervention(ctx, iv.InterventionID)
		if err != nil {
			s.logger.Debug("interventions: expire for turn", "id", iv.InterventionID, "err", err)
			continue
		}
		s.broadcast(sse.EventInterventionExpired, updated)
	}
	return nil
}

// ExpireForSession is called by the reconstructor at SessionEnd time. All
// pending interventions for the session are flipped to expired.
func (s *Service) ExpireForSession(ctx context.Context, sessionID string) error {
	if s == nil || s.store == nil || sessionID == "" {
		return nil
	}
	rows, err := s.store.ListPendingInterventionsBySession(ctx, sessionID)
	if err != nil {
		return err
	}
	for _, iv := range rows {
		updated, err := s.store.ExpireIntervention(ctx, iv.InterventionID)
		if err != nil {
			s.logger.Debug("interventions: expire for session", "id", iv.InterventionID, "err", err)
			continue
		}
		s.broadcast(sse.EventInterventionExpired, updated)
	}
	return nil
}

// ObservePostHookConsumption is the heuristic called by the reconstructor on
// every inbound hook event. When any interventions are in status=delivered
// for this session and the current hook event is downstream of a delivery,
// we flip the oldest such row to consumed and attach the log id.
func (s *Service) ObservePostHookConsumption(ctx context.Context, sessionID, hookEvent string, logID int64) {
	if s == nil || s.store == nil || sessionID == "" {
		return
	}
	if !isConsumptionHook(hookEvent) {
		return
	}
	rows, err := s.store.ListPendingInterventionsBySession(ctx, sessionID)
	if err != nil {
		s.logger.Debug("interventions: observe consumption", "err", err)
		return
	}
	for _, iv := range rows {
		if iv.Status != duckdb.InterventionStatusDelivered {
			continue
		}
		updated, err := s.store.MarkInterventionConsumed(ctx, iv.InterventionID, logID)
		if err != nil {
			s.logger.Debug("interventions: mark consumed", "id", iv.InterventionID, "err", err)
			continue
		}
		s.broadcast(sse.EventInterventionConsumed, updated)
	}
}

func (s *Service) broadcast(kind string, iv duckdb.Intervention) {
	if s.hub == nil {
		return
	}
	s.hub.Broadcast(sse.NewInterventionEvent(kind, s.clock(), iv))
}

func validDeliveryMode(m string) bool {
	switch m {
	case duckdb.InterventionModeInterrupt,
		duckdb.InterventionModeContext,
		duckdb.InterventionModeBoth:
		return true
	}
	return false
}

func validScope(sc string) bool {
	switch sc {
	case duckdb.InterventionScopeTurn, duckdb.InterventionScopeSession:
		return true
	}
	return false
}

func validUrgency(u string) bool {
	switch u {
	case duckdb.InterventionUrgencyHigh,
		duckdb.InterventionUrgencyNormal,
		duckdb.InterventionUrgencyLow:
		return true
	}
	return false
}

// isConsumptionHook reports whether a downstream hook event is a plausible
// proxy that Claude Code processed a prior block/context injection.
func isConsumptionHook(hookEvent string) bool {
	switch hookEvent {
	case "PreToolUse", "PostToolUse", "PostToolUseFailure", "Stop", "UserPromptSubmit":
		return true
	}
	return false
}

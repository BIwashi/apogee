package duckdb

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"
	"time"
)

// Intervention is a row in the interventions table. Nullable columns are
// wrapped in sql.NullXxx types; the API and SSE layers unwrap them for
// external callers.
type Intervention struct {
	InterventionID  string         `json:"intervention_id"`
	SessionID       string         `json:"session_id"`
	TurnID          sql.NullString `json:"-"`
	OperatorID      sql.NullString `json:"-"`
	CreatedAt       time.Time      `json:"created_at"`
	ClaimedAt       sql.NullTime   `json:"-"`
	DeliveredAt     sql.NullTime   `json:"-"`
	ConsumedAt      sql.NullTime   `json:"-"`
	ExpiredAt       sql.NullTime   `json:"-"`
	CancelledAt     sql.NullTime   `json:"-"`
	AutoExpireAt    time.Time      `json:"auto_expire_at"`
	Message         string         `json:"message"`
	DeliveryMode    string         `json:"delivery_mode"`
	Scope           string         `json:"scope"`
	Urgency         string         `json:"urgency"`
	Status          string         `json:"status"`
	DeliveredVia    sql.NullString `json:"-"`
	ConsumedEventID sql.NullInt64  `json:"-"`
	Notes           sql.NullString `json:"-"`
}

// InterventionRequest is the parameter bag accepted by InsertIntervention.
// Zero values are valid for optional fields (TurnID, OperatorID, Notes).
type InterventionRequest struct {
	SessionID    string
	TurnID       string
	OperatorID   string
	Message      string
	DeliveryMode string
	Scope        string
	Urgency      string
	Notes        string
	TTL          time.Duration
}

// Intervention lifecycle constants.
const (
	InterventionStatusQueued    = "queued"
	InterventionStatusClaimed   = "claimed"
	InterventionStatusDelivered = "delivered"
	InterventionStatusConsumed  = "consumed"
	InterventionStatusExpired   = "expired"
	InterventionStatusCancelled = "cancelled"
)

// Delivery mode constants.
const (
	InterventionModeInterrupt = "interrupt"
	InterventionModeContext   = "context"
	InterventionModeBoth      = "both"
)

// Scope constants.
const (
	InterventionScopeTurn    = "this_turn"
	InterventionScopeSession = "this_session"
)

// Urgency constants.
const (
	InterventionUrgencyHigh   = "high"
	InterventionUrgencyNormal = "normal"
	InterventionUrgencyLow    = "low"
)

// Hook event constants that this module uses for delivery-mode filtering.
const (
	HookEventPreToolUse       = "PreToolUse"
	HookEventUserPromptSubmit = "UserPromptSubmit"
)

// Default TTL applied when InterventionRequest.TTL is zero.
const defaultInterventionTTL = 10 * time.Minute

// Errors surfaced by the intervention layer.
var (
	ErrInterventionNotFound    = errors.New("intervention not found")
	ErrInterventionImmutable   = errors.New("intervention is in a terminal state")
	ErrInterventionInvalidMode = errors.New("invalid intervention delivery mode")
)

const selectIntervention = `
SELECT
  intervention_id, session_id, turn_id, operator_id, created_at,
  claimed_at, delivered_at, consumed_at, expired_at, cancelled_at,
  auto_expire_at, message, delivery_mode, scope, urgency, status,
  delivered_via, consumed_event_id, notes
FROM interventions
`

// interventionClaimMu is a package-level mutex defending the atomic claim
// flow against concurrent goroutines calling ClaimNextIntervention on the
// same Store. DuckDB's single-connection pool already serialises writes,
// but we hold a Go-side lock too so the SELECT + UPDATE pair behave as one
// logical transaction even when called from many goroutines.
var interventionClaimMu sync.Mutex

// InsertIntervention persists a fresh queued intervention. The row id and
// timestamps are generated here so callers do not have to synthesise them.
func (s *Store) InsertIntervention(ctx context.Context, req InterventionRequest) (Intervention, error) {
	if req.SessionID == "" {
		return Intervention{}, errors.New("intervention: session_id is required")
	}
	if req.Message == "" {
		return Intervention{}, errors.New("intervention: message is required")
	}
	if req.DeliveryMode == "" {
		req.DeliveryMode = InterventionModeInterrupt
	}
	if req.Scope == "" {
		req.Scope = InterventionScopeTurn
	}
	if req.Urgency == "" {
		req.Urgency = InterventionUrgencyNormal
	}
	ttl := req.TTL
	if ttl <= 0 {
		ttl = defaultInterventionTTL
	}
	now := time.Now().UTC().Truncate(time.Millisecond)
	id := newInterventionID()
	const q = `
INSERT INTO interventions (
  intervention_id, session_id, turn_id, operator_id, created_at,
  auto_expire_at, message, delivery_mode, scope, urgency, status, notes
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
`
	_, err := s.db.ExecContext(ctx, q,
		id,
		req.SessionID,
		nullString(req.TurnID),
		nullString(req.OperatorID),
		now,
		now.Add(ttl),
		req.Message,
		req.DeliveryMode,
		req.Scope,
		req.Urgency,
		InterventionStatusQueued,
		nullString(req.Notes),
	)
	if err != nil {
		return Intervention{}, fmt.Errorf("insert intervention: %w", err)
	}
	out, ok, err := s.GetIntervention(ctx, id)
	if err != nil {
		return Intervention{}, err
	}
	if !ok {
		return Intervention{}, ErrInterventionNotFound
	}
	return out, nil
}

// GetIntervention fetches one row by id. The bool return is false when the
// row does not exist (and err is nil).
func (s *Store) GetIntervention(ctx context.Context, id string) (Intervention, bool, error) {
	row := s.db.QueryRowContext(ctx, selectIntervention+` WHERE intervention_id = ?`, id)
	iv, err := scanIntervention(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Intervention{}, false, nil
		}
		return Intervention{}, false, fmt.Errorf("get intervention: %w", err)
	}
	return iv, true, nil
}

// ListInterventionsBySession returns every intervention for a session,
// newest-first.
func (s *Store) ListInterventionsBySession(ctx context.Context, sessionID string, limit int) ([]Intervention, error) {
	if limit <= 0 {
		limit = 200
	}
	const q = selectIntervention + ` WHERE session_id = ? ORDER BY created_at DESC LIMIT ?`
	return s.queryInterventions(ctx, q, sessionID, limit)
}

// ListPendingInterventionsBySession returns every non-terminal intervention
// for a session (queued or claimed or delivered), oldest-first. The hook
// polling loop and the operator queue both read this shape.
func (s *Store) ListPendingInterventionsBySession(ctx context.Context, sessionID string) ([]Intervention, error) {
	const q = selectIntervention + `
WHERE session_id = ? AND status IN ('queued','claimed','delivered')
ORDER BY created_at ASC`
	return s.queryInterventions(ctx, q, sessionID)
}

// ListPendingInterventionsByTurn is the turn-scoped variant of the hot-path
// pending list. Used by the attention engine rescore loop.
func (s *Store) ListPendingInterventionsByTurn(ctx context.Context, turnID string) ([]Intervention, error) {
	const q = selectIntervention + `
WHERE turn_id = ? AND status IN ('queued','claimed','delivered')
ORDER BY created_at ASC`
	return s.queryInterventions(ctx, q, turnID)
}

// ListInterventionsToAutoExpire returns every non-terminal row whose
// auto_expire_at cutoff has been crossed. The service sweeper calls this
// every tick and flips each row to status=expired.
func (s *Store) ListInterventionsToAutoExpire(ctx context.Context, now time.Time, limit int) ([]Intervention, error) {
	if limit <= 0 {
		limit = 100
	}
	const q = selectIntervention + `
WHERE status IN ('queued','claimed','delivered') AND auto_expire_at <= ?
ORDER BY auto_expire_at ASC
LIMIT ?`
	return s.queryInterventions(ctx, q, now, limit)
}

// ClaimNextIntervention is the atomic "give me one" primitive called by the
// Python hook. Given a session/turn/hook_event tuple, find the highest
// priority queued intervention whose delivery mode matches the hook and
// flip it to status=claimed. Returns (_, false) when nothing was claimed.
//
// Selection rules (matching the PR brief):
//  1. status = 'queued'
//  2. session_id matches
//  3. scope matches: intervention.turn_id IS NULL OR intervention.turn_id = turnID
//  4. delivery_mode matches the hook event:
//     PreToolUse       → {interrupt, both}
//     UserPromptSubmit → {context, both}
//  5. auto_expire_at > now (don't deliver a stale row)
//  6. priority: urgency=high → normal → low, then FIFO by created_at
func (s *Store) ClaimNextIntervention(ctx context.Context, sessionID, turnID, hookEvent string) (Intervention, bool, error) {
	modes, err := modesForHookEvent(hookEvent)
	if err != nil {
		return Intervention{}, false, err
	}

	interventionClaimMu.Lock()
	defer interventionClaimMu.Unlock()

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Intervention{}, false, fmt.Errorf("claim intervention: begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	now := time.Now().UTC().Truncate(time.Millisecond)

	// Build the selection query. We cannot use a parameterised IN clause
	// with a variable-length slice in go-duckdb cleanly, so the mode
	// predicate is composed inline from a trusted enum.
	modePreds := make([]string, 0, len(modes))
	for _, m := range modes {
		modePreds = append(modePreds, "'"+m+"'")
	}
	modeIn := ""
	for i, m := range modePreds {
		if i > 0 {
			modeIn += ","
		}
		modeIn += m
	}

	selectSQL := `
SELECT
  intervention_id, session_id, turn_id, operator_id, created_at,
  claimed_at, delivered_at, consumed_at, expired_at, cancelled_at,
  auto_expire_at, message, delivery_mode, scope, urgency, status,
  delivered_via, consumed_event_id, notes
FROM interventions
WHERE status = 'queued'
  AND session_id = ?
  AND (turn_id IS NULL OR turn_id = ?)
  AND delivery_mode IN (` + modeIn + `)
  AND auto_expire_at > ?
ORDER BY
  CASE urgency WHEN 'high' THEN 0 WHEN 'normal' THEN 1 ELSE 2 END ASC,
  created_at ASC
LIMIT 1
`
	row := tx.QueryRowContext(ctx, selectSQL, sessionID, turnID, now)
	candidate, err := scanIntervention(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Intervention{}, false, nil
		}
		return Intervention{}, false, fmt.Errorf("claim intervention: select: %w", err)
	}

	// Atomically flip queued → claimed. The WHERE status='queued' check is
	// the correctness guarantee if another goroutine also reached this
	// point via a different connection.
	res, err := tx.ExecContext(ctx,
		`UPDATE interventions SET status = 'claimed', claimed_at = ? WHERE intervention_id = ? AND status = 'queued'`,
		now, candidate.InterventionID,
	)
	if err != nil {
		return Intervention{}, false, fmt.Errorf("claim intervention: update: %w", err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return Intervention{}, false, fmt.Errorf("claim intervention: rows affected: %w", err)
	}
	if affected == 0 {
		return Intervention{}, false, nil
	}
	if err := tx.Commit(); err != nil {
		return Intervention{}, false, fmt.Errorf("claim intervention: commit: %w", err)
	}

	// Re-read outside the transaction so the returned struct reflects the
	// claimed_at timestamp we just wrote.
	out, ok, err := s.GetIntervention(ctx, candidate.InterventionID)
	if err != nil {
		return Intervention{}, false, err
	}
	if !ok {
		return Intervention{}, false, ErrInterventionNotFound
	}
	return out, true, nil
}

// MarkInterventionDelivered transitions a claimed row to delivered and
// stamps the hook event that delivered it.
func (s *Store) MarkInterventionDelivered(ctx context.Context, id, hookEvent string) (Intervention, error) {
	now := time.Now().UTC().Truncate(time.Millisecond)
	const q = `
UPDATE interventions
SET status = 'delivered', delivered_at = ?, delivered_via = ?
WHERE intervention_id = ? AND status = 'claimed'
`
	res, err := s.db.ExecContext(ctx, q, now, nullString(hookEvent), id)
	if err != nil {
		return Intervention{}, fmt.Errorf("mark intervention delivered: %w", err)
	}
	affected, _ := res.RowsAffected()
	if affected == 0 {
		// Verify whether the row exists at all.
		if _, ok, getErr := s.GetIntervention(ctx, id); getErr == nil && !ok {
			return Intervention{}, ErrInterventionNotFound
		}
		return Intervention{}, ErrInterventionImmutable
	}
	out, ok, err := s.GetIntervention(ctx, id)
	if err != nil {
		return Intervention{}, err
	}
	if !ok {
		return Intervention{}, ErrInterventionNotFound
	}
	return out, nil
}

// MarkInterventionConsumed transitions a delivered row to consumed and
// records the id of the log row that proved consumption.
func (s *Store) MarkInterventionConsumed(ctx context.Context, id string, eventID int64) (Intervention, error) {
	now := time.Now().UTC().Truncate(time.Millisecond)
	const q = `
UPDATE interventions
SET status = 'consumed', consumed_at = ?, consumed_event_id = ?
WHERE intervention_id = ? AND status = 'delivered'
`
	var evArg any
	if eventID > 0 {
		evArg = eventID
	}
	res, err := s.db.ExecContext(ctx, q, now, evArg, id)
	if err != nil {
		return Intervention{}, fmt.Errorf("mark intervention consumed: %w", err)
	}
	affected, _ := res.RowsAffected()
	if affected == 0 {
		if _, ok, getErr := s.GetIntervention(ctx, id); getErr == nil && !ok {
			return Intervention{}, ErrInterventionNotFound
		}
		return Intervention{}, ErrInterventionImmutable
	}
	out, ok, err := s.GetIntervention(ctx, id)
	if err != nil {
		return Intervention{}, err
	}
	if !ok {
		return Intervention{}, ErrInterventionNotFound
	}
	return out, nil
}

// CancelIntervention transitions a queued/claimed row to cancelled. Already
// terminal rows return ErrInterventionImmutable.
func (s *Store) CancelIntervention(ctx context.Context, id string) (Intervention, error) {
	now := time.Now().UTC().Truncate(time.Millisecond)
	const q = `
UPDATE interventions
SET status = 'cancelled', cancelled_at = ?
WHERE intervention_id = ? AND status IN ('queued','claimed')
`
	res, err := s.db.ExecContext(ctx, q, now, id)
	if err != nil {
		return Intervention{}, fmt.Errorf("cancel intervention: %w", err)
	}
	affected, _ := res.RowsAffected()
	if affected == 0 {
		if _, ok, getErr := s.GetIntervention(ctx, id); getErr == nil && !ok {
			return Intervention{}, ErrInterventionNotFound
		}
		return Intervention{}, ErrInterventionImmutable
	}
	out, ok, err := s.GetIntervention(ctx, id)
	if err != nil {
		return Intervention{}, err
	}
	if !ok {
		return Intervention{}, ErrInterventionNotFound
	}
	return out, nil
}

// ExpireIntervention transitions any non-terminal row to expired. Used by
// both the TTL sweeper and the turn/session-end hooks.
func (s *Store) ExpireIntervention(ctx context.Context, id string) (Intervention, error) {
	now := time.Now().UTC().Truncate(time.Millisecond)
	const q = `
UPDATE interventions
SET status = 'expired', expired_at = ?
WHERE intervention_id = ? AND status IN ('queued','claimed','delivered')
`
	res, err := s.db.ExecContext(ctx, q, now, id)
	if err != nil {
		return Intervention{}, fmt.Errorf("expire intervention: %w", err)
	}
	affected, _ := res.RowsAffected()
	if affected == 0 {
		if _, ok, getErr := s.GetIntervention(ctx, id); getErr == nil && !ok {
			return Intervention{}, ErrInterventionNotFound
		}
		return Intervention{}, ErrInterventionImmutable
	}
	out, ok, err := s.GetIntervention(ctx, id)
	if err != nil {
		return Intervention{}, err
	}
	if !ok {
		return Intervention{}, ErrInterventionNotFound
	}
	return out, nil
}

// modesForHookEvent returns the delivery_mode values the given hook event
// is allowed to pick up. Unknown hook events return an empty slice so the
// caller short-circuits to "nothing matches".
func modesForHookEvent(hookEvent string) ([]string, error) {
	switch hookEvent {
	case HookEventPreToolUse:
		return []string{InterventionModeInterrupt, InterventionModeBoth}, nil
	case HookEventUserPromptSubmit:
		return []string{InterventionModeContext, InterventionModeBoth}, nil
	default:
		return nil, ErrInterventionInvalidMode
	}
}

func (s *Store) queryInterventions(ctx context.Context, query string, args ...any) ([]Intervention, error) {
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query interventions: %w", err)
	}
	defer rows.Close()
	out := []Intervention{}
	for rows.Next() {
		iv, err := scanIntervention(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, iv)
	}
	return out, rows.Err()
}

func scanIntervention(r rowScanner) (Intervention, error) {
	var iv Intervention
	if err := r.Scan(
		&iv.InterventionID,
		&iv.SessionID,
		&iv.TurnID,
		&iv.OperatorID,
		&iv.CreatedAt,
		&iv.ClaimedAt,
		&iv.DeliveredAt,
		&iv.ConsumedAt,
		&iv.ExpiredAt,
		&iv.CancelledAt,
		&iv.AutoExpireAt,
		&iv.Message,
		&iv.DeliveryMode,
		&iv.Scope,
		&iv.Urgency,
		&iv.Status,
		&iv.DeliveredVia,
		&iv.ConsumedEventID,
		&iv.Notes,
	); err != nil {
		return Intervention{}, err
	}
	return iv, nil
}

// newInterventionID returns a fresh intervention id of the form
// "intv-<12 hex>". Kept inline rather than under internal/otel so the
// duckdb package stays self-contained.
func newInterventionID() string {
	var b [6]byte
	if _, err := rand.Read(b[:]); err != nil {
		// crypto/rand failure is catastrophic for any collision-free id
		// scheme; fall back to a timestamp-derived sequence rather than
		// returning an empty string.
		now := time.Now().UnixNano()
		for i := 0; i < 6; i++ {
			b[i] = byte(now >> (8 * i))
		}
	}
	return "intv-" + hex.EncodeToString(b[:])
}

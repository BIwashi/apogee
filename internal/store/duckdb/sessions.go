package duckdb

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Session is a row in the sessions table.
//
// The Attention*, Current*, and LiveStatus* fields are the fleet-view
// projection written back by the attention engine and the summarizer's
// live-status worker. They let the Live dashboard render one row per
// terminal without scanning turns and spans on every poll. All are
// optional: pre-engine rows surface with empty strings and the UI
// degrades gracefully.
type Session struct {
	SessionID  string     `json:"session_id"`
	SourceApp  string     `json:"source_app"`
	StartedAt  time.Time  `json:"started_at"`
	EndedAt    *time.Time `json:"ended_at,omitempty"`
	LastSeenAt time.Time  `json:"last_seen_at"`
	TurnCount  int        `json:"turn_count"`
	Model      string     `json:"model,omitempty"`
	MachineID  string     `json:"machine_id,omitempty"`
	// AttentionState mirrors the currently-representative turn's
	// attention bucket (intervene_now|watch|watchlist|healthy).
	AttentionState  string   `json:"attention_state,omitempty"`
	AttentionReason string   `json:"attention_reason,omitempty"`
	AttentionScore  *float64 `json:"attention_score,omitempty"`
	CurrentTurnID   string   `json:"current_turn_id,omitempty"`
	CurrentPhase    string   `json:"current_phase,omitempty"`
	// LiveState is "live" when the session has a currently-running turn,
	// "idle" when it has recent activity but no running turn, and empty
	// for pre-engine rows. The fleet view uses this to decide whether
	// to grey out the card.
	LiveState       string     `json:"live_state,omitempty"`
	LiveStatusText  string     `json:"live_status_text,omitempty"`
	LiveStatusAt    *time.Time `json:"live_status_at,omitempty"`
	LiveStatusModel string     `json:"live_status_model,omitempty"`
}

// selectSessionColumns is the column list used by every SELECT against
// the sessions table so adding a column is a one-line change in this
// file. Matches the order consumed by scanSession.
const selectSessionColumns = `SELECT
  session_id, source_app, started_at, ended_at, last_seen_at, turn_count,
  model, machine_id,
  attention_state, attention_reason, attention_score,
  current_turn_id, current_phase, live_state,
  live_status_text, live_status_at, live_status_model
FROM sessions`

// scanSession reads one session row off a *sql.Row or *sql.Rows. The
// argument is typed as rowScanner so both work.
func scanSession(r rowScanner) (Session, error) {
	var (
		out             Session
		endedAt         sql.NullTime
		model           sql.NullString
		machineID       sql.NullString
		attentionState  sql.NullString
		attentionReason sql.NullString
		attentionScore  sql.NullFloat64
		currentTurnID   sql.NullString
		currentPhase    sql.NullString
		liveState       sql.NullString
		liveStatusText  sql.NullString
		liveStatusAt    sql.NullTime
		liveStatusModel sql.NullString
	)
	if err := r.Scan(
		&out.SessionID,
		&out.SourceApp,
		&out.StartedAt,
		&endedAt,
		&out.LastSeenAt,
		&out.TurnCount,
		&model,
		&machineID,
		&attentionState,
		&attentionReason,
		&attentionScore,
		&currentTurnID,
		&currentPhase,
		&liveState,
		&liveStatusText,
		&liveStatusAt,
		&liveStatusModel,
	); err != nil {
		return Session{}, err
	}
	if endedAt.Valid {
		t := endedAt.Time
		out.EndedAt = &t
	}
	if model.Valid {
		out.Model = model.String
	}
	if machineID.Valid {
		out.MachineID = machineID.String
	}
	if attentionState.Valid {
		out.AttentionState = attentionState.String
	}
	if attentionReason.Valid {
		out.AttentionReason = attentionReason.String
	}
	if attentionScore.Valid {
		v := attentionScore.Float64
		out.AttentionScore = &v
	}
	if currentTurnID.Valid {
		out.CurrentTurnID = currentTurnID.String
	}
	if currentPhase.Valid {
		out.CurrentPhase = currentPhase.String
	}
	if liveState.Valid {
		out.LiveState = liveState.String
	}
	if liveStatusText.Valid {
		out.LiveStatusText = liveStatusText.String
	}
	if liveStatusAt.Valid {
		t := liveStatusAt.Time
		out.LiveStatusAt = &t
	}
	if liveStatusModel.Valid {
		out.LiveStatusModel = liveStatusModel.String
	}
	return out, nil
}

// UpsertSession inserts or refreshes a sessions row. started_at is preserved
// across upserts; last_seen_at and source_app/model are kept up to date.
func (s *Store) UpsertSession(ctx context.Context, sess Session) error {
	const q = `
INSERT INTO sessions (session_id, source_app, started_at, last_seen_at, turn_count, model, machine_id)
VALUES (?, ?, ?, ?, ?, ?, ?)
ON CONFLICT (session_id) DO UPDATE SET
  source_app   = excluded.source_app,
  last_seen_at = GREATEST(sessions.last_seen_at, excluded.last_seen_at),
  model        = COALESCE(excluded.model, sessions.model),
  machine_id   = COALESCE(excluded.machine_id, sessions.machine_id)
`
	_, err := s.db.ExecContext(ctx, q,
		sess.SessionID,
		sess.SourceApp,
		sess.StartedAt,
		sess.LastSeenAt,
		sess.TurnCount,
		nullString(sess.Model),
		nullString(sess.MachineID),
	)
	if err != nil {
		return fmt.Errorf("upsert session: %w", err)
	}
	return nil
}

// MarkSessionEnded sets ended_at on a session row.
func (s *Store) MarkSessionEnded(ctx context.Context, sessionID string, endedAt time.Time) error {
	_, err := s.db.ExecContext(ctx, `UPDATE sessions SET ended_at = ?, last_seen_at = GREATEST(last_seen_at, ?) WHERE session_id = ?`, endedAt, endedAt, sessionID)
	if err != nil {
		return fmt.Errorf("mark session ended: %w", err)
	}
	return nil
}

// IncrementSessionTurnCount bumps the rolling turn count for a session.
func (s *Store) IncrementSessionTurnCount(ctx context.Context, sessionID string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE sessions SET turn_count = turn_count + 1 WHERE session_id = ?`, sessionID)
	if err != nil {
		return fmt.Errorf("increment turn count: %w", err)
	}
	return nil
}

// GetSession fetches one session row by id.
func (s *Store) GetSession(ctx context.Context, sessionID string) (*Session, error) {
	row := s.db.QueryRowContext(ctx, selectSessionColumns+` WHERE session_id = ?`, sessionID)
	sess, err := scanSession(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("get session: %w", err)
	}
	return &sess, nil
}

// ListRecentSessions returns up to limit sessions ordered by last_seen_at DESC.
//
// Sessions whose source_app starts with "." are filtered out because
// those are the synthetic rows created by the summarizer feedback
// loop (the `claude` subprocess's cwd basename ends up as the
// source_app, and the daemon runs from ~/.apogee). Real source_app
// values can never legitimately start with a dot — the apogee hook's
// derivation rules (env var > git toplevel > cwd basename) would
// never pick a hidden directory — so this filter is a safe global
// cleanup that also hides any legacy rows left over from before the
// APOGEE_HOOK_SKIP guard was added.
func (s *Store) ListRecentSessions(ctx context.Context, limit int) ([]Session, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := s.db.QueryContext(ctx, selectSessionColumns+` WHERE source_app NOT LIKE '.%' ORDER BY last_seen_at DESC LIMIT ?`, limit)
	if err != nil {
		return nil, fmt.Errorf("list sessions: %w", err)
	}
	defer rows.Close()
	var out []Session
	for rows.Next() {
		sess, err := scanSession(rows)
		if err != nil {
			return nil, fmt.Errorf("scan session: %w", err)
		}
		out = append(out, sess)
	}
	return out, rows.Err()
}

// SessionFilter captures the optional filters accepted by the fleet-view
// endpoints. Zero fields are ignored. Since clamps the last_seen_at window,
// which is how the Live page's global "display period" selector is applied.
type SessionFilter struct {
	SourceApp string
	Since     *time.Time
	Until     *time.Time
}

// ListActiveSessions returns sessions whose last_seen_at falls within the
// filter window, sorted by attention priority then recency. It is the
// data source for the /v1/sessions/active fleet endpoint. Sessions are
// returned whether or not they have a running turn — LiveState ("live" |
// "idle") distinguishes the two so the UI can grey out idle ones.
func (s *Store) ListActiveSessions(ctx context.Context, f SessionFilter, limit int) ([]Session, error) {
	if limit <= 0 {
		limit = 200
	}
	if limit > 500 {
		limit = 500
	}
	clauses := []string{`source_app NOT LIKE '.%'`}
	args := []any{}
	if f.SourceApp != "" {
		clauses = append(clauses, `source_app = ?`)
		args = append(args, f.SourceApp)
	}
	if f.Since != nil {
		clauses = append(clauses, `last_seen_at >= ?`)
		args = append(args, *f.Since)
	}
	if f.Until != nil {
		clauses = append(clauses, `last_seen_at <= ?`)
		args = append(args, *f.Until)
	}
	where := ` WHERE ` + strings.Join(clauses, ` AND `)
	order := `
ORDER BY
  CASE attention_state
    WHEN 'intervene_now' THEN 0
    WHEN 'watch'         THEN 1
    WHEN 'watchlist'     THEN 2
    WHEN 'healthy'       THEN 3
    ELSE 4
  END ASC,
  last_seen_at DESC
`
	q := selectSessionColumns + where + order + ` LIMIT ?`
	args = append(args, limit)
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("list active sessions: %w", err)
	}
	defer rows.Close()
	var out []Session
	for rows.Next() {
		sess, err := scanSession(rows)
		if err != nil {
			return nil, fmt.Errorf("scan active session: %w", err)
		}
		out = append(out, sess)
	}
	return out, rows.Err()
}

// SessionCard wraps a Session with the aggregate fields the Live fleet
// view needs per card: the currently-running tool name, the timestamp of
// the newest span, and pending HITL / intervention counts. The embedded
// Session flattens through to the JSON wire format so the frontend sees
// one flat object per card.
type SessionCard struct {
	Session
	CurrentTool              string     `json:"current_tool,omitempty"`
	LastSpanAt               *time.Time `json:"last_span_at,omitempty"`
	HITLPendingCount         int        `json:"hitl_pending_count"`
	InterventionPendingCount int        `json:"intervention_pending_count"`
}

// ListActiveSessionCards returns the fleet-view payload for the Live
// dashboard: the filtered session rows plus the aggregate fields that
// would otherwise require N round-trips from the frontend. Aggregates
// are fetched in three batch queries keyed by session id so the cost
// stays linear in the card count.
func (s *Store) ListActiveSessionCards(ctx context.Context, f SessionFilter, limit int) ([]SessionCard, error) {
	sessions, err := s.ListActiveSessions(ctx, f, limit)
	if err != nil {
		return nil, err
	}
	cards := make([]SessionCard, len(sessions))
	for i, sess := range sessions {
		cards[i] = SessionCard{Session: sess}
	}
	if len(cards) == 0 {
		return cards, nil
	}
	ids := make([]string, len(cards))
	placeholders := make([]string, len(cards))
	args := make([]any, len(cards))
	for i, c := range cards {
		ids[i] = c.SessionID
		placeholders[i] = "?"
		args[i] = c.SessionID
	}
	inClause := strings.Join(placeholders, ",")
	idx := make(map[string]int, len(cards))
	for i, id := range ids {
		idx[id] = i
	}

	// Current tool and last-span timestamp per session. The latest span
	// with a non-null tool_name wins, ranked by start_time desc.
	toolQ := `
SELECT session_id, tool_name, start_time
FROM (
  SELECT session_id, tool_name, start_time,
         ROW_NUMBER() OVER (PARTITION BY session_id ORDER BY start_time DESC) AS rn
  FROM spans
  WHERE session_id IN (` + inClause + `) AND tool_name IS NOT NULL AND tool_name <> ''
) t
WHERE rn = 1
`
	rows, err := s.db.QueryContext(ctx, toolQ, args...)
	if err != nil {
		return nil, fmt.Errorf("session cards tool query: %w", err)
	}
	for rows.Next() {
		var sid, tool string
		var ts time.Time
		if err := rows.Scan(&sid, &tool, &ts); err != nil {
			rows.Close()
			return nil, err
		}
		if i, ok := idx[sid]; ok {
			cards[i].CurrentTool = tool
			t := ts
			cards[i].LastSpanAt = &t
		}
	}
	rows.Close()

	// Pending HITL per session.
	hitlQ := `SELECT session_id, COUNT(*) FROM hitl_events WHERE session_id IN (` + inClause + `) AND status = 'pending' GROUP BY session_id`
	rows, err = s.db.QueryContext(ctx, hitlQ, args...)
	if err != nil {
		return nil, fmt.Errorf("session cards hitl query: %w", err)
	}
	for rows.Next() {
		var sid string
		var n int
		if err := rows.Scan(&sid, &n); err != nil {
			rows.Close()
			return nil, err
		}
		if i, ok := idx[sid]; ok {
			cards[i].HITLPendingCount = n
		}
	}
	rows.Close()

	// Pending interventions per session. Any intervention that has not
	// yet been consumed / cancelled / expired counts as pending so the
	// badge reflects "there is still work queued for this terminal".
	ivQ := `SELECT session_id, COUNT(*) FROM interventions WHERE session_id IN (` + inClause + `) AND status IN ('queued','claimed','delivered') GROUP BY session_id`
	rows, err = s.db.QueryContext(ctx, ivQ, args...)
	if err != nil {
		return nil, fmt.Errorf("session cards intervention query: %w", err)
	}
	for rows.Next() {
		var sid string
		var n int
		if err := rows.Scan(&sid, &n); err != nil {
			rows.Close()
			return nil, err
		}
		if i, ok := idx[sid]; ok {
			cards[i].InterventionPendingCount = n
		}
	}
	rows.Close()

	return cards, nil
}

// CountSessionAttention returns the per-bucket count of sessions scoped by
// the filter window. It mirrors CountAttentionFiltered on the turns table
// so the Live page's CountPills can switch to session scope.
func (s *Store) CountSessionAttention(ctx context.Context, f SessionFilter) (AttentionCounts, error) {
	clauses := []string{`source_app NOT LIKE '.%'`}
	args := []any{}
	if f.SourceApp != "" {
		clauses = append(clauses, `source_app = ?`)
		args = append(args, f.SourceApp)
	}
	if f.Since != nil {
		clauses = append(clauses, `last_seen_at >= ?`)
		args = append(args, *f.Since)
	}
	if f.Until != nil {
		clauses = append(clauses, `last_seen_at <= ?`)
		args = append(args, *f.Until)
	}
	q := `SELECT COALESCE(attention_state, 'healthy') AS state, COUNT(*) FROM sessions WHERE ` + strings.Join(clauses, ` AND `) + ` GROUP BY 1`
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return AttentionCounts{}, fmt.Errorf("count session attention: %w", err)
	}
	defer rows.Close()
	var out AttentionCounts
	for rows.Next() {
		var state string
		var c int
		if err := rows.Scan(&state, &c); err != nil {
			return AttentionCounts{}, err
		}
		switch state {
		case "intervene_now":
			out.InterveneNow = c
		case "watch":
			out.Watch = c
		case "watchlist":
			out.Watchlist = c
		default:
			out.Healthy += c
		}
		out.Total += c
	}
	return out, rows.Err()
}

// UpdateSessionAttention writes the fleet-view projection of the
// currently-representative turn onto the session row. Called by the
// reconstructor after rescoring a turn so the Live endpoint can render
// one row per terminal without having to pick the "right" turn on every
// poll. liveState is "live" when the representative turn is still
// running, "idle" when it has closed.
func (s *Store) UpdateSessionAttention(ctx context.Context, sessionID string, state, reason string, score float64, currentTurnID, phase, liveState string) error {
	const q = `
UPDATE sessions SET
  attention_state  = ?,
  attention_reason = ?,
  attention_score  = ?,
  current_turn_id  = ?,
  current_phase    = ?,
  live_state       = ?
WHERE session_id = ?
`
	_, err := s.db.ExecContext(ctx, q,
		nullString(state),
		nullString(reason),
		score,
		nullString(currentTurnID),
		nullString(phase),
		nullString(liveState),
		sessionID,
	)
	if err != nil {
		return fmt.Errorf("update session attention: %w", err)
	}
	return nil
}

// UpdateSessionLiveStatus writes the summarizer's live-status blurb onto
// the session row. PR B's worker calls this once the LLM returns a
// "currently <verb>-ing <noun>" string. text is allowed to be empty,
// which clears the field (e.g., when a session transitions to idle).
func (s *Store) UpdateSessionLiveStatus(ctx context.Context, sessionID, text, model string, at time.Time) error {
	const q = `
UPDATE sessions SET
  live_status_text  = ?,
  live_status_at    = ?,
  live_status_model = ?
WHERE session_id = ?
`
	_, err := s.db.ExecContext(ctx, q,
		nullString(text),
		at,
		nullString(model),
		sessionID,
	)
	if err != nil {
		return fmt.Errorf("update session live status: %w", err)
	}
	return nil
}

// RepresentativeTurn returns the turn that should drive the session's
// fleet-view projection: the oldest still-running turn (highest-priority
// attention first) if any, else the most recently closed turn. It
// returns (nil, nil) when the session has no turns at all.
func (s *Store) RepresentativeTurn(ctx context.Context, sessionID string) (*Turn, error) {
	running := selectTurn + ` WHERE session_id = ? AND status = 'running' ` + attentionOrder + ` LIMIT 1`
	t, err := scanTurn(s.db.QueryRowContext(ctx, running, sessionID))
	if err == nil {
		return t, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("representative running turn: %w", err)
	}
	latest := selectTurn + ` WHERE session_id = ? ORDER BY started_at DESC LIMIT 1`
	t, err = scanTurn(s.db.QueryRowContext(ctx, latest, sessionID))
	if err == nil {
		return t, nil
	}
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return nil, fmt.Errorf("representative latest turn: %w", err)
}

// SessionSearchHit is one row in the /v1/sessions/search response. It is
// enriched with the latest turn's headline / prompt snippet so the command
// palette can render a single-line label per session.
type SessionSearchHit struct {
	SessionID           string    `json:"session_id"`
	SourceApp           string    `json:"source_app"`
	LastSeenAt          time.Time `json:"last_seen_at"`
	TurnCount           int       `json:"turn_count"`
	LatestHeadline      string    `json:"latest_headline,omitempty"`
	LatestPromptSnippet string    `json:"latest_prompt_snippet,omitempty"`
	AttentionState      string    `json:"attention_state,omitempty"`
}

// SearchSessions returns up to limit sessions matching q. Empty q returns
// the most-recent sessions unfiltered.
func (s *Store) SearchSessions(ctx context.Context, q string, limit int) ([]SessionSearchHit, error) {
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}
	// The inner latest_turn CTE picks the newest turn per session so the
	// headline + prompt + attention state come from the most-recent row.
	query := `
WITH latest_turn AS (
  SELECT t.session_id,
         t.headline,
         t.prompt_text,
         t.attention_state,
         t.started_at,
         ROW_NUMBER() OVER (PARTITION BY t.session_id ORDER BY t.started_at DESC) AS rn
  FROM turns t
)
SELECT s.session_id,
       s.source_app,
       s.last_seen_at,
       s.turn_count,
       COALESCE(lt.headline, ''),
       COALESCE(lt.prompt_text, ''),
       COALESCE(lt.attention_state, '')
FROM sessions s
LEFT JOIN latest_turn lt ON lt.session_id = s.session_id AND lt.rn = 1
WHERE s.source_app NOT LIKE '.%'
`
	args := []any{}
	if q != "" {
		query += `
  AND (
        s.session_id LIKE ?
     OR s.source_app LIKE ?
     OR COALESCE(lt.prompt_text, '') LIKE ?
     OR COALESCE(lt.headline, '') LIKE ?
  )
`
		like := "%" + q + "%"
		prefix := q + "%"
		args = append(args, prefix, like, like, like)
	}
	query += ` ORDER BY s.last_seen_at DESC LIMIT ?`
	args = append(args, limit)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("search sessions: %w", err)
	}
	defer rows.Close()
	out := []SessionSearchHit{}
	for rows.Next() {
		var h SessionSearchHit
		var headline, prompt, attn string
		if err := rows.Scan(&h.SessionID, &h.SourceApp, &h.LastSeenAt, &h.TurnCount, &headline, &prompt, &attn); err != nil {
			return nil, fmt.Errorf("scan search hit: %w", err)
		}
		h.LatestHeadline = headline
		if prompt != "" {
			snippet := prompt
			if len(snippet) > 120 {
				snippet = snippet[:120]
			}
			h.LatestPromptSnippet = snippet
		}
		h.AttentionState = attn
		out = append(out, h)
	}
	return out, rows.Err()
}

// SessionSummary is the denormalized header surfaced by
// GET /v1/sessions/:id/summary.
type SessionSummary struct {
	SessionID      string     `json:"session_id"`
	SourceApp      string     `json:"source_app"`
	StartedAt      time.Time  `json:"started_at"`
	LastSeenAt     time.Time  `json:"last_seen_at"`
	EndedAt        *time.Time `json:"ended_at,omitempty"`
	Model          string     `json:"model,omitempty"`
	MachineID      string     `json:"machine_id,omitempty"`
	TurnCount      int        `json:"turn_count"`
	RunningCount   int        `json:"running_count"`
	CompletedCount int        `json:"completed_count"`
	ErroredCount   int        `json:"errored_count"`
	LatestHeadline string     `json:"latest_headline,omitempty"`
	LatestTurnID   string     `json:"latest_turn_id,omitempty"`
	AttentionState string     `json:"attention_state,omitempty"`
}

// attentionPriority maps the four engine states onto a compare-friendly
// integer. Lower is more urgent.
func attentionPriority(state string) int {
	switch state {
	case "intervene_now":
		return 0
	case "watch":
		return 1
	case "watchlist":
		return 2
	case "healthy":
		return 3
	}
	return 4
}

// GetSessionSummary rolls up the turns belonging to sessionID into a single
// header row. Returns (nil, nil) when the session does not exist.
func (s *Store) GetSessionSummary(ctx context.Context, sessionID string) (*SessionSummary, error) {
	sess, err := s.GetSession(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	if sess == nil {
		return nil, nil
	}
	out := &SessionSummary{
		SessionID:  sess.SessionID,
		SourceApp:  sess.SourceApp,
		StartedAt:  sess.StartedAt,
		LastSeenAt: sess.LastSeenAt,
		EndedAt:    sess.EndedAt,
		Model:      sess.Model,
		MachineID:  sess.MachineID,
		TurnCount:  sess.TurnCount,
	}

	// Status breakdown.
	rows, err := s.db.QueryContext(ctx, `SELECT COALESCE(status, '') AS st, COUNT(*) FROM turns WHERE session_id = ? GROUP BY 1`, sessionID)
	if err != nil {
		return nil, fmt.Errorf("session summary counts: %w", err)
	}
	for rows.Next() {
		var st string
		var c int
		if err := rows.Scan(&st, &c); err != nil {
			rows.Close()
			return nil, err
		}
		switch st {
		case "running":
			out.RunningCount = c
		case "completed":
			out.CompletedCount = c
		case "errored":
			out.ErroredCount = c
		}
	}
	rows.Close()

	// Highest-priority attention state across all turns.
	attnRows, err := s.db.QueryContext(ctx, `SELECT DISTINCT attention_state FROM turns WHERE session_id = ? AND attention_state IS NOT NULL`, sessionID)
	if err != nil {
		return nil, fmt.Errorf("session summary attention: %w", err)
	}
	bestPri := 99
	for attnRows.Next() {
		var st sql.NullString
		if err := attnRows.Scan(&st); err != nil {
			attnRows.Close()
			return nil, err
		}
		if !st.Valid || st.String == "" {
			continue
		}
		p := attentionPriority(st.String)
		if p < bestPri {
			bestPri = p
			out.AttentionState = st.String
		}
	}
	attnRows.Close()

	// Latest turn headline and id.
	var headline, latestTurnID sql.NullString
	err = s.db.QueryRowContext(ctx, `
SELECT turn_id, COALESCE(headline, '')
FROM turns
WHERE session_id = ?
ORDER BY started_at DESC
LIMIT 1
`, sessionID).Scan(&latestTurnID, &headline)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("session summary latest turn: %w", err)
	}
	if latestTurnID.Valid {
		out.LatestTurnID = latestTurnID.String
	}
	if headline.Valid {
		out.LatestHeadline = headline.String
	}
	return out, nil
}

// RecentSessionsWithActivity returns up to n session IDs ordered by
// last_seen_at DESC. Used by the metrics collector to bound its per-session
// label write cost.
func (s *Store) RecentSessionsWithActivity(ctx context.Context, n int) ([]Session, error) {
	if n <= 0 {
		n = 20
	}
	return s.ListRecentSessions(ctx, n)
}

func nullString(v string) any {
	if v == "" {
		return nil
	}
	return v
}

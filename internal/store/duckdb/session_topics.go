package duckdb

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// SessionTopic is one node in a session's topic tree. Topics form a
// directed forest: every topic carries an optional parent_topic_id
// pointer (NULL for the session's root topic) plus an opened_at /
// last_seen_at / closed_at lifecycle. Identity is reused across
// resumes — visiting a previously-opened topic just bumps last_seen_at
// rather than creating a new row.
//
// The summarizer's per-turn recap classifier is the only writer.
// Readers are the Mission UI (topic chips, banners, branch lanes) and
// the future tier-2 / tier-3 summarizers that may want to anchor
// rollups per topic instead of per session.
type SessionTopic struct {
	TopicID       string         `json:"topic_id"`
	SessionID     string         `json:"session_id"`
	ParentTopicID sql.NullString `json:"-"`
	Goal          string         `json:"goal"`
	OpenedAt      time.Time      `json:"opened_at"`
	LastSeenAt    time.Time      `json:"last_seen_at"`
	ClosedAt      sql.NullTime   `json:"-"`
}

// MarshalJSON projects SessionTopic into the on-the-wire shape.
// parent_topic_id and closed_at become null when the underlying
// SQL field is NULL so the typescript client can branch on a
// stable null/value boundary.
func (t SessionTopic) MarshalJSON() ([]byte, error) {
	type alias struct {
		TopicID       string     `json:"topic_id"`
		SessionID     string     `json:"session_id"`
		ParentTopicID *string    `json:"parent_topic_id"`
		Goal          string     `json:"goal"`
		OpenedAt      time.Time  `json:"opened_at"`
		LastSeenAt    time.Time  `json:"last_seen_at"`
		ClosedAt      *time.Time `json:"closed_at"`
	}
	out := alias{
		TopicID:    t.TopicID,
		SessionID:  t.SessionID,
		Goal:       t.Goal,
		OpenedAt:   t.OpenedAt,
		LastSeenAt: t.LastSeenAt,
	}
	if t.ParentTopicID.Valid && t.ParentTopicID.String != "" {
		s := t.ParentTopicID.String
		out.ParentTopicID = &s
	}
	if t.ClosedAt.Valid {
		v := t.ClosedAt.Time
		out.ClosedAt = &v
	}
	return json.Marshal(out)
}

// UpsertSessionTopic inserts or refreshes a topic row. Goal,
// parent_topic_id, opened_at are only written on first insert; subsequent
// upserts only bump last_seen_at and (optionally) closed_at — that
// keeps the topic's original goal stable even if the operator pivots.
func (s *Store) UpsertSessionTopic(ctx context.Context, t SessionTopic) error {
	const q = `
INSERT INTO session_topics (
  topic_id, session_id, parent_topic_id, goal, opened_at, last_seen_at, closed_at
) VALUES (?, ?, ?, ?, ?, ?, ?)
ON CONFLICT (topic_id) DO UPDATE SET
  last_seen_at = greatest(session_topics.last_seen_at, excluded.last_seen_at),
  closed_at    = COALESCE(excluded.closed_at, session_topics.closed_at)
`
	_, err := s.db.ExecContext(ctx, q,
		t.TopicID,
		t.SessionID,
		t.ParentTopicID,
		t.Goal,
		t.OpenedAt,
		t.LastSeenAt,
		t.ClosedAt,
	)
	if err != nil {
		return fmt.Errorf("upsert session topic: %w", err)
	}
	return nil
}

// GetSessionTopic returns the topic row for topicID, or (_, false, nil)
// when nothing has been written yet.
func (s *Store) GetSessionTopic(ctx context.Context, topicID string) (SessionTopic, bool, error) {
	const q = `
SELECT topic_id, session_id, parent_topic_id, goal, opened_at, last_seen_at, closed_at
FROM session_topics
WHERE topic_id = ?
`
	var out SessionTopic
	err := s.db.QueryRowContext(ctx, q, topicID).Scan(
		&out.TopicID,
		&out.SessionID,
		&out.ParentTopicID,
		&out.Goal,
		&out.OpenedAt,
		&out.LastSeenAt,
		&out.ClosedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return SessionTopic{}, false, nil
		}
		return SessionTopic{}, false, fmt.Errorf("get session topic: %w", err)
	}
	return out, true, nil
}

// ListSessionTopics returns every topic in the session — open or
// closed — ordered by opened_at ASC so callers can render the
// per-topic Mission Goal banners in chronological order. Use this
// for the "show me the whole topic forest" projection. The live
// classifier uses ListOpenTopicsForSession (below) for the tighter
// "what topics could this turn join?" question.
func (s *Store) ListSessionTopics(ctx context.Context, sessionID string) ([]SessionTopic, error) {
	const q = `
SELECT topic_id, session_id, parent_topic_id, goal, opened_at, last_seen_at, closed_at
FROM session_topics
WHERE session_id = ?
ORDER BY opened_at ASC
`
	rows, err := s.db.QueryContext(ctx, q, sessionID)
	if err != nil {
		return nil, fmt.Errorf("list session topics: %w", err)
	}
	defer rows.Close()
	var out []SessionTopic
	for rows.Next() {
		var t SessionTopic
		if err := rows.Scan(
			&t.TopicID,
			&t.SessionID,
			&t.ParentTopicID,
			&t.Goal,
			&t.OpenedAt,
			&t.LastSeenAt,
			&t.ClosedAt,
		); err != nil {
			return nil, fmt.Errorf("scan session topic: %w", err)
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// ListOpenTopicsForSession returns the most-recently-touched open
// topics for the session (closed_at IS NULL), newest first, capped at
// limit. Used by the recap classifier to materialise the "recent
// topic candidates" the LLM picks `target_topic_ref` against.
func (s *Store) ListOpenTopicsForSession(ctx context.Context, sessionID string, limit int) ([]SessionTopic, error) {
	if limit <= 0 {
		limit = 5
	}
	const q = `
SELECT topic_id, session_id, parent_topic_id, goal, opened_at, last_seen_at, closed_at
FROM session_topics
WHERE session_id = ? AND closed_at IS NULL
ORDER BY last_seen_at DESC
LIMIT ?
`
	rows, err := s.db.QueryContext(ctx, q, sessionID, limit)
	if err != nil {
		return nil, fmt.Errorf("list open topics: %w", err)
	}
	defer rows.Close()
	out := make([]SessionTopic, 0, limit)
	for rows.Next() {
		var t SessionTopic
		if err := rows.Scan(
			&t.TopicID,
			&t.SessionID,
			&t.ParentTopicID,
			&t.Goal,
			&t.OpenedAt,
			&t.LastSeenAt,
			&t.ClosedAt,
		); err != nil {
			return nil, fmt.Errorf("scan session topic: %w", err)
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// TopicTransition is one append-only audit row recording a single
// classifier decision. Stored verbatim including the raw decision
// JSON so a later re-run of the classifier (different model, refined
// prompt) can replay history side-by-side with new decisions.
type TopicTransition struct {
	TurnID        string         `json:"turn_id"`
	SessionID     string         `json:"session_id"`
	FromTopicID   sql.NullString `json:"-"`
	ToTopicID     sql.NullString `json:"-"`
	Kind          string         `json:"kind"` // new | continue | resume | unknown
	Confidence    sql.NullFloat64
	Model         string    `json:"model"`
	PromptVersion string    `json:"prompt_version"`
	DecisionJSON  string    `json:"decision_json"`
	CreatedAt     time.Time `json:"created_at"`
}

// RecordTopicTransition writes one transition row. Idempotent on
// turn_id — re-running the classifier for the same turn replaces the
// previous transition record.
func (s *Store) RecordTopicTransition(ctx context.Context, t TopicTransition) error {
	const q = `
INSERT INTO topic_transitions (
  turn_id, session_id, from_topic_id, to_topic_id, kind, confidence,
  model, prompt_version, decision_json, created_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT (turn_id) DO UPDATE SET
  from_topic_id  = excluded.from_topic_id,
  to_topic_id    = excluded.to_topic_id,
  kind           = excluded.kind,
  confidence     = excluded.confidence,
  model          = excluded.model,
  prompt_version = excluded.prompt_version,
  decision_json  = excluded.decision_json,
  created_at     = excluded.created_at
`
	_, err := s.db.ExecContext(ctx, q,
		t.TurnID,
		t.SessionID,
		t.FromTopicID,
		t.ToTopicID,
		t.Kind,
		t.Confidence,
		t.Model,
		t.PromptVersion,
		t.DecisionJSON,
		t.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("record topic transition: %w", err)
	}
	return nil
}

// SetTurnTopic stamps the topic_id column on the turns row. Called
// after the classifier settles on a topic id (either an existing
// session_topics row or a freshly-inserted one).
func (s *Store) SetTurnTopic(ctx context.Context, turnID, topicID string) error {
	const q = `UPDATE turns SET topic_id = ? WHERE turn_id = ?`
	_, err := s.db.ExecContext(ctx, q, topicID, turnID)
	if err != nil {
		return fmt.Errorf("set turn topic: %w", err)
	}
	return nil
}

// SessionTopicSummary is one row of the per-session topic catalog
// rendered by `apogee topics list`. The counters are aggregated
// directly from session_topics + turns + topic_transitions in a
// single grouped query so the CLI does not have to walk the forest
// per session.
type SessionTopicSummary struct {
	SessionID          string
	SourceApp          string
	TopicCount         int
	OpenTopicCount     int
	TurnsTotal         int
	TurnsClassified    int
	TurnsLowConfidence int
	ActiveTopicID      sql.NullString
	ActiveGoal         sql.NullString
	LastSeenAt         time.Time
}

// ListSessionTopicSummaries returns one summary row per session that
// has at least one classified topic, ordered by last_seen DESC.
// Caller passes limit=0 for "no limit" (typical for `topics list`).
func (s *Store) ListSessionTopicSummaries(ctx context.Context, limit int) ([]SessionTopicSummary, error) {
	limitClause := ""
	args := []any{}
	if limit > 0 {
		limitClause = "LIMIT ?"
		args = append(args, limit)
	}
	q := `
WITH per_session AS (
  SELECT
    st.session_id,
    COUNT(*)                                               AS topic_count,
    SUM(CASE WHEN st.closed_at IS NULL THEN 1 ELSE 0 END)  AS open_count,
    MAX(st.last_seen_at)                                   AS last_seen_at
  FROM session_topics st
  GROUP BY st.session_id
), active AS (
  SELECT st.session_id, st.topic_id, st.goal
  FROM session_topics st
  INNER JOIN per_session ps
    ON ps.session_id = st.session_id AND ps.last_seen_at = st.last_seen_at
), turn_counts AS (
  SELECT
    t.session_id,
    COUNT(*) AS turns_total,
    SUM(CASE WHEN t.topic_id IS NOT NULL AND t.topic_id <> '' THEN 1 ELSE 0 END) AS turns_classified
  FROM turns t
  WHERE t.session_id IN (SELECT session_id FROM per_session)
  GROUP BY t.session_id
), low_conf AS (
  SELECT
    tr.session_id,
    COUNT(*) AS low_count
  FROM topic_transitions tr
  WHERE tr.kind = 'unknown'
  GROUP BY tr.session_id
)
SELECT
  ps.session_id,
  COALESCE(s.source_app, ''),
  ps.topic_count,
  ps.open_count,
  COALESCE(tc.turns_total, 0),
  COALESCE(tc.turns_classified, 0),
  COALESCE(lc.low_count, 0),
  active.topic_id,
  active.goal,
  ps.last_seen_at
FROM per_session ps
LEFT JOIN turn_counts tc ON tc.session_id = ps.session_id
LEFT JOIN low_conf     lc ON lc.session_id = ps.session_id
LEFT JOIN active          ON active.session_id = ps.session_id
LEFT JOIN sessions s      ON s.session_id = ps.session_id
ORDER BY ps.last_seen_at DESC
` + limitClause
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("list session topic summaries: %w", err)
	}
	defer rows.Close()
	var out []SessionTopicSummary
	for rows.Next() {
		var (
			row       SessionTopicSummary
			openCount sql.NullInt64
		)
		if err := rows.Scan(
			&row.SessionID,
			&row.SourceApp,
			&row.TopicCount,
			&openCount,
			&row.TurnsTotal,
			&row.TurnsClassified,
			&row.TurnsLowConfidence,
			&row.ActiveTopicID,
			&row.ActiveGoal,
			&row.LastSeenAt,
		); err != nil {
			return nil, fmt.Errorf("scan summary row: %w", err)
		}
		if openCount.Valid {
			row.OpenTopicCount = int(openCount.Int64)
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

// ListTopicTransitions returns the audit-trail rows for one session,
// chronologically (oldest first). Used by `apogee topics show
// <session-id>` so operators can sanity-check classifier output.
func (s *Store) ListTopicTransitions(ctx context.Context, sessionID string, limit int) ([]TopicTransition, error) {
	if limit <= 0 {
		limit = 500
	}
	const q = `
SELECT turn_id, session_id, from_topic_id, to_topic_id, kind, confidence,
       model, prompt_version, decision_json, created_at
FROM topic_transitions
WHERE session_id = ?
ORDER BY created_at ASC
LIMIT ?
`
	rows, err := s.db.QueryContext(ctx, q, sessionID, limit)
	if err != nil {
		return nil, fmt.Errorf("list topic transitions: %w", err)
	}
	defer rows.Close()
	var out []TopicTransition
	for rows.Next() {
		var t TopicTransition
		if err := rows.Scan(
			&t.TurnID,
			&t.SessionID,
			&t.FromTopicID,
			&t.ToTopicID,
			&t.Kind,
			&t.Confidence,
			&t.Model,
			&t.PromptVersion,
			&t.DecisionJSON,
			&t.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan transition: %w", err)
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

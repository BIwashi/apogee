package duckdb

import (
	"context"
	"database/sql"
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

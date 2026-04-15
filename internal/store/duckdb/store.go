// Package duckdb is the apogee collector's persistence layer. It owns the
// DuckDB schema, the open/close lifecycle, and all CRUD operations for
// sessions, turns, spans, logs, and metric points.
package duckdb

import (
	"context"
	"database/sql"
	"fmt"

	_ "github.com/marcboeker/go-duckdb/v2"
)

// Store is a thin wrapper around a *sql.DB connected to DuckDB. All public
// methods take context.Context for cancellation propagation. The zero value is
// not usable; callers must Open.
type Store struct {
	db          *sql.DB
	dsn         string
	releaseLock func() error
}

// Open returns a Store backed by DuckDB at the given dsn. Pass ":memory:" for
// an in-process database. Open also applies the embedded schema.
//
// Open performs a sidecar-lock pre-flight via AcquireDBLock so a second
// apogee process pointed at the same file fails fast with a typed
// *LockedError (wrapping ErrDBLocked) instead of the raw DuckDB driver
// error. The sidecar lock is released on Close.
func Open(ctx context.Context, dsn string) (*Store, error) {
	release, err := AcquireDBLock(dsn)
	if err != nil {
		return nil, err
	}
	db, err := sql.Open("duckdb", dsn)
	if err != nil {
		_ = release()
		return nil, fmt.Errorf("duckdb open: %w", err)
	}
	// DuckDB is single-writer. Force a single connection so concurrent calls
	// from the chi handlers serialise cleanly without lock contention.
	db.SetMaxOpenConns(1)
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		_ = release()
		return nil, fmt.Errorf("duckdb ping: %w", err)
	}
	s := &Store{db: db, dsn: dsn, releaseLock: release}
	if err := s.migrate(ctx); err != nil {
		_ = db.Close()
		_ = release()
		return nil, err
	}
	return s, nil
}

// Close releases the underlying connection and the sidecar lock.
func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	err := s.db.Close()
	if s.releaseLock != nil {
		_ = s.releaseLock()
		s.releaseLock = nil
	}
	return err
}

// DB exposes the underlying *sql.DB for tests.
func (s *Store) DB() *sql.DB { return s.db }

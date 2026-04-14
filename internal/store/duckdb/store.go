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
	db  *sql.DB
	dsn string
}

// Open returns a Store backed by DuckDB at the given dsn. Pass ":memory:" for
// an in-process database. Open also applies the embedded schema.
func Open(ctx context.Context, dsn string) (*Store, error) {
	db, err := sql.Open("duckdb", dsn)
	if err != nil {
		return nil, fmt.Errorf("duckdb open: %w", err)
	}
	// DuckDB is single-writer. Force a single connection so concurrent calls
	// from the chi handlers serialise cleanly without lock contention.
	db.SetMaxOpenConns(1)
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("duckdb ping: %w", err)
	}
	s := &Store{db: db, dsn: dsn}
	if err := s.migrate(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

// Close releases the underlying connection.
func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

// DB exposes the underlying *sql.DB for tests.
func (s *Store) DB() *sql.DB { return s.db }

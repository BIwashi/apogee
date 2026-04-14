package duckdb

import (
	"context"
	_ "embed"
	"fmt"
	"strings"
)

//go:embed schema.sql
var schemaSQL string

// migrate applies the embedded schema. DuckDB does not support multi-statement
// Exec for every statement type, so we split on semicolons and execute each
// one individually.
func (s *Store) migrate(ctx context.Context) error {
	for _, stmt := range splitStatements(schemaSQL) {
		if stmt == "" {
			continue
		}
		if _, err := s.db.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("apply schema statement %q: %w", firstLine(stmt), err)
		}
	}
	return nil
}

func splitStatements(src string) []string {
	parts := strings.Split(src, ";")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		trimmed := strings.TrimSpace(stripComments(p))
		if trimmed == "" {
			continue
		}
		out = append(out, trimmed)
	}
	return out
}

// stripComments removes leading SQL line comments so they do not become empty
// statements after splitting.
func stripComments(s string) string {
	var b strings.Builder
	for _, line := range strings.Split(s, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "--") {
			continue
		}
		b.WriteString(line)
		b.WriteByte('\n')
	}
	return b.String()
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

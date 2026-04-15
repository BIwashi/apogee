package duckdb

import (
	"context"
	"database/sql"
	"fmt"
)

// SpanDetail bundles a single span with its parent and direct children for
// the cross-cutting SpanDrawer (PR #36). Parent is nil when the span is a
// trace root; Children is empty when the span is a leaf. All three slices
// are projected from the same `spans` table and use the existing scanner.
type SpanDetail struct {
	Span     SpanRow   `json:"span"`
	Parent   *SpanRow  `json:"parent"`
	Children []SpanRow `json:"children"`
}

// GetSpanDetail returns the SpanDetail bundle for a (trace_id, span_id)
// pair. Returns nil when the span is not in the store.
func (s *Store) GetSpanDetail(ctx context.Context, traceID, spanID string) (*SpanDetail, error) {
	if traceID == "" || spanID == "" {
		return nil, nil
	}

	span, err := s.getSpanRow(ctx, traceID, spanID)
	if err != nil {
		return nil, err
	}
	if span == nil {
		return nil, nil
	}

	var parent *SpanRow
	if span.ParentSpanID != "" {
		parent, err = s.getSpanRow(ctx, traceID, span.ParentSpanID)
		if err != nil {
			return nil, err
		}
	}

	children, err := s.listSpanChildren(ctx, traceID, spanID)
	if err != nil {
		return nil, err
	}

	return &SpanDetail{
		Span:     *span,
		Parent:   parent,
		Children: children,
	}, nil
}

func (s *Store) getSpanRow(ctx context.Context, traceID, spanID string) (*SpanRow, error) {
	row := s.db.QueryRowContext(
		ctx,
		selectSpan+` WHERE trace_id = ? AND span_id = ? LIMIT 1`,
		traceID,
		spanID,
	)
	out, err := scanSpanRow(row)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("get span: %w", err)
	}
	return out, nil
}

func (s *Store) listSpanChildren(ctx context.Context, traceID, parentSpanID string) ([]SpanRow, error) {
	return s.querySpans(
		ctx,
		selectSpan+` WHERE trace_id = ? AND parent_span_id = ? ORDER BY start_time LIMIT 100`,
		traceID,
		parentSpanID,
	)
}

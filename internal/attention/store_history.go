package attention

import (
	"context"

	"github.com/BIwashi/apogee/internal/store/duckdb"
)

// StoreHistory adapts a *duckdb.Store to the attention.HistoryReader and
// HistoryWriter interfaces. It lets the engine consult the persistent
// task_type_history table without knowing anything about DuckDB.
type StoreHistory struct {
	DB  *duckdb.Store
	Ctx context.Context
}

// Lookup implements HistoryReader.
func (h *StoreHistory) Lookup(pattern string) (PatternStats, error) {
	if h == nil || h.DB == nil {
		return PatternStats{Pattern: pattern}, nil
	}
	ctx := h.Ctx
	if ctx == nil {
		ctx = context.Background()
	}
	ps, err := h.DB.GetPatternStats(ctx, pattern)
	if err != nil {
		return PatternStats{Pattern: pattern}, err
	}
	return PatternStats{
		Pattern:      ps.Pattern,
		TurnCount:    ps.TurnCount,
		FailureCount: ps.FailureCount,
		LastUpdated:  ps.LastUpdated,
	}, nil
}

// Upsert implements HistoryWriter.
func (h *StoreHistory) Upsert(pattern string, outcome Outcome) error {
	if h == nil || h.DB == nil {
		return nil
	}
	ctx := h.Ctx
	if ctx == nil {
		ctx = context.Background()
	}
	return h.DB.UpsertPatternOutcome(ctx, pattern, outcome.Success, timeNow())
}

package summarizer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/BIwashi/apogee/internal/sse"
	"github.com/BIwashi/apogee/internal/store/duckdb"
)

// NarrativeWorker is the tier-3 summarizer that sits above the per-turn
// recap (Haiku) and per-session rollup (Sonnet). It reads the closed turns
// of a session, groups them into semantic phases via an LLM call, and writes
// the resulting phases[] array back onto the existing session_rollups row.
//
// Trigger paths:
//   1. Chained from the rollup worker — when tier-2 lands, the service
//      enqueues a narrative job for the same session so phases are
//      computed immediately after.
//   2. Manual — POST /v1/sessions/:id/narrative enqueues with reason
//      "manual".
type NarrativeWorker struct {
	cfg    Config
	runner Runner
	store  *duckdb.Store
	hub    *sse.Hub
	logger *slog.Logger
	clock  func() time.Time
	prefs  PreferencesReader

	availMu      sync.RWMutex
	availability map[string]bool

	queue chan narrativeJob
	wg    sync.WaitGroup

	mu     sync.Mutex
	closed bool
}

type narrativeJob struct {
	SessionID string
	Reason    string
}

// Reason strings for narrative jobs.
const (
	NarrativeReasonSessionRollup = "session_rollup"
	NarrativeReasonManual        = "manual"
)

// narrativeStaleness is the minimum age of an existing phases[] field
// before a manual re-enqueue is honoured. Anything newer is treated as
// fresh and skipped.
const narrativeStaleness = 30 * time.Second

// NewNarrativeWorker constructs a NarrativeWorker.
func NewNarrativeWorker(cfg Config, runner Runner, store *duckdb.Store, hub *sse.Hub, logger *slog.Logger) *NarrativeWorker {
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelError}))
	}
	if cfg.QueueSize <= 0 {
		cfg.QueueSize = 64
	}
	return &NarrativeWorker{
		cfg:    cfg,
		runner: runner,
		store:  store,
		hub:    hub,
		logger: logger,
		clock:  time.Now,
		prefs:  NewStaticPreferencesReader(Defaults()),
		queue:  make(chan narrativeJob, cfg.QueueSize),
	}
}

// SetPreferencesReader installs the operator-controlled preferences source.
// nil is a no-op.
func (w *NarrativeWorker) SetPreferencesReader(r PreferencesReader) {
	if w == nil || r == nil {
		return
	}
	w.prefs = r
}

// SetAvailability installs the latest model availability snapshot.
// See Worker.SetAvailability for semantics.
func (w *NarrativeWorker) SetAvailability(avail map[string]bool) {
	if w == nil {
		return
	}
	w.availMu.Lock()
	defer w.availMu.Unlock()
	if avail == nil {
		w.availability = nil
		return
	}
	cp := make(map[string]bool, len(avail))
	for k, v := range avail {
		cp[k] = v
	}
	w.availability = cp
}

// Availability returns a safe copy of the current availability map.
func (w *NarrativeWorker) Availability() map[string]bool {
	if w == nil {
		return nil
	}
	w.availMu.RLock()
	defer w.availMu.RUnlock()
	if w.availability == nil {
		return nil
	}
	out := make(map[string]bool, len(w.availability))
	for k, v := range w.availability {
		out[k] = v
	}
	return out
}

// Enqueue drops a session id onto the queue without blocking. A full
// queue logs at WARN and drops the job.
func (w *NarrativeWorker) Enqueue(sessionID, reason string) {
	if w == nil || sessionID == "" {
		return
	}
	w.mu.Lock()
	closed := w.closed
	w.mu.Unlock()
	if closed {
		return
	}
	job := narrativeJob{SessionID: sessionID, Reason: reason}
	select {
	case w.queue <- job:
	default:
		w.logger.Warn("narrative queue full — dropping job",
			"session_id", sessionID, "reason", reason)
	}
}

// Start spawns a single worker goroutine.
func (w *NarrativeWorker) Start(ctx context.Context) {
	w.wg.Add(1)
	go w.loop(ctx)
}

// Stop closes the queue and waits for inflight jobs.
func (w *NarrativeWorker) Stop() {
	w.mu.Lock()
	if w.closed {
		w.mu.Unlock()
		return
	}
	w.closed = true
	close(w.queue)
	w.mu.Unlock()
	w.wg.Wait()
}

func (w *NarrativeWorker) loop(ctx context.Context) {
	defer w.wg.Done()
	for {
		select {
		case <-ctx.Done():
			return
		case job, ok := <-w.queue:
			if !ok {
				return
			}
			w.process(ctx, job)
		}
	}
}

// process runs a single narrative job. Errors log at WARN and return
// without touching the session_rollups row so the existing tier-2
// digest survives a bad tier-3 attempt.
func (w *NarrativeWorker) process(ctx context.Context, job narrativeJob) {
	sess, err := w.store.GetSession(ctx, job.SessionID)
	if err != nil {
		w.logger.Warn("narrative: load session", "session_id", job.SessionID, "err", err)
		return
	}
	if sess == nil {
		w.logger.Debug("narrative: unknown session", "session_id", job.SessionID)
		return
	}

	maxTurns := w.cfg.MaxRollupTurns
	if maxTurns <= 0 {
		maxTurns = defaultMaxRollupTurns
	}
	turns, err := w.store.ListSessionTurns(ctx, job.SessionID, maxTurns)
	if err != nil {
		w.logger.Warn("narrative: load turns", "session_id", job.SessionID, "err", err)
		return
	}

	var closedTurns []duckdb.Turn
	for _, t := range turns {
		if t.Status != "running" {
			closedTurns = append(closedTurns, t)
		}
	}
	if len(closedTurns) < 2 {
		w.logger.Debug("narrative: skipping — too few closed turns",
			"session_id", job.SessionID,
			"closed_count", len(closedTurns))
		return
	}
	sortRollupTurns(closedTurns)

	// Must have a tier-2 rollup to chain from. If the row is missing
	// entirely (e.g. manual call on a fresh session) we skip — the
	// narrative is meant to *extend* the rollup, not replace it.
	row, ok, err := w.store.GetSessionRollup(ctx, job.SessionID)
	if err != nil {
		w.logger.Warn("narrative: load rollup", "session_id", job.SessionID, "err", err)
		return
	}
	if !ok || row.RollupJSON == "" {
		w.logger.Debug("narrative: skipping — no tier-2 rollup yet",
			"session_id", job.SessionID)
		return
	}
	var rollup Rollup
	if err := json.Unmarshal([]byte(row.RollupJSON), &rollup); err != nil {
		w.logger.Warn("narrative: decode existing rollup",
			"session_id", job.SessionID, "err", err)
		return
	}

	// Staleness guard: skip if the narrative for this session has
	// generated_at within 30s OR if the rollup has not changed since the
	// last narrative run. Manual triggers still bypass the 30s floor.
	now := w.clock()
	if !rollup.NarrativeGeneratedAt.IsZero() {
		if now.Sub(rollup.NarrativeGeneratedAt) < narrativeStaleness {
			w.logger.Debug("narrative: existing phases are fresh — skipping",
				"session_id", job.SessionID)
			return
		}
		if job.Reason != NarrativeReasonManual && !row.GeneratedAt.After(rollup.NarrativeGeneratedAt) {
			w.logger.Debug("narrative: rollup unchanged since last run — skipping",
				"session_id", job.SessionID)
			return
		}
	}

	// Load preferences.
	prefs := Defaults()
	if w.prefs != nil {
		loaded, err := w.prefs.LoadSummarizerPreferences(ctx)
		if err != nil {
			w.logger.Warn("narrative: load preferences", "session_id", job.SessionID, "err", err)
		} else {
			prefs = loaded
		}
	}
	// Narrative falls back to the rollup config slot when the
	// narrative-specific alias is not set, then to the catalog resolver
	// so a fresh install with no config still gets a sensible default.
	configModel := w.cfg.NarrativeModel
	if strings.TrimSpace(configModel) == "" {
		configModel = w.cfg.RollupModel
	}
	model := ResolveModelForUseCase(
		UseCaseNarrative,
		prefs.NarrativeModelOverride,
		configModel,
		w.Availability(),
	)

	// Build the narrative turn list, pulling per-turn headline / key_steps
	// out of each turn's recap_json blob and a tool summary from its spans.
	narrativeTurns := make([]NarrativeTurn, 0, len(closedTurns))
	for i, t := range closedTurns {
		nt := NarrativeTurn{
			Index:     i,
			TurnID:    t.TurnID,
			StartedAt: t.StartedAt,
			Status:    t.Status,
			Headline:  t.Headline,
		}
		if t.EndedAt != nil {
			nt.EndedAt = *t.EndedAt
		}
		if t.DurationMs != nil {
			nt.DurationMs = *t.DurationMs
		}
		if t.RecapJSON != "" {
			var recap Recap
			if err := json.Unmarshal([]byte(t.RecapJSON), &recap); err == nil {
				if recap.Headline != "" {
					nt.Headline = recap.Headline
				}
				nt.Outcome = string(recap.Outcome)
				nt.KeySteps = recap.KeySteps
			}
		}
		nt.ToolSummary = w.toolSummaryForTurn(ctx, t.TurnID)
		narrativeTurns = append(narrativeTurns, nt)
	}

	prompt := BuildNarrativePrompt(NarrativePromptInput{
		SessionID: sess.SessionID,
		SourceApp: sess.SourceApp,
		Turns:     narrativeTurns,
		Rollup:    rollup,
	}, prefs)

	runCtx := ctx
	if w.cfg.Timeout > 0 {
		var cancel context.CancelFunc
		runCtx, cancel = context.WithTimeout(ctx, w.cfg.Timeout)
		defer cancel()
	}

	w.logger.Info("narrative: running",
		"session_id", job.SessionID,
		"model", model,
		"language", prefs.Language,
		"turn_count", len(narrativeTurns),
		"reason", job.Reason)

	output, err := w.runner.Run(runCtx, model, prompt)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return
		}
		w.logger.Warn("narrative: runner error",
			"session_id", job.SessionID, "model", model, "err", err)
		return
	}

	parsed, err := ParseNarrativeResponse(output, len(narrativeTurns))
	if err != nil {
		w.logger.Warn("narrative: parse error",
			"session_id", job.SessionID, "err", err,
			"raw", truncate(output, 1024))
		return
	}
	// Forecast is optional and best-effort — a failure here must not
	// block the historical phases from persisting, so we log and move
	// on with an empty forecast.
	forecast, fErr := ParseNarrativeForecast(output)
	if fErr != nil {
		w.logger.Debug("narrative: forecast parse error (ignored)",
			"session_id", job.SessionID, "err", fErr)
	}

	// Convert the parsed phases (which carry turn indices into the
	// ordered list) into PhaseBlock rows with real turn ids, timestamps,
	// duration, and a merged tool summary.
	phases := make([]PhaseBlock, 0, len(parsed))
	for i, p := range parsed {
		block := PhaseBlock{
			Index:       i,
			Headline:    p.Headline,
			Narrative:   p.Narrative,
			KeySteps:    append([]string(nil), p.KeySteps...),
			Kind:        p.Kind,
			ToolSummary: map[string]int{},
		}
		if p.FirstTurnIndex < 0 || p.LastTurnIndex >= len(narrativeTurns) {
			continue
		}
		for j := p.FirstTurnIndex; j <= p.LastTurnIndex; j++ {
			t := narrativeTurns[j]
			block.TurnIDs = append(block.TurnIDs, t.TurnID)
			for k, v := range t.ToolSummary {
				block.ToolSummary[k] += v
			}
		}
		block.TurnCount = len(block.TurnIDs)
		if block.TurnCount == 0 {
			continue
		}
		block.StartedAt = narrativeTurns[p.FirstTurnIndex].StartedAt
		block.EndedAt = narrativeTurns[p.LastTurnIndex].EndedAt
		if block.EndedAt.IsZero() {
			block.EndedAt = narrativeTurns[p.LastTurnIndex].StartedAt
		}
		if !block.EndedAt.IsZero() && !block.StartedAt.IsZero() {
			block.DurationMs = block.EndedAt.Sub(block.StartedAt).Milliseconds()
		}
		phases = append(phases, block)
	}

	// Merge into the rollup blob. We preserve every existing field and
	// only replace phases + forecast + narrative_generated_at +
	// narrative_model.
	rollup.Phases = phases
	rollup.Forecast = make([]ForecastPhase, 0, len(forecast))
	for _, f := range forecast {
		rollup.Forecast = append(rollup.Forecast, ForecastPhase{
			Kind:      f.Kind,
			Headline:  f.Headline,
			Rationale: f.Rationale,
		})
	}
	rollup.NarrativeGeneratedAt = now
	rollup.NarrativeModel = model

	blob, err := json.Marshal(rollup)
	if err != nil {
		w.logger.Warn("narrative: marshal", "session_id", job.SessionID, "err", err)
		return
	}

	// Write back — preserve the tier-2 metadata on the row and only swap
	// the JSON blob + touch generated_at to the narrative write time.
	updated := duckdb.SessionRollup{
		SessionID:   row.SessionID,
		GeneratedAt: row.GeneratedAt,
		Model:       row.Model,
		FromTurnID:  row.FromTurnID,
		ToTurnID:    row.ToTurnID,
		TurnCount:   row.TurnCount,
		RollupJSON:  string(blob),
	}
	if err := w.store.UpsertSessionRollup(ctx, updated); err != nil {
		w.logger.Warn("narrative: persist", "session_id", job.SessionID, "err", err)
		return
	}

	w.logger.Info("narrative: written",
		"session_id", job.SessionID,
		"phase_count", len(phases),
		"forecast_count", len(rollup.Forecast),
		"turn_count", len(narrativeTurns))

	if w.hub != nil {
		w.hub.Broadcast(sse.NewSessionEvent(now, *sess))
	}
}

// toolSummaryForTurn counts tool_name occurrences in the spans belonging
// to the turn. Zero value is an empty map, never nil so the prompt builder
// can range over it without a nil check.
func (w *NarrativeWorker) toolSummaryForTurn(ctx context.Context, turnID string) map[string]int {
	out := map[string]int{}
	if w.store == nil {
		return out
	}
	spans, err := w.store.GetSpansByTurn(ctx, turnID)
	if err != nil {
		return out
	}
	for _, sp := range spans {
		name := strings.TrimSpace(sp.ToolName)
		if name == "" {
			continue
		}
		out[name]++
	}
	return out
}

// ParsedPhase is the intermediate shape returned by ParseNarrativeResponse.
// Index values refer to positions in the turn list passed to the prompt
// builder; the worker maps them back to turn ids before persisting.
type ParsedPhase struct {
	Headline       string   `json:"headline"`
	Narrative      string   `json:"narrative"`
	KeySteps       []string `json:"key_steps"`
	Kind           string   `json:"kind"`
	FirstTurnIndex int      `json:"first_turn_index"`
	LastTurnIndex  int      `json:"last_turn_index"`
}

// ParsedForecastPhase is the intermediate shape for one predicted
// upcoming phase. The narrative worker persists these verbatim onto
// the rollup's Forecast field so the dashboard can render dimmed
// "next stops" planets beyond the realised phase chain.
type ParsedForecastPhase struct {
	Kind      string `json:"kind"`
	Headline  string `json:"headline"`
	Rationale string `json:"rationale,omitempty"`
}

type narrativeResponse struct {
	Phases   []ParsedPhase         `json:"phases"`
	Forecast []ParsedForecastPhase `json:"forecast,omitempty"`
}

// ParseNarrativeForecast extracts the optional forecast[] field from a
// tier-3 response body. It reuses the same JSON extraction trick as
// ParseNarrativeResponse so the caller can run both parsers on the
// same raw output. A response without a forecast field returns an
// empty slice and a nil error — the forecast is optional by design
// and the UI treats missing forecasts as "no prediction". Invalid
// entries are filtered silently: a single bad entry must not block
// the narrative phases from persisting.
func ParseNarrativeForecast(raw string) ([]ParsedForecastPhase, error) {
	cleaned := strings.TrimSpace(raw)
	cleaned = stripCodeFences(cleaned)
	cleaned = extractJSONObject(cleaned)
	if cleaned == "" {
		return nil, nil
	}
	var resp narrativeResponse
	if err := json.Unmarshal([]byte(cleaned), &resp); err != nil {
		return nil, nil
	}
	if len(resp.Forecast) == 0 {
		return nil, nil
	}
	out := make([]ParsedForecastPhase, 0, len(resp.Forecast))
	for _, f := range resp.Forecast {
		f.Headline = strings.TrimSpace(f.Headline)
		if f.Headline == "" {
			continue
		}
		if len(f.Headline) > 140 {
			f.Headline = f.Headline[:140]
		}
		f.Rationale = strings.TrimSpace(f.Rationale)
		if len(f.Rationale) > 200 {
			f.Rationale = f.Rationale[:200]
		}
		if f.Kind == "" || !validPhaseKinds[f.Kind] {
			f.Kind = PhaseKindOther
		}
		out = append(out, f)
		if len(out) >= 3 {
			break
		}
	}
	return out, nil
}

// ParseNarrativeResponse tolerates the same LLM output quirks the recap
// parser handles (code fences, leading/trailing prose) and enforces the
// schema rules: non-empty headline, valid kind enum (falling back to
// "other"), contiguous turn index coverage, non-overlapping and
// inclusive ranges.
func ParseNarrativeResponse(raw string, turnCount int) ([]ParsedPhase, error) {
	cleaned := strings.TrimSpace(raw)
	cleaned = stripCodeFences(cleaned)
	cleaned = extractJSONObject(cleaned)
	if cleaned == "" {
		return nil, fmt.Errorf("narrative: empty or unparseable input")
	}
	var resp narrativeResponse
	if err := json.Unmarshal([]byte(cleaned), &resp); err != nil {
		return nil, fmt.Errorf("narrative: unmarshal: %w", err)
	}
	if len(resp.Phases) == 0 {
		return nil, fmt.Errorf("narrative: response contains no phases")
	}

	out := make([]ParsedPhase, 0, len(resp.Phases))
	expectedNext := 0
	for i, p := range resp.Phases {
		p.Headline = strings.TrimSpace(p.Headline)
		if p.Headline == "" {
			return nil, fmt.Errorf("narrative: phase %d missing headline", i)
		}
		if len(p.Headline) > 140 {
			p.Headline = p.Headline[:140]
		}
		p.Narrative = strings.TrimSpace(p.Narrative)
		if len(p.Narrative) > 500 {
			p.Narrative = p.Narrative[:500]
		}
		p.KeySteps = truncateStringSlice(p.KeySteps, 5, 80)
		if p.Kind == "" || !validPhaseKinds[p.Kind] {
			p.Kind = PhaseKindOther
		}
		if p.FirstTurnIndex < 0 {
			return nil, fmt.Errorf("narrative: phase %d has negative first_turn_index", i)
		}
		if p.LastTurnIndex < p.FirstTurnIndex {
			return nil, fmt.Errorf("narrative: phase %d has last < first (%d < %d)", i, p.LastTurnIndex, p.FirstTurnIndex)
		}
		if turnCount > 0 && p.LastTurnIndex >= turnCount {
			return nil, fmt.Errorf("narrative: phase %d last_turn_index %d out of range (turn_count=%d)", i, p.LastTurnIndex, turnCount)
		}
		if p.FirstTurnIndex != expectedNext {
			return nil, fmt.Errorf("narrative: phase %d starts at %d, expected %d (coverage gap or overlap)", i, p.FirstTurnIndex, expectedNext)
		}
		expectedNext = p.LastTurnIndex + 1
		out = append(out, p)
	}
	if turnCount > 0 && expectedNext != turnCount {
		return nil, fmt.Errorf("narrative: phases cover [0, %d) but turn_count=%d", expectedNext, turnCount)
	}
	return out, nil
}

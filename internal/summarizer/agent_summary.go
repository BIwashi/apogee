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

// AgentSummary is the structured per-agent label produced by AgentSummaryWorker.
// Mirrors the TypeScript AgentSummary type in web/app/lib/api-types.ts.
//
// Title is the short headline shown in the /agents catalog list (e.g.
// "Investigating auth middleware migration"). Role is a one-sentence
// description of the agent's responsibility in the session. Focus is a small
// set of free-form tags the LLM extracted (file types, tools, domains).
type AgentSummary struct {
	Title       string    `json:"title"`
	Role        string    `json:"role"`
	Focus       []string  `json:"focus,omitempty"`
	GeneratedAt time.Time `json:"generated_at"`
	Model       string    `json:"model"`
}

// agentSummaryJob is the unit of work consumed by AgentSummaryWorker.loop.
type agentSummaryJob struct {
	AgentID   string
	SessionID string
	Reason    string
}

// Reason strings for agent-summary jobs.
const (
	AgentSummaryReasonManual        = "manual"
	AgentSummaryReasonSessionEnd    = "session_end"
	AgentSummaryReasonSessionRollup = "session_rollup"
	AgentSummaryReasonScheduled     = "scheduled"
)

// agentSummaryStaleness is the minimum age of an existing summary before the
// scheduled / chained paths overwrite it. Manual triggers ignore this.
const agentSummaryStaleness = 5 * time.Minute

// agentSummaryMaxTurns caps the number of turns loaded into one prompt.
const agentSummaryMaxTurns = 12

// agentSummaryMaxTools caps the tool histogram fed into the prompt.
const agentSummaryMaxTools = 12

// AgentSummaryWorker is the per-agent label goroutine. Architecturally
// identical to RollupWorker; the tier difference is the model alias and the
// prompt content.
type AgentSummaryWorker struct {
	cfg    Config
	runner Runner
	store  *duckdb.Store
	hub    *sse.Hub
	logger *slog.Logger
	clock  func() time.Time
	prefs  PreferencesReader

	availMu      sync.RWMutex
	availability map[string]bool

	queue chan agentSummaryJob
	wg    sync.WaitGroup

	mu     sync.Mutex
	closed bool
}

// NewAgentSummaryWorker constructs an AgentSummaryWorker.
func NewAgentSummaryWorker(cfg Config, runner Runner, store *duckdb.Store, hub *sse.Hub, logger *slog.Logger) *AgentSummaryWorker {
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelError}))
	}
	if cfg.QueueSize <= 0 {
		cfg.QueueSize = 64
	}
	return &AgentSummaryWorker{
		cfg:    cfg,
		runner: runner,
		store:  store,
		hub:    hub,
		logger: logger,
		clock:  time.Now,
		prefs:  NewStaticPreferencesReader(Defaults()),
		queue:  make(chan agentSummaryJob, cfg.QueueSize),
	}
}

// SetPreferencesReader installs the operator-controlled preferences source.
func (w *AgentSummaryWorker) SetPreferencesReader(r PreferencesReader) {
	if w == nil || r == nil {
		return
	}
	w.prefs = r
}

// SetAvailability installs the latest model availability snapshot.
func (w *AgentSummaryWorker) SetAvailability(avail map[string]bool) {
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
func (w *AgentSummaryWorker) Availability() map[string]bool {
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

// Enqueue drops an agent id onto the queue without blocking. A full queue
// logs at WARN and drops the job.
func (w *AgentSummaryWorker) Enqueue(agentID, sessionID, reason string) {
	if w == nil || agentID == "" || sessionID == "" {
		return
	}
	w.mu.Lock()
	closed := w.closed
	w.mu.Unlock()
	if closed {
		return
	}
	job := agentSummaryJob{AgentID: agentID, SessionID: sessionID, Reason: reason}
	select {
	case w.queue <- job:
	default:
		w.logger.Warn("agent summary queue full — dropping job",
			"agent_id", agentID, "session_id", sessionID, "reason", reason)
	}
}

// EnqueueSession fans out to every distinct (agent_id, session_id) candidate
// in the session that has either no summary or a stale one.
func (w *AgentSummaryWorker) EnqueueSession(ctx context.Context, sessionID, reason string) {
	if w == nil || sessionID == "" {
		return
	}
	candidates, err := w.store.ListAgentSummaryCandidates(ctx, sessionID, agentSummaryStaleness, 50)
	if err != nil {
		w.logger.Warn("agent summary: list candidates",
			"session_id", sessionID, "err", err)
		return
	}
	for _, c := range candidates {
		w.Enqueue(c.AgentID, c.SessionID, reason)
	}
}

// Start spawns a single worker goroutine.
func (w *AgentSummaryWorker) Start(ctx context.Context) {
	w.wg.Add(1)
	go w.loop(ctx)
}

// Stop closes the queue and waits for the in-flight job to finish.
func (w *AgentSummaryWorker) Stop() {
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

func (w *AgentSummaryWorker) loop(ctx context.Context) {
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

// process runs a single agent-summary job. Errors log at WARN and return
// without touching the agent_summaries row.
func (w *AgentSummaryWorker) process(ctx context.Context, job agentSummaryJob) {
	// Load the agent aggregate (counts, type, parent etc.).
	agent, err := w.store.GetAgentDetail(ctx, job.AgentID)
	if err != nil {
		w.logger.Warn("agent summary: load agent",
			"agent_id", job.AgentID, "err", err)
		return
	}
	if agent == nil {
		w.logger.Debug("agent summary: unknown agent",
			"agent_id", job.AgentID)
		return
	}
	if agent.Agent.SessionID == "" {
		// No session means we can't key the summary row. Skip.
		return
	}

	// Staleness check for non-manual triggers. The candidate query already
	// filters out fresh rows for the EnqueueSession path; this guard catches
	// re-enqueues from manual / per-agent paths that bypass the candidate
	// query.
	if job.Reason != AgentSummaryReasonManual {
		if existing, ok, err := w.store.GetAgentSummary(ctx, job.AgentID, agent.Agent.SessionID); err == nil && ok {
			if w.clock().Sub(existing.GeneratedAt) < agentSummaryStaleness &&
				existing.InvocationCountAtGeneration >= agent.Agent.InvocationCount {
				w.logger.Debug("agent summary: existing row is fresh — skipping",
					"agent_id", job.AgentID, "session_id", agent.Agent.SessionID)
				return
			}
		}
	}

	// Skip agents with no recorded activity — there's nothing to summarise.
	if agent.Agent.InvocationCount == 0 {
		return
	}

	// Load operator preferences.
	prefs := Defaults()
	if w.prefs != nil {
		loaded, err := w.prefs.LoadSummarizerPreferences(ctx)
		if err != nil {
			w.logger.Warn("agent summary: load preferences",
				"agent_id", job.AgentID, "err", err)
		} else {
			prefs = loaded
		}
	}

	model := ResolveModelForUseCase(
		UseCaseAgentSummary,
		"", // no per-tier preference override yet — recap_model_override is recap-tier only
		"",
		w.Availability(),
	)

	prompt := BuildAgentSummaryPrompt(*agent, prefs)
	runCtx := ctx
	if w.cfg.Timeout > 0 {
		var cancel context.CancelFunc
		runCtx, cancel = context.WithTimeout(ctx, w.cfg.Timeout)
		defer cancel()
	}

	w.logger.Info("agent summary: running",
		"agent_id", job.AgentID,
		"session_id", agent.Agent.SessionID,
		"model", model,
		"language", prefs.Language,
		"reason", job.Reason)

	output, err := w.runner.Run(runCtx, model, prompt)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return
		}
		w.logger.Warn("agent summary: runner error",
			"agent_id", job.AgentID, "model", model, "err", err)
		return
	}

	summary, err := ParseAgentSummary(output)
	if err != nil {
		w.logger.Warn("agent summary: parse error",
			"agent_id", job.AgentID, "err", err,
			"raw", truncate(output, 1024))
		return
	}
	now := w.clock()
	summary.GeneratedAt = now
	summary.Model = model

	blob, err := json.Marshal(summary)
	if err != nil {
		w.logger.Warn("agent summary: marshal",
			"agent_id", job.AgentID, "err", err)
		return
	}

	row := duckdb.AgentSummary{
		AgentID:                     job.AgentID,
		SessionID:                   agent.Agent.SessionID,
		GeneratedAt:                 now,
		Model:                       model,
		Title:                       summary.Title,
		SummaryJSON:                 string(blob),
		InvocationCountAtGeneration: agent.Agent.InvocationCount,
	}
	if summary.Role != "" {
		row.Role.String = summary.Role
		row.Role.Valid = true
	}
	if err := w.store.UpsertAgentSummary(ctx, row); err != nil {
		w.logger.Warn("agent summary: persist",
			"agent_id", job.AgentID, "err", err)
		return
	}

	w.logger.Info("agent summary: written",
		"agent_id", job.AgentID,
		"session_id", agent.Agent.SessionID,
		"title", summary.Title)

	// Broadcast as a session event so the dashboard refreshes the agents
	// list. We don't have a dedicated "agent updated" SSE type yet — the
	// session refresh fires the right SWR mutator on the frontend because
	// the agents list lives on a session-keyed cache.
	if w.hub != nil {
		if sess, err := w.store.GetSession(ctx, agent.Agent.SessionID); err == nil && sess != nil {
			w.hub.Broadcast(sse.NewSessionEvent(now, *sess))
		}
	}
}

// ParseAgentSummary tolerates the same LLM output quirks Parse handles for
// the per-turn recap. Validation enforces a non-empty title and trims long
// strings.
func ParseAgentSummary(raw string) (AgentSummary, error) {
	cleaned := strings.TrimSpace(raw)
	cleaned = stripCodeFences(cleaned)
	cleaned = extractJSONObject(cleaned)
	if cleaned == "" {
		return AgentSummary{}, fmt.Errorf("agent summary: empty or unparseable input")
	}
	var s AgentSummary
	if err := json.Unmarshal([]byte(cleaned), &s); err != nil {
		return AgentSummary{}, fmt.Errorf("agent summary: unmarshal: %w", err)
	}
	s.Title = strings.TrimSpace(s.Title)
	if s.Title == "" {
		return AgentSummary{}, fmt.Errorf("agent summary: title is required")
	}
	if len(s.Title) > 100 {
		s.Title = s.Title[:100]
	}
	s.Role = strings.TrimSpace(s.Role)
	if len(s.Role) > 240 {
		s.Role = s.Role[:240]
	}
	s.Focus = truncateStringSlice(s.Focus, 5, 40)
	return s, nil
}

// BuildAgentSummaryPrompt assembles the LLM input for one agent-summary job.
// The detail bundle already carries the agent aggregate, recent turns, tool
// histogram, and parent — enough context for the LLM to decide what this
// agent does.
func BuildAgentSummaryPrompt(detail duckdb.AgentDetail, prefs Preferences) string {
	var sb strings.Builder
	sb.WriteString("You are labelling one Claude Code agent inside an observability dashboard.\n\n")
	sb.WriteString("## Agent metadata\n")
	fmt.Fprintf(&sb, "agent_id: %s\n", detail.Agent.AgentID)
	if detail.Agent.AgentType != "" {
		fmt.Fprintf(&sb, "agent_type: %s\n", detail.Agent.AgentType)
	}
	fmt.Fprintf(&sb, "kind: %s\n", agentKindLabel(detail.Agent))
	fmt.Fprintf(&sb, "session_id: %s\n", detail.Agent.SessionID)
	fmt.Fprintf(&sb, "invocation_count: %d\n", detail.Agent.InvocationCount)
	fmt.Fprintf(&sb, "total_duration_ms: %d\n", detail.Agent.TotalDurationMs)
	fmt.Fprintf(&sb, "last_seen_at: %s\n", formatTime(detail.Agent.LastSeen))

	if detail.Parent != nil && detail.Parent.AgentID != "" {
		sb.WriteString("\n## Parent agent\n")
		fmt.Fprintf(&sb, "parent_agent_id: %s\n", detail.Parent.AgentID)
		if detail.Parent.AgentType != "" {
			fmt.Fprintf(&sb, "parent_agent_type: %s\n", detail.Parent.AgentType)
		}
		if detail.Parent.Title != "" {
			fmt.Fprintf(&sb, "parent_title: %s\n", detail.Parent.Title)
		}
	}

	if len(detail.ToolCounts) > 0 {
		sb.WriteString("\n## Tool histogram (most-used first)\n")
		limit := len(detail.ToolCounts)
		if limit > agentSummaryMaxTools {
			limit = agentSummaryMaxTools
		}
		for i := 0; i < limit; i++ {
			t := detail.ToolCounts[i]
			fmt.Fprintf(&sb, "- %s: %d\n", t.Name, t.Count)
		}
	}

	if len(detail.Turns) > 0 {
		sb.WriteString("\n## Recent turns this agent participated in\n")
		limit := len(detail.Turns)
		if limit > agentSummaryMaxTurns {
			limit = agentSummaryMaxTurns
		}
		for i := 0; i < limit; i++ {
			writeAgentSummaryTurn(&sb, i+1, detail.Turns[i])
		}
	}

	sb.WriteString("\n## Instruction\n")
	sb.WriteString(agentSummaryInstructionBlock(prefs.Language))
	return sb.String()
}

// writeAgentSummaryTurn renders one turn line in the prompt. Mirrors the
// shape used by the rollup prompt but avoids dragging in writeTurnLine so
// agent-summary stays trimmer.
func writeAgentSummaryTurn(sb *strings.Builder, idx int, t duckdb.Turn) {
	headline := t.Headline
	if t.RecapJSON != "" {
		var recap Recap
		if err := json.Unmarshal([]byte(t.RecapJSON), &recap); err == nil && recap.Headline != "" {
			headline = recap.Headline
		}
	}
	if headline == "" {
		headline = oneLine(t.PromptText)
		if len(headline) > 120 {
			headline = headline[:120] + "…"
		}
	}
	fmt.Fprintf(sb, "[%d] %s %s %s\n", idx, formatTime(t.StartedAt), t.Status, headline)
}

func agentKindLabel(a duckdb.Agent) string {
	if a.Kind == "main" || a.Kind == "MAIN" {
		return "main"
	}
	if a.Kind == "subagent" || a.Kind == "SUBAGENT" || a.Kind == "sub" {
		return "subagent"
	}
	if a.AgentType == "" || a.AgentType == "main" {
		return "main"
	}
	return "subagent"
}

func agentSummaryInstructionBlock(language string) string {
	switch language {
	case LanguageJA:
		return agentSummaryInstructionBlockJA
	default:
		return agentSummaryInstructionBlockEN
	}
}

const agentSummaryInstructionBlockEN = `
Produce a single JSON object matching exactly:

type AgentSummary = {
  title: string;   // 4-10 words, max 100 chars; describe what the agent is doing.
                   //   Prefer present-progressive phrasing ("Refactoring auth middleware",
                   //   "Investigating CI failure"). No leading verbs like "Agent that".
  role:  string;   // one sentence, max 240 chars; describe the agent's responsibility
                   //   in this session. Mention domain (file paths, tool families,
                   //   parent task) when available.
  focus: string[]; // 0-5 short tags (max 40 chars each). Free-form: file types,
                   //   tool names, domains. Skip when nothing concrete stands out.
};

Rules:
- Be concrete. Cite file names, tool names, and concrete actions instead of
  abstractions like "various tasks" or "general work".
- For subagents, focus on the slice of work the parent delegated.
- For main agents, focus on the dominant theme of the session as a whole.
- Never use the literal word "main" or "subagent" inside title — it's already
  shown as a separate badge in the UI.

Output ONLY the JSON object.
`

const agentSummaryInstructionBlockJA = `
日本語で応答してください。以下に正確に一致する単一の JSON オブジェクトを
生成してください:

type AgentSummary = {
  title: string;   // 4-15 字、最大 100 字。エージェントが何をしているかを
                   //   現在進行形で記述（例: "認証ミドルウェアのリファクタ"、
                   //   "CI 失敗の調査"）。"〜するエージェント" のような
                   //   修飾は不要。
  role:  string;   // 一文、最大 240 字。このセッションでのエージェントの役割を
                   //   記述。可能ならドメイン（ファイルパス、ツール群、親タスク）
                   //   に触れる。
  focus: string[]; // 短いタグ 0-5 件（各最大 40 字）。ファイル種別、ツール名、
                   //   ドメインなど自由形式。具体的な特徴がなければ省略。
};

ルール:
- 具体的に書いてください。"様々なタスク" のような抽象表現ではなく、
  ファイル名・ツール名・実際の操作を引用してください。
- サブエージェントの場合、親から委譲された作業スライスに焦点を当ててください。
- メインエージェントの場合、セッション全体の主要テーマに焦点を当ててください。
- "main" や "subagent" という単語を title に含めないでください
  （UI で別バッジとして表示されます）。
- すべてのテキストフィールドは日本語で記述してください。

JSON オブジェクトのみを出力してください。
`

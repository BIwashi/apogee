// Package semconv exposes Go constants for the apogee `claude_code.*`
// OpenTelemetry semantic conventions registry. The authoritative source
// of truth lives in `semconv/model/registry.yaml`; the constants below
// must stay in lockstep with it. `attrs_test.go` enforces that link.
//
// We deliberately do not depend on an external code generator in this
// PR — the constants are hand-written so a fresh checkout builds with
// nothing more than `go build ./...`. When the registry grows large
// enough to warrant codegen we can add a `weaver`-style generator
// without breaking the import surface this package presents today.
package semconv

import "go.opentelemetry.io/otel/attribute"

// SchemaVersion is the registry version this package mirrors. It is a
// development pre-release until the conventions stabilise.
const SchemaVersion = "0.0.0-dev"

// SchemaURL identifies this registry to OTel SDKs that key on schemas.
// We host the canonical YAML inside the apogee repo until a shared
// publication point exists.
const SchemaURL = "https://github.com/BIwashi/apogee/semconv/" + SchemaVersion

// ----------------------------------------------------------------------------
// claude_code.session.*
// ----------------------------------------------------------------------------

var (
	SessionID        = attribute.Key("claude_code.session.id")
	SessionSourceApp = attribute.Key("claude_code.session.source_app")
	SessionMachineID = attribute.Key("claude_code.session.machine_id")
	SessionModel     = attribute.Key("claude_code.session.model")
)

// ----------------------------------------------------------------------------
// claude_code.turn.*
// ----------------------------------------------------------------------------

var (
	TurnID            = attribute.Key("claude_code.turn.id")
	TurnStatus        = attribute.Key("claude_code.turn.status")
	TurnPromptChars   = attribute.Key("claude_code.turn.prompt_chars")
	TurnOutputChars   = attribute.Key("claude_code.turn.output_chars")
	TurnToolCallCount = attribute.Key("claude_code.turn.tool_call_count")
	TurnSubagentCount = attribute.Key("claude_code.turn.subagent_count")
	TurnErrorCount    = attribute.Key("claude_code.turn.error_count")
)

// Turn status enum values.
const (
	TurnStatusRunning   = "running"
	TurnStatusCompleted = "completed"
	TurnStatusErrored   = "errored"
	TurnStatusStopped   = "stopped"
	TurnStatusCompacted = "compacted"
)

// ----------------------------------------------------------------------------
// claude_code.agent.*
// ----------------------------------------------------------------------------

var (
	AgentID       = attribute.Key("claude_code.agent.id")
	AgentKind     = attribute.Key("claude_code.agent.kind")
	AgentParentID = attribute.Key("claude_code.agent.parent_id")
	AgentType     = attribute.Key("claude_code.agent.type")
)

// Agent kind enum values.
const (
	AgentKindMain     = "main"
	AgentKindSubagent = "subagent"
)

// ----------------------------------------------------------------------------
// claude_code.tool.*
// ----------------------------------------------------------------------------

var (
	ToolName          = attribute.Key("claude_code.tool.name")
	ToolUseID         = attribute.Key("claude_code.tool.use_id")
	ToolMCPServer     = attribute.Key("claude_code.tool.mcp.server")
	ToolMCPName       = attribute.Key("claude_code.tool.mcp.name")
	ToolInputSummary  = attribute.Key("claude_code.tool.input_summary")
	ToolOutputSummary = attribute.Key("claude_code.tool.output_summary")
	ToolBlockedReason = attribute.Key("claude_code.tool.blocked_reason")
)

// ----------------------------------------------------------------------------
// claude_code.phase.*
// ----------------------------------------------------------------------------

var (
	PhaseName       = attribute.Key("claude_code.phase.name")
	PhaseInferredBy = attribute.Key("claude_code.phase.inferred_by")
)

// Phase name enum values (open set — agents may declare their own).
const (
	PhasePlan     = "plan"
	PhaseExplore  = "explore"
	PhaseEdit     = "edit"
	PhaseTest     = "test"
	PhaseRun      = "run"
	PhaseCommit   = "commit"
	PhaseDebug    = "debug"
	PhaseDelegate = "delegate"
	PhaseVerify   = "verify"
	PhaseIdle     = "idle"
)

// PhaseInferredBy enum values.
const (
	PhaseInferredByHeuristic     = "heuristic"
	PhaseInferredByLLM           = "llm"
	PhaseInferredByAgentDeclared = "agent_declared"
)

// ----------------------------------------------------------------------------
// claude_code.hitl.*
// ----------------------------------------------------------------------------

var (
	HITLID             = attribute.Key("claude_code.hitl.id")
	HITLKind           = attribute.Key("claude_code.hitl.kind")
	HITLStatus         = attribute.Key("claude_code.hitl.status")
	HITLDecision       = attribute.Key("claude_code.hitl.decision")
	HITLReasonCategory = attribute.Key("claude_code.hitl.reason_category")
	HITLOperatorNote   = attribute.Key("claude_code.hitl.operator_note")
	HITLResumeMode     = attribute.Key("claude_code.hitl.resume_mode")
)

// HITL kind enum values.
const (
	HITLKindPermission   = "permission"
	HITLKindToolApproval = "tool_approval"
	HITLKindPrompt       = "prompt"
	HITLKindChoice       = "choice"
)

// HITL status enum values.
const (
	HITLStatusPending   = "pending"
	HITLStatusResponded = "responded"
	HITLStatusTimeout   = "timeout"
	HITLStatusError     = "error"
	HITLStatusExpired   = "expired"
)

// HITL decision enum values.
const (
	HITLDecisionAllow   = "allow"
	HITLDecisionDeny    = "deny"
	HITLDecisionCustom  = "custom"
	HITLDecisionTimeout = "timeout"
)

// HITL resume mode enum values.
const (
	HITLResumeContinue    = "continue"
	HITLResumeRetry       = "retry"
	HITLResumeAbort       = "abort"
	HITLResumeAlternative = "alternative"
)

// ----------------------------------------------------------------------------
// claude_code.intervention.*
// ----------------------------------------------------------------------------

var (
	InterventionID           = attribute.Key("claude_code.intervention.id")
	InterventionDeliveryMode = attribute.Key("claude_code.intervention.delivery_mode")
	InterventionScope        = attribute.Key("claude_code.intervention.scope")
	InterventionUrgency      = attribute.Key("claude_code.intervention.urgency")
	InterventionStatus       = attribute.Key("claude_code.intervention.status")
	InterventionDeliveredVia = attribute.Key("claude_code.intervention.delivered_via")
	InterventionOperatorID   = attribute.Key("claude_code.intervention.operator_id")
)

// Intervention delivery mode enum values.
const (
	InterventionDeliveryInterrupt = "interrupt"
	InterventionDeliveryContext   = "context"
	InterventionDeliveryBoth      = "both"
)

// Intervention scope enum values.
const (
	InterventionScopeThisTurn    = "this_turn"
	InterventionScopeThisSession = "this_session"
)

// Intervention urgency enum values.
const (
	InterventionUrgencyHigh   = "high"
	InterventionUrgencyNormal = "normal"
	InterventionUrgencyLow    = "low"
)

// Intervention status enum values.
const (
	InterventionStatusQueued    = "queued"
	InterventionStatusClaimed   = "claimed"
	InterventionStatusDelivered = "delivered"
	InterventionStatusConsumed  = "consumed"
	InterventionStatusExpired   = "expired"
	InterventionStatusCancelled = "cancelled"
)

// EventInterventionDelivered is the span event name emitted by the
// reconstructor when an operator intervention lands on Claude Code.
const EventInterventionDelivered = "claude_code.intervention.delivered"

// ----------------------------------------------------------------------------
// claude_code.attention.*
// ----------------------------------------------------------------------------

var (
	AttentionState  = attribute.Key("claude_code.attention.state")
	AttentionScore  = attribute.Key("claude_code.attention.score")
	AttentionReason = attribute.Key("claude_code.attention.reason")
)

// Attention state enum values.
const (
	AttentionStateHealthy      = "healthy"
	AttentionStateWatchlist    = "watchlist"
	AttentionStateWatch        = "watch"
	AttentionStateInterveneNow = "intervene_now"
)

// ----------------------------------------------------------------------------
// claude_code.recap.*
// ----------------------------------------------------------------------------

var (
	RecapHeadline     = attribute.Key("claude_code.recap.headline")
	RecapOutcome      = attribute.Key("claude_code.recap.outcome")
	RecapModel        = attribute.Key("claude_code.recap.model")
	RecapKeySteps     = attribute.Key("claude_code.recap.key_steps")
	RecapFailureCause = attribute.Key("claude_code.recap.failure_cause")
)

// Recap outcome enum values.
const (
	RecapOutcomeSuccess = "success"
	RecapOutcomePartial = "partial"
	RecapOutcomeFailure = "failure"
	RecapOutcomeAborted = "aborted"
)

// ----------------------------------------------------------------------------
// claude_code.compaction.*
// ----------------------------------------------------------------------------

var CompactionTrigger = attribute.Key("claude_code.compaction.trigger")

// ----------------------------------------------------------------------------
// claude_code.prompt.* (event attributes)
// ----------------------------------------------------------------------------

var (
	PromptText  = attribute.Key("claude_code.prompt.text")
	PromptChars = attribute.Key("claude_code.prompt.chars")
)

// ----------------------------------------------------------------------------
// claude_code.assistant_message.* (event attributes)
// ----------------------------------------------------------------------------

var (
	AssistantMessageText  = attribute.Key("claude_code.assistant_message.text")
	AssistantMessageChars = attribute.Key("claude_code.assistant_message.chars")
)

// ----------------------------------------------------------------------------
// claude_code.notification.* (event attributes)
// ----------------------------------------------------------------------------

var (
	NotificationType    = attribute.Key("claude_code.notification.type")
	NotificationMessage = attribute.Key("claude_code.notification.message")
)

// ----------------------------------------------------------------------------
// gen_ai.* — reuse of the OpenTelemetry GenAI semconv SIG keys for model
// and token attributes. apogee writes these alongside the claude_code.*
// keys whenever a model name or token count is available.
// ----------------------------------------------------------------------------

var (
	GenAISystem            = attribute.Key("gen_ai.system")
	GenAIRequestModel      = attribute.Key("gen_ai.request.model")
	GenAIResponseModel     = attribute.Key("gen_ai.response.model")
	GenAIOperationName     = attribute.Key("gen_ai.operation.name")
	GenAIUsageInputTokens  = attribute.Key("gen_ai.usage.input_tokens")
	GenAIUsageOutputTokens = attribute.Key("gen_ai.usage.output_tokens")
)

// GenAI system constant values used by apogee.
const (
	GenAISystemAnthropic = "anthropic"
)

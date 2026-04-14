package semconv

// Canonical span name constants for Claude Code execution. All apogee
// reconstructor spans MUST use one of these prefixes so dashboards and
// downstream OTel pipelines can pivot on the namespace.
const (
	// SpanTurn is the root span for one user turn.
	SpanTurn = "claude_code.turn"
	// SpanToolPrefix prefixes every tool call span; the suffix is the
	// tool name (e.g. claude_code.tool.Bash).
	SpanToolPrefix = "claude_code.tool"
	// SpanSubagentPrefix prefixes every subagent span; the suffix is
	// the subagent type (e.g. claude_code.subagent.Explore).
	SpanSubagentPrefix = "claude_code.subagent"
	// SpanHITLPermission is the canonical name for a HITL permission
	// request span.
	SpanHITLPermission = "claude_code.hitl.permission"
	// SpanTurnRecap is the post-hoc enrichment span emitted when a turn
	// recap lands. It is short-lived and linked to the turn root.
	SpanTurnRecap = "claude_code.turn.recap"
)

// ToolSpanName returns the canonical span name for a tool call with the
// given tool name.
func ToolSpanName(toolName string) string {
	if toolName == "" {
		return SpanToolPrefix
	}
	return SpanToolPrefix + "." + toolName
}

// SubagentSpanName returns the canonical span name for a subagent of the
// given declared kind/type.
func SubagentSpanName(kind string) string {
	if kind == "" {
		return SpanSubagentPrefix
	}
	return SpanSubagentPrefix + "." + kind
}

// MCPToolSpanName returns the canonical span name for an MCP-provided
// tool call. server and tool are the parsed components of the
// `mcp__server__tool` naming convention used by Claude Code.
func MCPToolSpanName(server, tool string) string {
	if server == "" {
		return ToolSpanName(tool)
	}
	if tool == "" {
		return SpanToolPrefix + ".mcp." + server
	}
	return SpanToolPrefix + ".mcp." + server + "." + tool
}

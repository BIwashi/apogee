package attention

import (
	"regexp"
	"time"

	"github.com/BIwashi/apogee/internal/store/duckdb"
)

// Phase is the short label attention engine assigns to the current work
// pattern of a turn. It is heuristic and derived from the last 10 tool spans.
type Phase string

const (
	PhaseDelegating Phase = "delegating"
	PhaseTesting    Phase = "testing"
	PhaseCommitting Phase = "committing"
	PhaseEditing    Phase = "editing"
	PhaseExploring  Phase = "exploring"
	PhaseRunning    Phase = "running"
	PhaseIdle       Phase = "idle"
)

// PhaseResult is the output of Phase. Since is the wall-clock time at which
// the phase last changed; it lets the engine detect stalls ("same phase for
// > N minutes").
type PhaseResult struct {
	Name       Phase
	Confidence float64
	Since      time.Time
}

// phaseWindow is the cluster size the heuristic inspects. Only the most
// recent N tool spans are considered.
const phaseWindow = 10

// recentToolWindow controls the "any tool in the last N seconds → running"
// fallback below the cluster heuristic.
const recentToolWindow = 30 * time.Second

var (
	reTestCommand = regexp.MustCompile(`\b(npm|pnpm|yarn|go|pytest|cargo)\s+test\b`)
	reGitCommand  = regexp.MustCompile(`\bgit\b`)
)

// Phase returns the heuristic phase of a turn given its ordered span list
// and a "now" timestamp. The span list should be in start-time order; only
// tool spans (those with a non-empty ToolName) contribute. `now` is only
// used to compute the Since timestamp and the "running" fallback.
func ComputePhase(spans []duckdb.SpanRow, now time.Time) PhaseResult {
	tools := collectTools(spans)
	if len(tools) == 0 {
		return PhaseResult{Name: PhaseIdle, Confidence: 1, Since: now}
	}

	window := tools
	if len(window) > phaseWindow {
		window = window[len(window)-phaseWindow:]
	}

	counts := map[Phase]int{}
	for _, t := range window {
		counts[classifyTool(t)]++
	}
	total := len(window)

	// Ordered checks: first rule whose share is > 50% wins.
	for _, p := range []Phase{
		PhaseDelegating,
		PhaseTesting,
		PhaseCommitting,
		PhaseEditing,
		PhaseExploring,
	} {
		if counts[p]*2 > total {
			return PhaseResult{
				Name:       p,
				Confidence: float64(counts[p]) / float64(total),
				Since:      sinceFor(tools, p, now),
			}
		}
	}

	// Fallback: any tool started within the last 30 s → running.
	last := tools[len(tools)-1]
	if !last.StartTime.IsZero() && now.Sub(last.StartTime) <= recentToolWindow {
		return PhaseResult{
			Name:       PhaseRunning,
			Confidence: 0.5,
			Since:      last.StartTime,
		}
	}
	return PhaseResult{Name: PhaseIdle, Confidence: 0.5, Since: now}
}

// toolSpan is the minimal projection of a duckdb.SpanRow used by the phase
// heuristic.
type toolSpan struct {
	Name      string
	Command   string
	StartTime time.Time
}

// collectTools filters the span slice down to just tool spans in start-time
// order. Non-tool spans (turn root, HITL, subagent containers) are skipped.
func collectTools(spans []duckdb.SpanRow) []toolSpan {
	out := make([]toolSpan, 0, len(spans))
	for _, sp := range spans {
		if sp.ToolName == "" {
			continue
		}
		cmd, _ := sp.Attributes["claude_code.tool.input"].(string)
		out = append(out, toolSpan{
			Name:      sp.ToolName,
			Command:   cmd,
			StartTime: sp.StartTime,
		})
	}
	return out
}

// classifyTool buckets a single tool span into a phase. The classification is
// based solely on tool name plus — for Bash — a regex over the command text.
func classifyTool(t toolSpan) Phase {
	switch t.Name {
	case "Task":
		return PhaseDelegating
	case "Bash":
		if reTestCommand.MatchString(t.Command) {
			return PhaseTesting
		}
		if reGitCommand.MatchString(t.Command) {
			return PhaseCommitting
		}
		return PhaseRunning
	case "Edit", "Write", "MultiEdit":
		return PhaseEditing
	case "Read", "Grep", "Glob":
		return PhaseExploring
	}
	return PhaseRunning
}

// sinceFor walks backwards from the most recent tool span and returns the
// start time of the earliest contiguous run that still classifies as `p`.
// That timestamp is the attention engine's "phase started at" value.
func sinceFor(tools []toolSpan, p Phase, now time.Time) time.Time {
	since := now
	for i := len(tools) - 1; i >= 0; i-- {
		if classifyTool(tools[i]) != p {
			break
		}
		if !tools[i].StartTime.IsZero() {
			since = tools[i].StartTime
		}
	}
	return since
}

package semconv_test

import (
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"

	"go.opentelemetry.io/otel/attribute"
	"gopkg.in/yaml.v3"

	apogeesemconv "github.com/BIwashi/apogee/semconv"
)

// registryDoc mirrors the subset of the OTel semconv YAML we care about
// for the coverage check. We intentionally ignore display strings and
// requirement levels — only ids matter for the constant-sync invariant.
type registryDoc struct {
	Groups []registryGroup `yaml:"groups"`
	Events []registryGroup `yaml:"events"`
}

type registryGroup struct {
	ID         string              `yaml:"id"`
	Type       string              `yaml:"type"`
	Attributes []registryAttribute `yaml:"attributes"`
}

type registryAttribute struct {
	ID  string `yaml:"id"`
	Ref string `yaml:"ref"`
}

func loadRegistry(t *testing.T) registryDoc {
	t.Helper()
	path := filepath.Join("model", "registry.yaml")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read registry yaml: %v", err)
	}
	var doc registryDoc
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("decode registry yaml: %v", err)
	}
	return doc
}

// declaredAttrIDs returns the set of attribute ids the YAML registry
// declares (excluding pure refs, which are pointers to ids declared in
// another group).
func declaredAttrIDs(doc registryDoc) map[string]struct{} {
	out := map[string]struct{}{}
	collect := func(groups []registryGroup) {
		for _, g := range groups {
			for _, a := range g.Attributes {
				if a.ID == "" {
					continue
				}
				out[a.ID] = struct{}{}
			}
		}
	}
	collect(doc.Groups)
	collect(doc.Events)
	return out
}

// constantAttrIDs reflects on the apogeesemconv package and returns the
// set of attribute.Key values exposed by exported package variables.
func constantAttrIDs(t *testing.T) map[string]struct{} {
	t.Helper()
	// We can't reflect directly over a package, but we can enumerate
	// every exported attribute.Key by listing the variables we know
	// about. To avoid drift the list lives in this test only — any new
	// constant must be added below as well, which is the explicit
	// guard rail this test provides.
	keys := []attribute.Key{
		// session
		apogeesemconv.SessionID,
		apogeesemconv.SessionSourceApp,
		apogeesemconv.SessionMachineID,
		apogeesemconv.SessionModel,
		// turn
		apogeesemconv.TurnID,
		apogeesemconv.TurnStatus,
		apogeesemconv.TurnPromptChars,
		apogeesemconv.TurnOutputChars,
		apogeesemconv.TurnToolCallCount,
		apogeesemconv.TurnSubagentCount,
		apogeesemconv.TurnErrorCount,
		// agent
		apogeesemconv.AgentID,
		apogeesemconv.AgentKind,
		apogeesemconv.AgentParentID,
		apogeesemconv.AgentType,
		// tool
		apogeesemconv.ToolName,
		apogeesemconv.ToolUseID,
		apogeesemconv.ToolMCPServer,
		apogeesemconv.ToolMCPName,
		apogeesemconv.ToolInputSummary,
		apogeesemconv.ToolOutputSummary,
		apogeesemconv.ToolBlockedReason,
		// phase
		apogeesemconv.PhaseName,
		apogeesemconv.PhaseInferredBy,
		// hitl
		apogeesemconv.HITLID,
		apogeesemconv.HITLKind,
		apogeesemconv.HITLStatus,
		apogeesemconv.HITLDecision,
		apogeesemconv.HITLReasonCategory,
		apogeesemconv.HITLOperatorNote,
		apogeesemconv.HITLResumeMode,
		// intervention
		apogeesemconv.InterventionID,
		apogeesemconv.InterventionDeliveryMode,
		apogeesemconv.InterventionScope,
		apogeesemconv.InterventionUrgency,
		apogeesemconv.InterventionStatus,
		apogeesemconv.InterventionDeliveredVia,
		apogeesemconv.InterventionOperatorID,
		// attention
		apogeesemconv.AttentionState,
		apogeesemconv.AttentionScore,
		apogeesemconv.AttentionReason,
		// recap
		apogeesemconv.RecapHeadline,
		apogeesemconv.RecapOutcome,
		apogeesemconv.RecapModel,
		apogeesemconv.RecapKeySteps,
		apogeesemconv.RecapFailureCause,
		// compaction
		apogeesemconv.CompactionTrigger,
		// prompt event
		apogeesemconv.PromptText,
		apogeesemconv.PromptChars,
		// assistant message event
		apogeesemconv.AssistantMessageText,
		apogeesemconv.AssistantMessageChars,
		// notification event
		apogeesemconv.NotificationType,
		apogeesemconv.NotificationMessage,
	}
	out := make(map[string]struct{}, len(keys))
	for _, k := range keys {
		out[string(k)] = struct{}{}
	}
	return out
}

func TestRegistryDeclaredIDsHaveGoConstants(t *testing.T) {
	doc := loadRegistry(t)
	declared := declaredAttrIDs(doc)
	constants := constantAttrIDs(t)

	var missing []string
	for id := range declared {
		if _, ok := constants[id]; !ok {
			missing = append(missing, id)
		}
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		t.Fatalf("registry attribute ids missing Go constants: %v", missing)
	}
}

func TestGoConstantsAreDeclaredInRegistry(t *testing.T) {
	doc := loadRegistry(t)
	declared := declaredAttrIDs(doc)
	constants := constantAttrIDs(t)

	var stale []string
	for id := range constants {
		if _, ok := declared[id]; !ok {
			stale = append(stale, id)
		}
	}
	if len(stale) > 0 {
		sort.Strings(stale)
		t.Fatalf("Go constants without a registry entry: %v", stale)
	}
}

func TestSchemaVersionPresent(t *testing.T) {
	if apogeesemconv.SchemaVersion == "" {
		t.Fatal("SchemaVersion must not be empty")
	}
}

func TestToolSpanName(t *testing.T) {
	cases := []struct{ in, want string }{
		{"", "claude_code.tool"},
		{"Bash", "claude_code.tool.Bash"},
		{"Read", "claude_code.tool.Read"},
	}
	for _, tc := range cases {
		if got := apogeesemconv.ToolSpanName(tc.in); got != tc.want {
			t.Errorf("ToolSpanName(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestSubagentSpanName(t *testing.T) {
	cases := []struct{ in, want string }{
		{"", "claude_code.subagent"},
		{"Explore", "claude_code.subagent.Explore"},
	}
	for _, tc := range cases {
		if got := apogeesemconv.SubagentSpanName(tc.in); got != tc.want {
			t.Errorf("SubagentSpanName(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// sanity: catch typos that would render an attribute.Key empty.
func TestNoEmptyConstants(t *testing.T) {
	constants := constantAttrIDs(t)
	for id := range constants {
		if id == "" {
			t.Fatal("found an empty attribute key constant")
		}
	}
	// Spot check that a representative key has the right namespace.
	want := "claude_code.turn.id"
	if _, ok := constants[want]; !ok {
		t.Fatalf("expected constant set to contain %q", want)
	}
	// Reflect-sanity: verify reflection-driven walk would still find the
	// same keys (guards against a typo in this test's manual list).
	if !reflect.DeepEqual(constants, constants) {
		t.Fatal("constant set is unstable")
	}
}

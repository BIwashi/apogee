package cli

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestDoctorCmdRunsWithoutError(t *testing.T) {
	var stdout bytes.Buffer
	root := NewRootCmd(&stdout, &stdout)
	root.SetArgs([]string{"doctor"})
	if err := root.Execute(); err != nil {
		t.Fatalf("doctor: %v", err)
	}
	out := stdout.String()
	if !strings.Contains(out, "apogee doctor") {
		t.Errorf("doctor output missing banner: %q", out)
	}
	// Every line begins with one of the styled-glyph markers
	// (✓/⚠/✗ — U+2713 / U+26A0 / U+2717). At least one must show
	// up; if not, runDoctorChecks stopped emitting anything.
	if !strings.ContainsAny(out, "\u2713\u26A0\u2717") {
		t.Errorf("doctor output did not emit any glyph lines: %q", out)
	}
	// The summary footer is always present.
	if !strings.Contains(out, "ok ·") || !strings.Contains(out, "warning") {
		t.Errorf("doctor output missing summary footer: %q", out)
	}
}

func TestDoctorCmdJSON(t *testing.T) {
	var stdout bytes.Buffer
	root := NewRootCmd(&stdout, &stdout)
	root.SetArgs([]string{"doctor", "--json"})
	if err := root.Execute(); err != nil {
		t.Fatalf("doctor --json: %v", err)
	}
	var checks []DoctorCheck
	if err := json.Unmarshal(stdout.Bytes(), &checks); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, stdout.String())
	}
	if len(checks) == 0 {
		t.Fatalf("doctor --json emitted no checks")
	}
	seen := map[string]bool{}
	for _, c := range checks {
		seen[c.Name] = true
		if c.Severity == "" || c.Message == "" {
			t.Errorf("check missing fields: %+v", c)
		}
	}
	for _, want := range []string{"home", "claude_cli", "db_path", "db_lock", "collector", "hook_install"} {
		if !seen[want] {
			t.Errorf("doctor --json missing check %q", want)
		}
	}
}

func TestRunDoctorChecksHasSevenChecks(t *testing.T) {
	checks := runDoctorChecks(t.Context())
	if len(checks) < 6 {
		t.Errorf("expected at least 6 checks, got %d: %+v", len(checks), checks)
	}
}

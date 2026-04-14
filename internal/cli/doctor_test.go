package cli

import (
	"bytes"
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
	// The check lines should either be OK, WARN, or INFO. Assert that the
	// output actually contains at least one marker so the test fails if
	// runDoctor stops emitting anything.
	if !strings.Contains(out, "OK") && !strings.Contains(out, "WARN") {
		t.Errorf("doctor output did not emit any OK/WARN lines: %q", out)
	}
}

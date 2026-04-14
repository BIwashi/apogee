package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/BIwashi/apogee/internal/version"
)

func TestVersionCmdPrintsFullBuildInfo(t *testing.T) {
	var stdout bytes.Buffer
	root := NewRootCmd(&stdout, &stdout)
	root.SetArgs([]string{"version"})
	if err := root.Execute(); err != nil {
		t.Fatalf("version: %v", err)
	}
	got := stdout.String()
	if !strings.Contains(got, "apogee v"+version.Version) {
		t.Errorf("version output missing version string: %q", got)
	}
	if !strings.Contains(got, "commit ") {
		t.Errorf("version output missing commit field: %q", got)
	}
	if !strings.Contains(got, "built ") {
		t.Errorf("version output missing built field: %q", got)
	}
}

func TestVersionFlagShort(t *testing.T) {
	var stdout bytes.Buffer
	root := NewRootCmd(&stdout, &stdout)
	root.SetArgs([]string{"--version"})
	if err := root.Execute(); err != nil {
		t.Fatalf("--version: %v", err)
	}
	if got := strings.TrimSpace(stdout.String()); got != version.Short() {
		t.Errorf("--version output: got %q, want %q", got, version.Short())
	}
}

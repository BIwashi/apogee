package cli

import (
	"bytes"
	"strings"
	"testing"
)

func TestOpenCmdHelp(t *testing.T) {
	var stdout, stderr bytes.Buffer
	root := NewRootCmd(&stdout, &stderr)
	root.SetArgs([]string{"open", "--help"})
	if err := root.Execute(); err != nil {
		t.Fatalf("open --help: %v", err)
	}
	if !strings.Contains(stdout.String(), "open") {
		t.Errorf("expected help output for `open`, got: %s", stdout.String())
	}
}

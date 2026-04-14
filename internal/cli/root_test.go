package cli

import (
	"bytes"
	"strings"
	"testing"
)

func TestRootCmdSubcommandTree(t *testing.T) {
	root := NewRootCmd(&bytes.Buffer{}, &bytes.Buffer{})
	want := map[string]bool{
		"serve":   false,
		"init":    false,
		"hooks":   false,
		"version": false,
		"doctor":  false,
	}
	for _, c := range root.Commands() {
		if _, ok := want[c.Name()]; ok {
			want[c.Name()] = true
		}
	}
	for name, seen := range want {
		if !seen {
			t.Errorf("root command is missing subcommand %q", name)
		}
	}
}

func TestRootCmdHooksExtract(t *testing.T) {
	root := NewRootCmd(&bytes.Buffer{}, &bytes.Buffer{})
	hooks, _, err := root.Find([]string{"hooks"})
	if err != nil {
		t.Fatalf("find hooks: %v", err)
	}
	var sawExtract bool
	for _, c := range hooks.Commands() {
		if c.Name() == "extract" {
			sawExtract = true
		}
	}
	if !sawExtract {
		t.Errorf("hooks command is missing the extract verb")
	}
}

func TestRootCmdHelpLists(t *testing.T) {
	var stdout bytes.Buffer
	root := NewRootCmd(&stdout, &stdout)
	root.SetArgs([]string{"--help"})
	if err := root.Execute(); err != nil {
		t.Fatalf("help: %v", err)
	}
	out := stdout.String()
	for _, name := range []string{"serve", "init", "hooks", "version", "doctor"} {
		if !strings.Contains(out, name) {
			t.Errorf("help output missing %q: %s", name, out)
		}
	}
}

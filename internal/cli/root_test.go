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
		"hook":    false,
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

func TestRootCmdHooksSubcommandRemoved(t *testing.T) {
	root := NewRootCmd(&bytes.Buffer{}, &bytes.Buffer{})
	for _, c := range root.Commands() {
		if c.Name() == "hooks" {
			t.Errorf("`hooks` subcommand should be removed; found %q", c.Name())
		}
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
	for _, name := range []string{"serve", "init", "hook", "version", "doctor"} {
		if !strings.Contains(out, name) {
			t.Errorf("help output missing %q: %s", name, out)
		}
	}
}

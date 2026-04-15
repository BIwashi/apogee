package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestUninstallPromptAborts(t *testing.T) {
	fm := newFakeManager()
	fm.installed = true
	withFakeManager(t, fm)

	var stdout, stderr bytes.Buffer
	stdin := strings.NewReader("n\n")
	if err := runUninstall(t.Context(), &stdout, &stderr, stdin, false, false); err != nil {
		t.Fatalf("uninstall: %v", err)
	}
	if !strings.Contains(stdout.String(), "aborted") {
		t.Errorf("expected abort message, got: %s", stdout.String())
	}
	if fm.uninstallCalls != 0 {
		t.Errorf("expected no uninstall call when user says no")
	}
}

func TestUninstallYesRemovesHooksAndDaemon(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	// Stage a ~/.claude/settings.json with an apogee hook entry.
	claudeDir := filepath.Join(tmp, ".claude")
	if err := os.MkdirAll(claudeDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	settings := map[string]any{
		"hooks": map[string]any{
			"PreToolUse": []any{
				map[string]any{
					"hooks": []any{
						map[string]any{"type": "command", "command": "/usr/local/bin/apogee hook --event PreToolUse"},
					},
				},
				// A non-apogee entry should survive.
				map[string]any{
					"hooks": []any{
						map[string]any{"type": "command", "command": "echo unrelated"},
					},
				},
			},
		},
	}
	data, _ := json.Marshal(settings)
	if err := os.WriteFile(filepath.Join(claudeDir, "settings.json"), data, 0o644); err != nil {
		t.Fatalf("write settings: %v", err)
	}

	fm := newFakeManager()
	fm.installed = true
	fm.running = true
	withFakeManager(t, fm)

	// Simulate "y" on the data-dir prompt (daemon+hook prompt is
	// skipped via --yes). But we don't have a data dir, so the
	// prompt is not shown.
	var stdout, stderr bytes.Buffer
	if err := runUninstall(t.Context(), &stdout, &stderr, strings.NewReader(""), true, false); err != nil {
		t.Fatalf("uninstall: %v", err)
	}
	if fm.uninstallCalls == 0 {
		t.Errorf("expected uninstall call")
	}
	// Verify the apogee hook was removed but the unrelated one kept.
	raw, err := os.ReadFile(filepath.Join(claudeDir, "settings.json"))
	if err != nil {
		t.Fatalf("read settings: %v", err)
	}
	if strings.Contains(string(raw), "apogee hook") {
		t.Errorf("expected apogee hook removed, settings now: %s", raw)
	}
	if !strings.Contains(string(raw), "echo unrelated") {
		t.Errorf("unrelated hook should survive, settings now: %s", raw)
	}
	if !strings.Contains(stdout.String(), "removed 1 apogee hook") {
		t.Errorf("expected hook removal summary, got: %s", stdout.String())
	}
}

func TestUninstallPurgesDataDir(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	// Create a fake data dir.
	dataDir := filepath.Join(tmp, ".apogee")
	if err := os.MkdirAll(filepath.Join(dataDir, "logs"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dataDir, "apogee.duckdb"), []byte("db"), 0o644); err != nil {
		t.Fatalf("write db: %v", err)
	}

	fm := newFakeManager()
	withFakeManager(t, fm)

	var stdout, stderr bytes.Buffer
	if err := runUninstall(t.Context(), &stdout, &stderr, strings.NewReader(""), true, true); err != nil {
		t.Fatalf("uninstall: %v", err)
	}
	if _, err := os.Stat(dataDir); !os.IsNotExist(err) {
		t.Errorf("expected data dir gone, stat err: %v", err)
	}
}

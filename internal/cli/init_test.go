package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func newTestConfig(t *testing.T) InitConfig {
	t.Helper()
	target := filepath.Join(t.TempDir(), "project", ".claude")
	hooksDir := t.TempDir()
	return InitConfig{
		Target:    target,
		SourceApp: "test-app",
		ServerURL: "http://localhost:4100/v1/events",
		Scope:     ScopeProject,
		HooksDir:  hooksDir,
	}
}

func readSettings(t *testing.T, path string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var out map[string]any
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	return out
}

func TestInitWritesSettingsJSON(t *testing.T) {
	cfg := newTestConfig(t)
	result, err := Init(cfg)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	if len(result.Added) != len(HookEvents) {
		t.Errorf("expected %d added, got %d", len(HookEvents), len(result.Added))
	}
	if len(result.Skipped) != 0 {
		t.Errorf("expected 0 skipped on fresh install, got %d", len(result.Skipped))
	}

	settings := readSettings(t, cfg.SettingsPath())
	hooksRaw, ok := settings["hooks"].(map[string]any)
	if !ok {
		t.Fatalf("hooks section missing or wrong type: %T", settings["hooks"])
	}

	for _, event := range HookEvents {
		entries, ok := hooksRaw[event].([]any)
		if !ok {
			t.Fatalf("event %s not installed: %T", event, hooksRaw[event])
		}
		if len(entries) != 1 {
			t.Errorf("event %s: expected 1 entry, got %d", event, len(entries))
		}
		first, _ := entries[0].(map[string]any)
		inner, _ := first["hooks"].([]any)
		if len(inner) != 1 {
			t.Fatalf("event %s: expected 1 inner hook, got %d", event, len(inner))
		}
		h, _ := inner[0].(map[string]any)
		if h["type"] != "command" {
			t.Errorf("event %s: type=%v", event, h["type"])
		}
		cmd, _ := h["command"].(string)
		if !strings.Contains(cmd, "send_event.py") {
			t.Errorf("event %s: command missing send_event.py: %s", event, cmd)
		}
		if !strings.Contains(cmd, "--event-type "+event) {
			t.Errorf("event %s: command missing --event-type %s: %s", event, event, cmd)
		}
		if !strings.Contains(cmd, "--source-app test-app") {
			t.Errorf("event %s: command missing source-app: %s", event, cmd)
		}
	}
}

func TestInitExtractsEmbeddedHooksWhenMissing(t *testing.T) {
	cfg := newTestConfig(t)
	// HooksDir is an empty temp dir; Init should extract into it.
	if _, err := Init(cfg); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if _, err := os.Stat(filepath.Join(cfg.HooksDir, "send_event.py")); err != nil {
		t.Errorf("send_event.py was not extracted: %v", err)
	}
}

func TestInitPreservesExistingSettingsKeys(t *testing.T) {
	cfg := newTestConfig(t)
	if err := os.MkdirAll(cfg.Target, 0o755); err != nil {
		t.Fatal(err)
	}
	preset := map[string]any{
		"editor": map[string]any{"theme": "dark"},
		"hooks": map[string]any{
			"SessionStart": []any{
				map[string]any{
					"hooks": []any{
						map[string]any{
							"type":    "command",
							"command": "echo user-hook",
						},
					},
				},
			},
		},
	}
	presetBytes, _ := json.MarshalIndent(preset, "", "  ")
	if err := os.WriteFile(cfg.SettingsPath(), presetBytes, 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := Init(cfg); err != nil {
		t.Fatalf("Init: %v", err)
	}

	settings := readSettings(t, cfg.SettingsPath())
	if _, ok := settings["editor"]; !ok {
		t.Error("existing 'editor' key was dropped")
	}
	hooksRaw := settings["hooks"].(map[string]any)
	entries := hooksRaw["SessionStart"].([]any)
	if len(entries) != 2 {
		t.Errorf("expected 2 SessionStart entries (user + apogee), got %d", len(entries))
	}

	// First entry is the user's original.
	userEntry := entries[0].(map[string]any)
	userInner := userEntry["hooks"].([]any)[0].(map[string]any)
	if userInner["command"] != "echo user-hook" {
		t.Errorf("user hook not preserved: %v", userInner["command"])
	}
}

func TestInitSkipsExistingApogeeEntry(t *testing.T) {
	cfg := newTestConfig(t)
	if _, err := Init(cfg); err != nil {
		t.Fatalf("first init: %v", err)
	}
	result, err := Init(cfg)
	if err != nil {
		t.Fatalf("second init: %v", err)
	}
	if len(result.Skipped) != len(HookEvents) {
		t.Errorf("second run should skip all events, got added=%v skipped=%v", result.Added, result.Skipped)
	}
	if len(result.Added) != 0 {
		t.Errorf("second run should not add anything, got %v", result.Added)
	}
}

func TestInitForceOverwritesApogeeEntry(t *testing.T) {
	cfg := newTestConfig(t)
	if _, err := Init(cfg); err != nil {
		t.Fatalf("first init: %v", err)
	}
	cfg.Force = true
	cfg.SourceApp = "renamed-app"
	result, err := Init(cfg)
	if err != nil {
		t.Fatalf("force init: %v", err)
	}
	if len(result.Added) != len(HookEvents) {
		t.Errorf("force run should re-add all events, got %v", result.Added)
	}

	settings := readSettings(t, cfg.SettingsPath())
	hooksRaw := settings["hooks"].(map[string]any)
	entries := hooksRaw["PreToolUse"].([]any)
	if len(entries) != 1 {
		t.Fatalf("expected 1 PreToolUse entry after force, got %d", len(entries))
	}
	cmd := entries[0].(map[string]any)["hooks"].([]any)[0].(map[string]any)["command"].(string)
	if !strings.Contains(cmd, "--source-app renamed-app") {
		t.Errorf("command not updated with new source-app: %s", cmd)
	}
	if strings.Contains(cmd, "--source-app test-app") {
		t.Errorf("command still references old source-app: %s", cmd)
	}
}

func TestInitDryRunMakesNoChanges(t *testing.T) {
	cfg := newTestConfig(t)
	cfg.DryRun = true
	result, err := Init(cfg)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	if len(result.Added) != len(HookEvents) {
		t.Errorf("dry run should still produce added list, got %v", result.Added)
	}
	if _, err := os.Stat(cfg.SettingsPath()); !os.IsNotExist(err) {
		t.Errorf("dry run wrote %s (err=%v)", cfg.SettingsPath(), err)
	}
}

func TestInitInvalidHooksSection(t *testing.T) {
	cfg := newTestConfig(t)
	if err := os.MkdirAll(cfg.Target, 0o755); err != nil {
		t.Fatal(err)
	}
	bad := map[string]any{"hooks": "not-an-object"}
	b, _ := json.Marshal(bad)
	if err := os.WriteFile(cfg.SettingsPath(), b, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Init(cfg); err == nil {
		t.Error("expected error for malformed hooks section")
	}
}

func TestResolveTargetUserScope(t *testing.T) {
	out, err := ResolveTarget("./.claude", ScopeUser)
	if err != nil {
		t.Fatalf("ResolveTarget: %v", err)
	}
	home, _ := os.UserHomeDir()
	if out != filepath.Join(home, ".claude") {
		t.Errorf("user scope should resolve to $HOME/.claude, got %s", out)
	}
}

func TestDeriveSourceAppFromTarget(t *testing.T) {
	got := deriveSourceApp("/tmp/my-project/.claude")
	if got != "my-project" {
		t.Errorf("deriveSourceApp: got %q, want %q", got, "my-project")
	}
}

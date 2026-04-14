package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const testHookCommand = "/fake/apogee hook"

func newTestConfig(t *testing.T) InitConfig {
	t.Helper()
	target := filepath.Join(t.TempDir(), "project", ".claude")
	return InitConfig{
		Target:      target,
		SourceApp:   "test-app",
		ServerURL:   "http://localhost:4100/v1/events",
		Scope:       ScopeProject,
		HookCommand: testHookCommand,
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

func commandForEvent(t *testing.T, settings map[string]any, event string) string {
	t.Helper()
	hooksRaw, ok := settings["hooks"].(map[string]any)
	if !ok {
		t.Fatalf("hooks section missing: %T", settings["hooks"])
	}
	entries, ok := hooksRaw[event].([]any)
	if !ok {
		t.Fatalf("event %s not installed: %T", event, hooksRaw[event])
	}
	if len(entries) == 0 {
		t.Fatalf("event %s has zero entries", event)
	}
	first, _ := entries[0].(map[string]any)
	inner, _ := first["hooks"].([]any)
	if len(inner) == 0 {
		t.Fatalf("event %s has zero inner hooks", event)
	}
	h, _ := inner[0].(map[string]any)
	cmd, _ := h["command"].(string)
	return cmd
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
		if !strings.HasPrefix(cmd, testHookCommand+" ") {
			t.Errorf("event %s: command missing hook prefix: %s", event, cmd)
		}
		if !strings.Contains(cmd, "--event "+event) {
			t.Errorf("event %s: command missing --event %s: %s", event, event, cmd)
		}
		if strings.Contains(cmd, "--event-type") {
			t.Errorf("event %s: command still uses legacy --event-type flag: %s", event, cmd)
		}
		if strings.Contains(cmd, "send_event.py") {
			t.Errorf("event %s: command still references Python send_event.py: %s", event, cmd)
		}
		if strings.Contains(cmd, "python3") {
			t.Errorf("event %s: command still references python3: %s", event, cmd)
		}
		if !strings.Contains(cmd, "--source-app test-app") {
			t.Errorf("event %s: command missing source-app: %s", event, cmd)
		}
		if !strings.Contains(cmd, "--server-url http://localhost:4100/v1/events") {
			t.Errorf("event %s: command missing server-url: %s", event, cmd)
		}
	}
}

func TestInitDefaultHookCommandResolvesExecutable(t *testing.T) {
	// When HookCommand is empty, Init should default to
	// ``<os.Executable> hook``. For the go test binary this is the test
	// executable path — we just assert the suffix is ``hook`` and the
	// prefix is non-empty.
	cfg := newTestConfig(t)
	cfg.HookCommand = ""
	result, err := Init(cfg)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	if result.HookCommand == "" {
		t.Fatal("HookCommand should default to <os.Executable> hook")
	}
	if !strings.HasSuffix(result.HookCommand, " hook") {
		t.Errorf("default HookCommand should end with ' hook', got %q", result.HookCommand)
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
	cmd := commandForEvent(t, settings, "PreToolUse")
	if !strings.Contains(cmd, "--source-app renamed-app") {
		t.Errorf("command not updated with new source-app: %s", cmd)
	}
	if strings.Contains(cmd, "--source-app test-app") {
		t.Errorf("command still references old source-app: %s", cmd)
	}
}

func TestInitLegacyPythonDetectionHintsForce(t *testing.T) {
	cfg := newTestConfig(t)
	if err := os.MkdirAll(cfg.Target, 0o755); err != nil {
		t.Fatal(err)
	}
	// Seed a v0.1.x-style Python hook entry.
	preset := map[string]any{
		"hooks": map[string]any{
			"PreToolUse": []any{
				map[string]any{
					"hooks": []any{
						map[string]any{
							"type":    "command",
							"command": "python3 /tmp/hooks/send_event.py --event-type PreToolUse",
						},
					},
				},
			},
		},
	}
	b, _ := json.MarshalIndent(preset, "", "  ")
	if err := os.WriteFile(cfg.SettingsPath(), b, 0o644); err != nil {
		t.Fatal(err)
	}
	result, err := Init(cfg)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	if result.LegacyFound == 0 {
		t.Errorf("expected LegacyFound > 0, got 0")
	}
	// Without --force the legacy entry survives and the new entry is
	// appended alongside it.
	settings := readSettings(t, cfg.SettingsPath())
	entries := settings["hooks"].(map[string]any)["PreToolUse"].([]any)
	if len(entries) != 2 {
		t.Errorf("expected legacy + new entry, got %d", len(entries))
	}
}

func TestInitForceStripsLegacyPythonEntries(t *testing.T) {
	cfg := newTestConfig(t)
	if err := os.MkdirAll(cfg.Target, 0o755); err != nil {
		t.Fatal(err)
	}
	preset := map[string]any{
		"hooks": map[string]any{
			"PreToolUse": []any{
				map[string]any{
					"hooks": []any{
						map[string]any{
							"type":    "command",
							"command": "python3 /tmp/hooks/send_event.py --event-type PreToolUse",
						},
					},
				},
			},
		},
	}
	b, _ := json.MarshalIndent(preset, "", "  ")
	if err := os.WriteFile(cfg.SettingsPath(), b, 0o644); err != nil {
		t.Fatal(err)
	}
	cfg.Force = true
	if _, err := Init(cfg); err != nil {
		t.Fatalf("Init force: %v", err)
	}
	settings := readSettings(t, cfg.SettingsPath())
	entries := settings["hooks"].(map[string]any)["PreToolUse"].([]any)
	if len(entries) != 1 {
		t.Fatalf("expected exactly 1 entry after force (legacy stripped), got %d", len(entries))
	}
	cmd := commandForEvent(t, settings, "PreToolUse")
	if strings.Contains(cmd, "python3") {
		t.Errorf("force run should strip python3 entries; got %s", cmd)
	}
	if !strings.HasPrefix(cmd, testHookCommand+" ") {
		t.Errorf("force run did not install new hook command; got %s", cmd)
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
	got := deriveSourceAppFromTarget("/tmp/my-project/.claude")
	if got != "my-project" {
		t.Errorf("deriveSourceAppFromTarget: got %q, want %q", got, "my-project")
	}
}

func TestInitEmptySourceAppOmitsFlag(t *testing.T) {
	cfg := newTestConfig(t)
	cfg.SourceApp = "" // dynamic — apogee hook derives at runtime
	result, err := Init(cfg)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	if result.SourceApp != "" {
		t.Errorf("expected empty SourceApp on result, got %q", result.SourceApp)
	}

	settings := readSettings(t, cfg.SettingsPath())
	hooksRaw, ok := settings["hooks"].(map[string]any)
	if !ok {
		t.Fatalf("hooks section missing: %T", settings["hooks"])
	}
	for _, event := range HookEvents {
		entries := hooksRaw[event].([]any)
		cmd := entries[0].(map[string]any)["hooks"].([]any)[0].(map[string]any)["command"].(string)
		if strings.Contains(cmd, "--source-app") {
			t.Errorf("event %s: command should not contain --source-app when dynamic: %s", event, cmd)
		}
		if !strings.Contains(cmd, "--event "+event) {
			t.Errorf("event %s: command missing --event: %s", event, cmd)
		}
	}
}

func TestInitPinnedSourceAppWritesFlag(t *testing.T) {
	cfg := newTestConfig(t)
	cfg.SourceApp = "pinned-name"
	if _, err := Init(cfg); err != nil {
		t.Fatalf("Init: %v", err)
	}
	settings := readSettings(t, cfg.SettingsPath())
	cmd := commandForEvent(t, settings, "PreToolUse")
	if !strings.Contains(cmd, "--source-app pinned-name") {
		t.Errorf("pinned SourceApp missing from command: %s", cmd)
	}
}

func TestInitDynamicAndPinnedCoexistOnForce(t *testing.T) {
	// First install dynamic; then force-install a pinned label — the old
	// dynamic command should be replaced by the new pinned one.
	cfg := newTestConfig(t)
	cfg.SourceApp = ""
	if _, err := Init(cfg); err != nil {
		t.Fatalf("first init: %v", err)
	}
	cfg.SourceApp = "pinned-after"
	cfg.Force = true
	if _, err := Init(cfg); err != nil {
		t.Fatalf("force init: %v", err)
	}
	settings := readSettings(t, cfg.SettingsPath())
	entries := settings["hooks"].(map[string]any)["PreToolUse"].([]any)
	if len(entries) != 1 {
		t.Fatalf("expected exactly 1 entry after force, got %d", len(entries))
	}
	cmd := entries[0].(map[string]any)["hooks"].([]any)[0].(map[string]any)["command"].(string)
	if !strings.Contains(cmd, "--source-app pinned-after") {
		t.Errorf("force did not update source-app: %s", cmd)
	}
}

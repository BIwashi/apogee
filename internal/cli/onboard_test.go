package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/BIwashi/apogee/internal/daemon"
	"github.com/BIwashi/apogee/internal/summarizer"
)

// fakePreferencesWriter captures every UpsertPreference call so
// onboard tests can assert on the exact set of keys + values the
// wizard emits without touching a real DuckDB store.
type fakePreferencesWriter struct {
	calls map[string]any
}

func newFakePreferencesWriter() *fakePreferencesWriter {
	return &fakePreferencesWriter{calls: map[string]any{}}
}

func (f *fakePreferencesWriter) UpsertPreference(_ context.Context, key string, value any) error {
	f.calls[key] = value
	return nil
}

// onboardTestHarness bundles the injectable seams + captured output
// so each test sets up once and asserts with short accessors.
type onboardTestHarness struct {
	t           *testing.T
	tempHome    string
	opts        onboardOptions
	fakeMgr     *fakeManager
	prefsWriter *fakePreferencesWriter
	loadedPrefs summarizer.Preferences
	browserURL  string
	stdout      *bytes.Buffer
	stderr      *bytes.Buffer
	startCalled int
}

func newOnboardHarness(t *testing.T) *onboardTestHarness {
	t.Helper()
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("APOGEE_ONBOARD_NONINTERACTIVE", "")

	fm := newFakeManager()
	writer := newFakePreferencesWriter()

	h := &onboardTestHarness{
		t:           t,
		tempHome:    tempHome,
		fakeMgr:     fm,
		prefsWriter: writer,
		stdout:      &bytes.Buffer{},
		stderr:      &bytes.Buffer{},
	}

	h.opts = onboardOptions{
		ConfigPath: filepath.Join(tempHome, ".apogee", "config.toml"),
		DBPath:     filepath.Join(tempHome, ".apogee", "apogee.duckdb"),
		Addr:       daemon.DefaultAddr,
		Stdin:      bytes.NewReader(nil), // not a *os.File → non-interactive
		Stdout:     h.stdout,
		Stderr:     h.stderr,
		ManagerFactory: func() (daemon.Manager, error) {
			return fm, nil
		},
		LoadPrefs: func(_ context.Context, _ string) (summarizer.Preferences, error) {
			return h.loadedPrefs, nil
		},
		WritePrefs: func(ctx context.Context, _ string, prefs summarizer.Preferences) error {
			return writeSummarizerPreferences(ctx, writer, prefs)
		},
		StartDaemon: func(_ context.Context, _ daemon.Manager) error {
			h.startCalled++
			return nil
		},
		OpenBrowser: func(_ context.Context, _ io.Writer, url string) error {
			h.browserURL = url
			return nil
		},
	}
	return h
}

func (h *onboardTestHarness) run() error {
	h.t.Helper()
	return runOnboard(context.Background(), h.opts)
}

// TestOnboard_PlanFromCurrentState_FreshInstall verifies the plan
// defaults match the "nothing installed yet" state: install hooks,
// install daemon, English summarizer, empty prompts, telemetry off.
func TestOnboard_PlanFromCurrentState_FreshInstall(t *testing.T) {
	h := newOnboardHarness(t)
	h.loadedPrefs = summarizer.Defaults()

	// No settings.json, no daemon installed, no config.toml — the
	// fakeManager starts with installed=false, so the plan should
	// propose installing everything.
	fillDefaults(&h.opts)
	state, err := loadOnboardState(t.Context(), h.opts)
	if err != nil {
		t.Fatalf("loadOnboardState: %v", err)
	}
	plan := toPlanDefaults(h.opts, state)

	if plan.HooksAction != "install" {
		t.Errorf("fresh plan hooks action = %q, want install", plan.HooksAction)
	}
	if plan.DaemonAction != "install" {
		t.Errorf("fresh plan daemon action = %q, want install", plan.DaemonAction)
	}
	if plan.SummarizerLanguage != summarizer.LanguageEN {
		t.Errorf("fresh plan language = %q, want en", plan.SummarizerLanguage)
	}
	if plan.RecapSystemPrompt != "" {
		t.Errorf("fresh plan recap prompt should be empty, got %q", plan.RecapSystemPrompt)
	}
	if plan.TelemetryEnabled {
		t.Errorf("fresh plan should have telemetry disabled")
	}
	if !plan.StartDaemon {
		t.Errorf("fresh plan should start daemon after install")
	}
}

// TestOnboard_PlanFromCurrentState_Reinstall verifies the plan
// defaults for a machine that already has apogee wired: hooks
// installed, daemon installed + running, JA language + custom
// prompt. The plan should propose re-install paths and preserve
// the loaded values.
func TestOnboard_PlanFromCurrentState_Reinstall(t *testing.T) {
	h := newOnboardHarness(t)
	h.fakeMgr.installed = true
	h.fakeMgr.running = true
	h.fakeMgr.pid = 7777
	h.loadedPrefs = summarizer.Preferences{
		Language:          summarizer.LanguageJA,
		RecapSystemPrompt: "日本語でお願いします",
	}

	// Pre-populate ~/.claude/settings.json with an apogee hook so
	// loadOnboardState sees HooksInstalled=true.
	claudeDir := filepath.Join(h.tempHome, ".claude")
	if err := os.MkdirAll(claudeDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	settings := map[string]any{
		"hooks": map[string]any{
			"SessionStart": []any{
				map[string]any{
					"hooks": []any{
						map[string]any{
							"type":    "command",
							"command": "/usr/local/bin/apogee hook --event SessionStart",
						},
					},
				},
			},
		},
	}
	b, _ := json.Marshal(settings)
	if err := os.WriteFile(filepath.Join(claudeDir, "settings.json"), b, 0o644); err != nil {
		t.Fatalf("write settings: %v", err)
	}

	fillDefaults(&h.opts)
	state, err := loadOnboardState(t.Context(), h.opts)
	if err != nil {
		t.Fatalf("loadOnboardState: %v", err)
	}
	plan := toPlanDefaults(h.opts, state)

	if plan.HooksAction != "reinstall" {
		t.Errorf("reinstall plan hooks action = %q, want reinstall", plan.HooksAction)
	}
	if plan.DaemonAction != "reinstall" {
		t.Errorf("reinstall plan daemon action = %q, want reinstall", plan.DaemonAction)
	}
	if plan.SummarizerLanguage != summarizer.LanguageJA {
		t.Errorf("reinstall plan language = %q, want ja", plan.SummarizerLanguage)
	}
	if plan.RecapSystemPrompt != "日本語でお願いします" {
		t.Errorf("reinstall plan prompt = %q, want preserved", plan.RecapSystemPrompt)
	}
	// Daemon already running → StartDaemon should default to false.
	if plan.StartDaemon {
		t.Errorf("reinstall plan should not re-start a running daemon")
	}
}

// TestOnboard_NonInteractiveSkipsPrompts verifies --yes drives the
// full wizard end-to-end: every step runs without prompting.
func TestOnboard_NonInteractiveSkipsPrompts(t *testing.T) {
	h := newOnboardHarness(t)
	h.opts.Yes = true

	if err := h.run(); err != nil {
		t.Fatalf("run onboard --yes: %v", err)
	}
	if h.fakeMgr.installCalls != 1 {
		t.Errorf("expected 1 daemon install, got %d", h.fakeMgr.installCalls)
	}
	if h.startCalled != 1 {
		t.Errorf("expected daemon start to be called, got %d", h.startCalled)
	}
	// Summarizer language always writes in fresh install.
	if got := h.prefsWriter.calls[summarizer.PrefKeyLanguage]; got != summarizer.LanguageEN {
		t.Errorf("expected language=en, got %v", got)
	}
	// Config TOML was written.
	if _, err := os.Stat(h.opts.ConfigPath); err != nil {
		t.Errorf("expected config.toml to exist at %s: %v", h.opts.ConfigPath, err)
	}
	// Hooks settings.json was written.
	hooksPath := filepath.Join(h.tempHome, ".claude", "settings.json")
	if _, err := os.Stat(hooksPath); err != nil {
		t.Errorf("expected settings.json to exist at %s: %v", hooksPath, err)
	}
	// In --yes mode we must NOT open the browser.
	if h.browserURL != "" {
		t.Errorf("--yes should not open browser, got %q", h.browserURL)
	}
	out := h.stdout.String()
	if !strings.Contains(out, "apogee is ready") {
		t.Errorf("expected ready banner in output: %s", out)
	}
}

// TestOnboard_DryRunMakesNoChanges verifies --dry-run prints the
// plan and leaves every side channel untouched.
func TestOnboard_DryRunMakesNoChanges(t *testing.T) {
	h := newOnboardHarness(t)
	h.opts.DryRun = true

	if err := h.run(); err != nil {
		t.Fatalf("run dry-run: %v", err)
	}
	if h.fakeMgr.installCalls != 0 {
		t.Errorf("dry-run should not install daemon, got %d calls", h.fakeMgr.installCalls)
	}
	if h.startCalled != 0 {
		t.Errorf("dry-run should not start daemon")
	}
	if len(h.prefsWriter.calls) != 0 {
		t.Errorf("dry-run should not write preferences, got %v", h.prefsWriter.calls)
	}
	if _, err := os.Stat(h.opts.ConfigPath); !os.IsNotExist(err) {
		t.Errorf("dry-run should not create config.toml, stat err = %v", err)
	}
	out := h.stdout.String()
	if !strings.Contains(out, "apogee onboard — plan") {
		t.Errorf("expected plan heading in dry-run output: %s", out)
	}
	if !strings.Contains(out, "Run without --dry-run to apply.") {
		t.Errorf("expected dry-run footer: %s", out)
	}
}

// TestOnboard_SkipFlags verifies each --skip-* flag removes its
// step from the applied plan. We exercise all four in one run to
// check they compose cleanly.
func TestOnboard_SkipFlags(t *testing.T) {
	h := newOnboardHarness(t)
	h.opts.Yes = true
	h.opts.SkipHooks = true
	h.opts.SkipDaemon = true
	h.opts.SkipSummarizer = true
	h.opts.SkipTelemetry = true

	if err := h.run(); err != nil {
		t.Fatalf("run with skips: %v", err)
	}
	if h.fakeMgr.installCalls != 0 {
		t.Errorf("--skip-daemon should skip daemon install, got %d calls", h.fakeMgr.installCalls)
	}
	if h.startCalled != 0 {
		t.Errorf("--skip-daemon should skip daemon start")
	}
	if len(h.prefsWriter.calls) != 0 {
		t.Errorf("--skip-summarizer should skip pref writes, got %v", h.prefsWriter.calls)
	}
	hooksPath := filepath.Join(h.tempHome, ".claude", "settings.json")
	if _, err := os.Stat(hooksPath); err == nil {
		t.Errorf("--skip-hooks should not create settings.json at %s", hooksPath)
	}
	out := h.stdout.String()
	for _, want := range []string{"hooks: skipped", "daemon: skipped", "summarizer: skipped"} {
		if !strings.Contains(out, want) {
			t.Errorf("expected %q in output: %s", want, out)
		}
	}
}

// TestOnboard_SkipFlagsIndividual exercises each --skip-* flag in
// isolation to catch the "one flag breaks another step" class of
// bug.
func TestOnboard_SkipFlagsIndividual(t *testing.T) {
	cases := []struct {
		name string
		tune func(opts *onboardOptions)
		want func(t *testing.T, h *onboardTestHarness)
	}{
		{
			name: "skip-hooks",
			tune: func(opts *onboardOptions) { opts.SkipHooks = true },
			want: func(t *testing.T, h *onboardTestHarness) {
				hooksPath := filepath.Join(h.tempHome, ".claude", "settings.json")
				if _, err := os.Stat(hooksPath); err == nil {
					t.Errorf("settings.json should not exist")
				}
				if h.fakeMgr.installCalls != 1 {
					t.Errorf("daemon install should still run, got %d", h.fakeMgr.installCalls)
				}
			},
		},
		{
			name: "skip-daemon",
			tune: func(opts *onboardOptions) { opts.SkipDaemon = true },
			want: func(t *testing.T, h *onboardTestHarness) {
				if h.fakeMgr.installCalls != 0 {
					t.Errorf("daemon install should skip, got %d", h.fakeMgr.installCalls)
				}
				hooksPath := filepath.Join(h.tempHome, ".claude", "settings.json")
				if _, err := os.Stat(hooksPath); err != nil {
					t.Errorf("settings.json should still exist: %v", err)
				}
			},
		},
		{
			name: "skip-summarizer",
			tune: func(opts *onboardOptions) { opts.SkipSummarizer = true },
			want: func(t *testing.T, h *onboardTestHarness) {
				if len(h.prefsWriter.calls) != 0 {
					t.Errorf("prefs writer should be empty, got %v", h.prefsWriter.calls)
				}
			},
		},
		{
			name: "skip-telemetry",
			tune: func(opts *onboardOptions) { opts.SkipTelemetry = true },
			want: func(t *testing.T, h *onboardTestHarness) {
				// Config still written (daemon block), but the
				// telemetry block must be absent.
				raw, err := os.ReadFile(h.opts.ConfigPath)
				if err != nil {
					t.Fatalf("read config: %v", err)
				}
				if strings.Contains(string(raw), "[telemetry]") {
					t.Errorf("--skip-telemetry should omit [telemetry] block: %s", raw)
				}
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newOnboardHarness(t)
			h.opts.Yes = true
			tc.tune(&h.opts)
			if err := h.run(); err != nil {
				t.Fatalf("run: %v", err)
			}
			tc.want(t, h)
		})
	}
}

// TestOnboard_NonInteractive_PreservesExistingPrompts guards the
// "--yes must not blow away a non-empty existing system prompt"
// invariant documented in the PR brief.
func TestOnboard_NonInteractive_PreservesExistingPrompts(t *testing.T) {
	h := newOnboardHarness(t)
	h.opts.Yes = true
	h.loadedPrefs = summarizer.Preferences{
		Language:          summarizer.LanguageJA,
		RecapSystemPrompt: "existing prompt, do not overwrite",
	}

	if err := h.run(); err != nil {
		t.Fatalf("run: %v", err)
	}
	got, ok := h.prefsWriter.calls[summarizer.PrefKeyRecapSystemPrompt]
	if !ok {
		t.Fatalf("recap_system_prompt was never written")
	}
	if s, _ := got.(string); s != "existing prompt, do not overwrite" {
		t.Errorf("recap prompt = %q, want preserved original", got)
	}
	if got := h.prefsWriter.calls[summarizer.PrefKeyLanguage]; got != summarizer.LanguageJA {
		t.Errorf("language = %v, want ja", got)
	}
}

// TestOnboard_Root wires the subcommand into the root cobra tree and
// verifies `apogee --help` lists it. This catches the "forgot to
// AddCommand" class of regression.
func TestOnboard_Root(t *testing.T) {
	var buf bytes.Buffer
	root := NewRootCmd(&buf, &buf)
	root.SetArgs([]string{"--help"})
	if err := root.Execute(); err != nil {
		t.Fatalf("help: %v", err)
	}
	if !strings.Contains(buf.String(), "onboard") {
		t.Errorf("expected onboard in help output: %s", buf.String())
	}
}

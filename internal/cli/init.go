package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// HookEvents is the canonical ordering of hook events apogee cares about.
// The slice is iterated when writing the init plan and when editing
// settings.json, so the list ordering is stable across runs.
var HookEvents = []string{
	"SessionStart",
	"SessionEnd",
	"UserPromptSubmit",
	"PreToolUse",
	"PostToolUse",
	"PostToolUseFailure",
	"PermissionRequest",
	"Notification",
	"SubagentStart",
	"SubagentStop",
	"Stop",
	"PreCompact",
}

// Scope selects the install destination.
type Scope string

const (
	ScopeProject Scope = "project"
	ScopeUser    Scope = "user"
)

// DefaultServerURL is the collector endpoint used when --server-url is not
// passed explicitly.
const DefaultServerURL = "http://localhost:4100/v1/events"

// InitConfig is the fully-resolved configuration for an `apogee init` run.
// Callers can either build it by hand (tests do this) or let RunInit parse
// CLI flags into it.
type InitConfig struct {
	// Target is the resolved path to the `.claude` directory (e.g.
	// ``/path/to/project/.claude``). InitConfig.SettingsPath returns the
	// corresponding ``settings.json``.
	Target string
	// SourceApp, when set, pins the label stamped onto every event.
	// Leave empty to let ``send_event.py`` derive the label dynamically
	// at hook invocation time from $APOGEE_SOURCE_APP, the git toplevel
	// basename, or $PWD basename — in that order. Dynamic derivation is
	// the default so one user-scope install can observe every project
	// on the machine with the right per-project label.
	SourceApp string
	// ServerURL is the collector endpoint.
	ServerURL string
	// Scope selects project vs. user install.
	Scope Scope
	// HooksDir is the directory `send_event.py` lives in. When empty, RunInit
	// extracts the embedded hooks to ``~/.apogee/hooks/<version>/``.
	HooksDir string
	// DryRun skips the actual write and prints the plan instead.
	DryRun bool
	// Force overwrites existing apogee hook entries without prompting.
	Force bool
}

// SettingsPath returns the absolute path to settings.json for this config.
func (c InitConfig) SettingsPath() string {
	return filepath.Join(c.Target, "settings.json")
}

// InitResult captures the outcome of an init run for display / assertions.
type InitResult struct {
	SettingsPath string
	// SourceApp is the label that will be stamped on events. When empty,
	// the commands written to settings.json omit the ``--source-app``
	// flag and ``send_event.py`` derives it at runtime.
	SourceApp string
	HooksDir  string
	ServerURL string
	Added     []string
	Skipped   []string
	Settings  map[string]any
}

// RunInit is the `apogee init` entry point. It parses flags, resolves paths,
// optionally extracts the embedded hooks, rewrites settings.json, and prints
// a plan.
func RunInit(args []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("init", flag.ContinueOnError)
	flags.SetOutput(stderr)

	target := flags.String("target", "", "Claude Code settings directory. Defaults to ~/.claude (user scope) or ./.claude (project scope).")
	sourceApp := flags.String("source-app", "", "Pin the source_app label. Leave empty (the default) to let the hook derive it at runtime from $APOGEE_SOURCE_APP, git toplevel, or $PWD.")
	serverURL := flags.String("server-url", DefaultServerURL, "Collector URL")
	scope := flags.String("scope", "user", "Install scope: user | project. Defaults to user so one install covers every Claude Code project on this machine.")
	hooksDir := flags.String("hooks-dir", "", "Directory containing send_event.py (default: extract embedded hooks to ~/.apogee/hooks/<version>/)")
	dryRun := flags.Bool("dry-run", false, "Print what would change without writing")
	force := flags.Bool("force", false, "Overwrite existing apogee hooks without prompting")

	flags.Usage = func() {
		fmt.Fprintln(stderr, "apogee init — install apogee hook entries into .claude/settings.json")
		fmt.Fprintln(stderr)
		fmt.Fprintln(stderr, "Default install: user scope (~/.claude/settings.json) with dynamic")
		fmt.Fprintln(stderr, "source_app resolution. One install covers every Claude Code session on")
		fmt.Fprintln(stderr, "the machine, and each event is labelled with the repo name of the")
		fmt.Fprintln(stderr, "directory the session was started from.")
		fmt.Fprintln(stderr)
		fmt.Fprintln(stderr, "send_event.py lookup order:")
		fmt.Fprintln(stderr, "  1. --hooks-dir <dir>/send_event.py")
		fmt.Fprintln(stderr, "  2. $APOGEE_HOOKS_DIR/send_event.py")
		fmt.Fprintln(stderr, "  3. extract the embedded copy to ~/.apogee/hooks/<version>/")
		fmt.Fprintln(stderr)
		fmt.Fprintln(stderr, "Flags:")
		flags.PrintDefaults()
	}
	if err := flags.Parse(args); err != nil {
		return err
	}

	cfg := InitConfig{
		SourceApp: *sourceApp,
		ServerURL: *serverURL,
		HooksDir:  *hooksDir,
		DryRun:    *dryRun,
		Force:     *force,
	}

	switch *scope {
	case "user", "":
		cfg.Scope = ScopeUser
	case "project":
		cfg.Scope = ScopeProject
	default:
		return fmt.Errorf("init: invalid --scope %q (expected 'user' or 'project')", *scope)
	}

	resolvedTarget, err := ResolveTarget(*target, cfg.Scope)
	if err != nil {
		return err
	}
	cfg.Target = resolvedTarget

	// SourceApp stays empty by default — the Python hook derives it at
	// runtime so one install can span every project on this machine.

	if cfg.HooksDir == "" {
		cfg.HooksDir = os.Getenv("APOGEE_HOOKS_DIR")
	}
	if cfg.HooksDir == "" {
		d, err := DefaultHooksDir()
		if err != nil {
			return err
		}
		cfg.HooksDir = d
	}
	expandedHooksDir, err := expandHome(cfg.HooksDir)
	if err != nil {
		return err
	}
	cfg.HooksDir = expandedHooksDir

	warnIfMissingPython(stderr)

	result, err := Init(cfg)
	if err != nil {
		return err
	}
	printInitPlan(stdout, cfg, result)
	return nil
}

// Init performs the init logic against an already-resolved InitConfig. It is
// the unit-testable entry point.
func Init(cfg InitConfig) (*InitResult, error) {
	if cfg.Target == "" {
		return nil, errors.New("init: target is required")
	}
	if cfg.ServerURL == "" {
		cfg.ServerURL = DefaultServerURL
	}

	// Make sure send_event.py is available at cfg.HooksDir. When it is not,
	// we extract the embedded copy. In --dry-run we still extract so the
	// path reported in the plan is truthful (we always extract to the same
	// versioned path, so the cost is bounded and idempotent).
	sendEvent := filepath.Join(cfg.HooksDir, "send_event.py")
	if _, err := os.Stat(sendEvent); err != nil {
		if _, err := ExtractHooks(cfg.HooksDir, false); err != nil {
			return nil, fmt.Errorf("init: extract hooks: %w", err)
		}
	}

	// Load the existing settings.json if present.
	settingsPath := cfg.SettingsPath()
	settings := map[string]any{}
	if data, err := os.ReadFile(settingsPath); err == nil {
		if err := json.Unmarshal(data, &settings); err != nil {
			return nil, fmt.Errorf("init: parse %s: %w", settingsPath, err)
		}
	} else if !os.IsNotExist(err) {
		return nil, fmt.Errorf("init: read %s: %w", settingsPath, err)
	}

	hooksSection, err := hooksSectionOf(settings)
	if err != nil {
		return nil, err
	}

	result := &InitResult{
		SettingsPath: settingsPath,
		SourceApp:    cfg.SourceApp,
		HooksDir:     cfg.HooksDir,
		ServerURL:    cfg.ServerURL,
	}

	commandPrefix := fmt.Sprintf("python3 %s", sendEvent)
	for _, event := range HookEvents {
		entries := listOf(hooksSection[event])
		already := hasApogeeEntry(entries, commandPrefix)
		if already && !cfg.Force {
			result.Skipped = append(result.Skipped, event)
			continue
		}
		if cfg.Force {
			entries = removeApogeeEntries(entries, commandPrefix)
		}
		parts := []string{
			commandPrefix,
			"--event-type", event,
			"--server-url", cfg.ServerURL,
		}
		if cfg.SourceApp != "" {
			// Pinning --source-app overrides the hook's runtime
			// derivation, matching the user's explicit intent.
			parts = append(parts, "--source-app", cfg.SourceApp)
		}
		command := strings.Join(parts, " ")
		entries = append(entries, map[string]any{
			"hooks": []any{
				map[string]any{
					"type":    "command",
					"command": command,
				},
			},
		})
		hooksSection[event] = entries
		result.Added = append(result.Added, event)
	}

	settings["hooks"] = hooksSection
	result.Settings = settings

	if cfg.DryRun {
		return result, nil
	}

	// Serialise with stable key ordering (json.Marshal sorts map keys).
	serialised, err := marshalStable(settings)
	if err != nil {
		return nil, fmt.Errorf("init: marshal settings: %w", err)
	}

	if err := writeFileAtomic(settingsPath, serialised, 0o644); err != nil {
		return nil, fmt.Errorf("init: write %s: %w", settingsPath, err)
	}
	return result, nil
}

// ResolveTarget expands a target path according to the scope. For the user
// scope a non-default --target is still honoured so that tests and advanced
// users can override it. The returned path is absolute.
func ResolveTarget(target string, scope Scope) (string, error) {
	if scope == ScopeUser && (target == "" || target == "./.claude") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(home, ".claude"), nil
	}
	if target == "" {
		target = "./.claude"
	}
	expanded, err := expandHome(target)
	if err != nil {
		return "", err
	}
	abs, err := filepath.Abs(expanded)
	if err != nil {
		return "", err
	}
	return abs, nil
}

// hooksSectionOf safely extracts the ``hooks`` sub-object from a parsed
// settings.json, creating it if absent.
func hooksSectionOf(settings map[string]any) (map[string]any, error) {
	raw, ok := settings["hooks"]
	if !ok || raw == nil {
		section := map[string]any{}
		return section, nil
	}
	section, ok := raw.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("init: existing 'hooks' is %T, expected object", raw)
	}
	return section, nil
}

// listOf converts ``hooksSection[event]`` into a concrete slice. Missing or
// wrong-typed entries become an empty slice.
func listOf(raw any) []any {
	if raw == nil {
		return nil
	}
	slice, ok := raw.([]any)
	if !ok {
		return nil
	}
	return slice
}

// hasApogeeEntry returns true if any hook under ``entries`` has a command
// that starts with prefix.
func hasApogeeEntry(entries []any, prefix string) bool {
	for _, entry := range entries {
		m, ok := entry.(map[string]any)
		if !ok {
			continue
		}
		inner, ok := m["hooks"].([]any)
		if !ok {
			continue
		}
		for _, h := range inner {
			hm, ok := h.(map[string]any)
			if !ok {
				continue
			}
			cmd, ok := hm["command"].(string)
			if !ok {
				continue
			}
			if strings.HasPrefix(cmd, prefix) {
				return true
			}
		}
	}
	return false
}

// removeApogeeEntries strips every hook command that starts with prefix,
// dropping any hook groups that become empty as a result. Non-apogee entries
// are preserved.
func removeApogeeEntries(entries []any, prefix string) []any {
	out := make([]any, 0, len(entries))
	for _, entry := range entries {
		m, ok := entry.(map[string]any)
		if !ok {
			out = append(out, entry)
			continue
		}
		inner, ok := m["hooks"].([]any)
		if !ok {
			out = append(out, entry)
			continue
		}
		kept := make([]any, 0, len(inner))
		for _, h := range inner {
			hm, hok := h.(map[string]any)
			if !hok {
				kept = append(kept, h)
				continue
			}
			cmd, cok := hm["command"].(string)
			if cok && strings.HasPrefix(cmd, prefix) {
				continue
			}
			kept = append(kept, h)
		}
		if len(kept) == 0 {
			continue
		}
		// Preserve all other fields on the outer entry.
		copyEntry := map[string]any{}
		for k, v := range m {
			copyEntry[k] = v
		}
		copyEntry["hooks"] = kept
		out = append(out, copyEntry)
	}
	return out
}

// marshalStable serialises data with indent=2 and ensures map keys are
// sorted deterministically (encoding/json already sorts them but we wrap it
// here so callers do not have to remember).
func marshalStable(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// deriveSourceApp converts a ``.claude`` directory path into a plausible
// source_app label: the basename of the parent directory, or ``apogee`` as a
// last resort.
//
// Kept for backward compatibility and explicit overrides (``apogee init
// --source-app $(apogee derive)`` style workflows). The default init flow
// leaves ``SourceApp`` empty so the Python hook derives it at runtime; this
// helper is only invoked by callers that want to pin a label based on the
// current target directory.
func deriveSourceApp(target string) string {
	parent := filepath.Dir(target)
	if parent == "" || parent == "/" || parent == "." {
		return "apogee"
	}
	base := filepath.Base(parent)
	if base == "" || base == "." || base == "/" {
		return "apogee"
	}
	return sanitiseLabel(base)
}

func sanitiseLabel(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return "apogee"
	}
	return s
}

// warnIfMissingPython runs ``python3 --version`` and prints a soft warning
// to stderr if it fails. We do not abort the init — the user may have a
// different Python binary in PATH at Claude Code runtime.
func warnIfMissingPython(stderr io.Writer) {
	cmd := exec.Command("python3", "--version")
	if err := cmd.Run(); err != nil {
		fmt.Fprintln(stderr, "apogee init: warning: `python3` not found in PATH; hooks will fail at runtime until you install Python 3.")
	}
}

// printInitPlan prints a human-readable summary of an InitResult in the
// diff-style format documented in the PR description.
func printInitPlan(w io.Writer, cfg InitConfig, r *InitResult) {
	fmt.Fprintln(w, "apogee init — plan")
	fmt.Fprintln(w)
	fmt.Fprintf(w, "  Target: %s (%s scope)\n", r.SettingsPath, cfg.Scope)
	if r.SourceApp != "" {
		fmt.Fprintf(w, "  Source app: %s (pinned)\n", r.SourceApp)
	} else {
		fmt.Fprintln(w, "  Source app: auto — derived by send_event.py at runtime")
		fmt.Fprintln(w, "              ($APOGEE_SOURCE_APP → git toplevel → $PWD)")
	}
	fmt.Fprintf(w, "  Hooks dir: %s\n", r.HooksDir)
	fmt.Fprintln(w)

	fmt.Fprintln(w, "  Hook events to install:")
	added := append([]string{}, r.Added...)
	sort.Strings(added)
	for _, e := range added {
		fmt.Fprintf(w, "    + %s\n", e)
	}
	if len(r.Skipped) > 0 {
		fmt.Fprintln(w)
		fmt.Fprintln(w, "  Hook events skipped (already installed; pass --force to replace):")
		skipped := append([]string{}, r.Skipped...)
		sort.Strings(skipped)
		for _, e := range skipped {
			fmt.Fprintf(w, "    = %s\n", e)
		}
	}
	fmt.Fprintln(w)
	fmt.Fprintf(w, "  Collector URL: %s\n", r.ServerURL)
	fmt.Fprintln(w)

	if cfg.DryRun {
		fmt.Fprintln(w, "Run without --dry-run to apply.")
		return
	}
	fmt.Fprintf(w, "apogee init: added %d, skipped %d, wrote %s\n",
		len(r.Added), len(r.Skipped), r.SettingsPath)
}

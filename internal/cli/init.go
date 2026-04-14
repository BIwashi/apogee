package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
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
	// Leave empty to let `apogee hook` derive the label dynamically at
	// hook invocation time from $APOGEE_SOURCE_APP, the git toplevel
	// basename, or $PWD basename — in that order. Dynamic derivation is
	// the default so one user-scope install can observe every project
	// on the machine with the right per-project label.
	SourceApp string
	// ServerURL is the collector endpoint.
	ServerURL string
	// Scope selects project vs. user install.
	Scope Scope
	// HookCommand is the literal prefix (binary path + "hook" subcommand)
	// written into settings.json. Defaults to ``<os.Executable> hook`` so
	// Claude Code re-invokes the exact apogee binary that ran init. Tests
	// inject a stable path so assertions do not depend on the real
	// executable location.
	HookCommand string
	// DryRun skips the actual write and prints the plan instead.
	DryRun bool
	// Force overwrites existing apogee hook entries without prompting.
	Force bool
}

// DefaultHookCommand is the default command prefix written into
// settings.json. It is the currently-running apogee binary's absolute
// path followed by ``hook``. When os.Executable() is not resolvable
// (unusual), the function falls back to the literal ``apogee hook``
// so the generated settings.json still has a working command once the
// binary is on PATH.
func DefaultHookCommand() string {
	exe, err := os.Executable()
	if err != nil || exe == "" {
		return "apogee hook"
	}
	if abs, err := filepath.Abs(exe); err == nil && abs != "" {
		exe = abs
	}
	return exe + " hook"
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
	// flag and `apogee hook` derives it at runtime.
	SourceApp string
	// HookCommand is the resolved command prefix written into
	// settings.json (typically ``<binary> hook``).
	HookCommand string
	ServerURL   string
	Added       []string
	Skipped     []string
	// LegacyFound is the count of existing settings.json entries that
	// point at the old ``python3 send_event.py`` prefix. These are NOT
	// auto-migrated; the plan output hints at ``--force`` to replace them.
	LegacyFound int
	Settings    map[string]any
}

// RunInit is the `apogee init` entry point. It parses flags, resolves
// paths, rewrites settings.json, and prints a plan. It no longer has any
// Python or hook-extraction side effects — the binary itself is the
// hook, so the only output is the edited settings.json.
func RunInit(args []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("init", flag.ContinueOnError)
	flags.SetOutput(stderr)

	target := flags.String("target", "", "Claude Code settings directory. Defaults to ~/.claude (user scope) or ./.claude (project scope).")
	sourceApp := flags.String("source-app", "", "Pin the source_app label. Leave empty (the default) to let the hook derive it at runtime from $APOGEE_SOURCE_APP, git toplevel, or $PWD.")
	serverURL := flags.String("server-url", DefaultServerURL, "Collector URL")
	scope := flags.String("scope", "user", "Install scope: user | project. Defaults to user so one install covers every Claude Code project on this machine.")
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
		fmt.Fprintln(stderr, "The command written into settings.json is the absolute path of the")
		fmt.Fprintln(stderr, "currently-running apogee binary plus ``hook --event <X>``. No Python")
		fmt.Fprintln(stderr, "is involved, and no files are extracted outside settings.json itself.")
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

	// SourceApp stays empty by default — `apogee hook` derives it at
	// runtime so one install can span every project on this machine.

	result, err := Init(cfg)
	if err != nil {
		return err
	}
	printInitPlan(stdout, cfg, result)
	return nil
}

// legacyPythonPrefix is the detection prefix used to find v0.1.x hook
// entries that still call the old Python ``send_event.py`` script.
// These rows are NOT auto-migrated — the plan output hints at
// ``--force`` to replace them in place.
const legacyPythonPrefix = "python3 "

// Init performs the init logic against an already-resolved InitConfig. It is
// the unit-testable entry point.
func Init(cfg InitConfig) (*InitResult, error) {
	if cfg.Target == "" {
		return nil, errors.New("init: target is required")
	}
	if cfg.ServerURL == "" {
		cfg.ServerURL = DefaultServerURL
	}
	if cfg.HookCommand == "" {
		cfg.HookCommand = DefaultHookCommand()
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
		HookCommand:  cfg.HookCommand,
		ServerURL:    cfg.ServerURL,
	}

	commandPrefix := cfg.HookCommand
	for _, event := range HookEvents {
		entries := listOf(hooksSection[event])
		// Count any legacy Python entries so the plan can hint at --force.
		result.LegacyFound += countEntriesWithPrefix(entries, legacyPythonPrefix)

		already := hasApogeeEntry(entries, commandPrefix)
		if already && !cfg.Force {
			result.Skipped = append(result.Skipped, event)
			continue
		}
		if cfg.Force {
			entries = removeApogeeEntries(entries, commandPrefix)
			// --force also strips legacy Python rows so the rewrite is
			// truly a replacement, not an accumulation.
			entries = removeApogeeEntries(entries, legacyPythonPrefix)
		}
		parts := []string{
			commandPrefix,
			"--event", event,
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

// countEntriesWithPrefix returns the number of hook commands under
// entries whose command starts with prefix. Used to count legacy
// ``python3 send_event.py`` rows so init can warn about them.
func countEntriesWithPrefix(entries []any, prefix string) int {
	n := 0
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
				n++
			}
		}
	}
	return n
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

// deriveSourceAppFromTarget converts a ``.claude`` directory path into a
// plausible source_app label: the basename of the parent directory, or
// ``apogee`` as a last resort. Kept for callers that want to pin a label
// based on the current target directory; the default init flow leaves
// SourceApp empty so `apogee hook` derives it at runtime.
func deriveSourceAppFromTarget(target string) string {
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

// printInitPlan prints a human-readable summary of an InitResult in the
// diff-style format documented in the PR description.
func printInitPlan(w io.Writer, cfg InitConfig, r *InitResult) {
	fmt.Fprintln(w, "apogee init — plan")
	fmt.Fprintln(w)
	fmt.Fprintf(w, "  Target: %s (%s scope)\n", r.SettingsPath, cfg.Scope)
	if r.SourceApp != "" {
		fmt.Fprintf(w, "  Source app: %s (pinned)\n", r.SourceApp)
	} else {
		fmt.Fprintln(w, "  Source app: auto — derived by `apogee hook` at runtime")
		fmt.Fprintln(w, "              ($APOGEE_SOURCE_APP → git toplevel → $PWD)")
	}
	fmt.Fprintf(w, "  Hook command: %s\n", r.HookCommand)
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
	if r.LegacyFound > 0 && !cfg.Force {
		fmt.Fprintln(w)
		fmt.Fprintf(w, "  Notice: found %d legacy Python hook entries; pass --force to replace them.\n", r.LegacyFound)
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

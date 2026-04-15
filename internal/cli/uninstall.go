package cli

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
)

// NewUninstallCmd returns the `apogee uninstall` subcommand: the
// big red button. In order: stop+uninstall the daemon, strip apogee
// hooks from ~/.claude/settings.json, and (optionally) wipe
// ~/.apogee.
func NewUninstallCmd(stdout, stderr io.Writer) *cobra.Command {
	var (
		yes   bool
		purge bool
	)
	cmd := &cobra.Command{
		Use:   "uninstall",
		Short: "Remove apogee entirely (daemon + hooks, optionally data)",
		Long: `Stop the daemon, remove the unit file, strip apogee hook
entries from ~/.claude/settings.json, and optionally wipe the
~/.apogee data directory.

The daemon + hook removal are gated behind --yes (or an interactive
"y" prompt). Wiping ~/.apogee is a separate decision, gated behind
--purge (or its own prompt).`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runUninstall(cmd.Context(), stdout, stderr, os.Stdin, yes, purge)
		},
	}
	cmd.Flags().BoolVar(&yes, "yes", false, "Skip the daemon + hook confirmation prompt")
	cmd.Flags().BoolVar(&purge, "purge", false, "Also delete ~/.apogee (skips the data prompt)")
	return cmd
}

func runUninstall(ctx context.Context, stdout, stderr io.Writer, stdin io.Reader, yes, purge bool) error {
	if !yes {
		ok, err := confirm(stdin, stdout,
			"This will stop the apogee daemon and strip apogee hooks from ~/.claude/settings.json.\nProceed? [y/N]: ")
		if err != nil {
			return err
		}
		if !ok {
			fmt.Fprintln(stdout, "uninstall: aborted.")
			return nil
		}
	}

	// Step 1: stop + uninstall the daemon. Best-effort: a missing
	// daemon is not an error.
	m, err := managerFactory()
	if err != nil {
		fmt.Fprintf(stderr, "uninstall: daemon manager: %v\n", err)
	} else {
		_ = m.Stop(ctx)
		if uerr := m.Uninstall(ctx); uerr != nil {
			fmt.Fprintf(stderr, "uninstall: daemon uninstall: %v\n", uerr)
		} else {
			fmt.Fprintf(stdout, "%s daemon removed\n", checkGlyph)
		}
	}

	// Step 2: strip apogee hook entries from settings.json. We
	// reuse the existing init removal helper so this stays in sync
	// with the hook installer.
	if removed, err := stripApogeeHooks(); err != nil {
		fmt.Fprintf(stderr, "uninstall: hooks strip: %v\n", err)
	} else if removed > 0 {
		fmt.Fprintf(stdout, "%s removed %d apogee hook entries from ~/.claude/settings.json\n", checkGlyph, removed)
	} else {
		fmt.Fprintf(stdout, "%s no apogee hook entries to remove\n", checkGlyph)
	}

	// Step 3: data directory. This is gated separately: --purge
	// skips the prompt, otherwise we show a breakdown of what's
	// there and ask.
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	dataDir := filepath.Join(home, ".apogee")
	if _, statErr := os.Stat(dataDir); os.IsNotExist(statErr) {
		fmt.Fprintln(stdout, "uninstall: no data directory at ~/.apogee")
		return nil
	}

	if !purge {
		fmt.Fprintln(stdout)
		fmt.Fprintln(stdout, "Data directory contents (~/.apogee):")
		if err := printDataDirSummary(stdout, dataDir); err != nil {
			fmt.Fprintf(stderr, "uninstall: data summary: %v\n", err)
		}
		ok, err := confirm(stdin, stdout, "\nDelete ~/.apogee entirely? [y/N]: ")
		if err != nil {
			return err
		}
		if !ok {
			fmt.Fprintln(stdout, "uninstall: keeping data directory.")
			return nil
		}
	}

	if err := os.RemoveAll(dataDir); err != nil {
		fmt.Fprintf(stderr, "uninstall: remove data dir: %v\n", err)
		return err
	}
	fmt.Fprintf(stdout, "%s removed %s\n", checkGlyph, dataDir)
	return nil
}

// stripApogeeHooks walks every entry under ~/.claude/settings.json
// and removes every hook command whose prefix matches the apogee
// binary. Returns the number of removed entries. Missing settings
// file is not an error.
func stripApogeeHooks() (int, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return 0, err
	}
	settingsPath := filepath.Join(home, ".claude", "settings.json")
	data, err := os.ReadFile(settingsPath)
	if errors.Is(err, fs.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	// Load, walk, and rewrite using the existing init helpers. The
	// "apogee" detection prefix matches any command containing
	// "apogee hook" — broad enough to catch both absolute and
	// relative binary paths.
	settings := map[string]any{}
	if err := json.Unmarshal(data, &settings); err != nil {
		return 0, fmt.Errorf("parse settings: %w", err)
	}
	hooks, err := hooksSectionOf(settings)
	if err != nil {
		return 0, err
	}
	removed := 0
	for event, raw := range hooks {
		entries := listOf(raw)
		cleaned := removeHooksMatching(entries, func(cmd string) bool {
			return strings.Contains(cmd, "apogee hook") || strings.Contains(cmd, "apogee-hook")
		})
		removed += len(entries) - len(cleaned)
		if len(cleaned) == 0 {
			delete(hooks, event)
		} else {
			hooks[event] = cleaned
		}
	}
	if removed == 0 {
		return 0, nil
	}
	settings["hooks"] = hooks
	out, err := marshalStable(settings)
	if err != nil {
		return 0, err
	}
	if err := writeFileAtomic(settingsPath, out, 0o644); err != nil {
		return 0, err
	}
	return removed, nil
}

// removeHooksMatching is a generic variant of removeApogeeEntries
// that takes a predicate. The base helper in init.go only matches a
// literal prefix, which doesn't quite work for "apogee hook"
// anywhere in the command. Kept here so init.go stays narrow.
func removeHooksMatching(entries []any, match func(cmd string) bool) []any {
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
			if cok && match(cmd) {
				continue
			}
			kept = append(kept, h)
		}
		if len(kept) == 0 {
			continue
		}
		copyEntry := map[string]any{}
		for k, v := range m {
			copyEntry[k] = v
		}
		copyEntry["hooks"] = kept
		out = append(out, copyEntry)
	}
	return out
}

// printDataDirSummary walks ~/.apogee one level deep and prints a
// human-readable size breakdown. Best-effort: individual stat
// errors are skipped.
func printDataDirSummary(w io.Writer, dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, e := range entries {
		path := filepath.Join(dir, e.Name())
		size, err := pathSize(path)
		if err != nil {
			continue
		}
		kind := "file"
		if e.IsDir() {
			kind = "dir"
		}
		fmt.Fprintf(w, "  %-4s %-24s %s\n", kind, e.Name(), humanSize(size))
	}
	return nil
}

func pathSize(path string) (int64, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return 0, err
	}
	if !info.IsDir() {
		return info.Size(), nil
	}
	var total int64
	err = filepath.Walk(path, func(_ string, info fs.FileInfo, werr error) error {
		if werr != nil {
			return nil
		}
		if !info.IsDir() {
			total += info.Size()
		}
		return nil
	})
	return total, err
}

func humanSize(n int64) string {
	const k = 1024
	switch {
	case n < k:
		return fmt.Sprintf("%d B", n)
	case n < k*k:
		return fmt.Sprintf("%.1f KiB", float64(n)/k)
	case n < k*k*k:
		return fmt.Sprintf("%.1f MiB", float64(n)/(k*k))
	default:
		return fmt.Sprintf("%.1f GiB", float64(n)/(k*k*k))
	}
}

// confirm prompts stdin for a y/n response. Any answer other than
// "y" or "yes" (case-insensitive) is treated as "no".
func confirm(stdin io.Reader, prompt io.Writer, message string) (bool, error) {
	fmt.Fprint(prompt, message)
	reader := bufio.NewReader(stdin)
	line, err := reader.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return false, err
	}
	line = strings.ToLower(strings.TrimSpace(line))
	return line == "y" || line == "yes", nil
}


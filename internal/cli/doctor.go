package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/BIwashi/apogee/internal/daemon"
	"github.com/BIwashi/apogee/internal/store/duckdb"
)

// DoctorCheck is one row in the doctor report. Severity is one of
// "ok" / "warn" / "error" / "info". Detail is optional supporting
// text rendered in muted style after the message.
type DoctorCheck struct {
	Name     string `json:"name"`
	Severity string `json:"severity"`
	Message  string `json:"message"`
	Detail   string `json:"detail,omitempty"`
}

// NewDoctorCmd reports a quick environment summary so users can sanity-check
// their install before wiring Claude Code at it. Every check is best-effort:
// nothing the command reports blocks the collector from booting. The output
// is stable enough to eyeball in CI and the --json flag exposes a machine
// readable variant for menubar / scripts.
func NewDoctorCmd(stdout io.Writer) *cobra.Command {
	var jsonOut bool
	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "Run a quick environment check",
		RunE: func(cmd *cobra.Command, _ []string) error {
			checks := runDoctorChecks(cmd.Context())
			if jsonOut {
				return writeDoctorJSON(stdout, checks)
			}
			return writeDoctorText(styledWriter(stdout), checks)
		},
	}
	cmd.Flags().BoolVar(&jsonOut, "json", false, "Emit checks as a JSON array (for scripts / menubar)")
	return cmd
}

// runDoctorChecks runs every doctor check sequentially and returns
// the combined slice. Pure logic so it is easy to unit test.
func runDoctorChecks(ctx context.Context) []DoctorCheck {
	out := make([]DoctorCheck, 0, 8)

	// 1. ~/.apogee/ writability
	apogeeDir, err := homeSubdir(".apogee")
	switch {
	case err != nil:
		out = append(out, DoctorCheck{Name: "home", Severity: "warn", Message: "home directory unavailable", Detail: err.Error()})
	default:
		if werr := checkDirWritable(apogeeDir); werr != nil {
			out = append(out, DoctorCheck{Name: "home", Severity: "warn", Message: apogeeDir + " not writable", Detail: werr.Error()})
		} else {
			out = append(out, DoctorCheck{Name: "home", Severity: "ok", Message: apogeeDir + " writable"})
		}
	}

	// 2. claude CLI (used by the summarizer)
	if path, err := exec.LookPath("claude"); err != nil {
		out = append(out, DoctorCheck{Name: "claude_cli", Severity: "warn", Message: "claude CLI not found (summarizer will be disabled)"})
	} else {
		out = append(out, DoctorCheck{Name: "claude_cli", Severity: "ok", Message: "claude CLI on PATH", Detail: path})
	}

	// 3. default DuckDB path writable
	dbPath := ""
	if apogeeDir != "" {
		dbPath = filepath.Join(apogeeDir, "apogee.duckdb")
	}
	switch {
	case dbPath == "":
		out = append(out, DoctorCheck{Name: "db_path", Severity: "warn", Message: "default db path not resolvable"})
	default:
		if err := checkDirWritable(filepath.Dir(dbPath)); err != nil {
			out = append(out, DoctorCheck{Name: "db_path", Severity: "warn", Message: dbPath + " not writable", Detail: err.Error()})
		} else {
			out = append(out, DoctorCheck{Name: "db_path", Severity: "ok", Message: "default db path " + dbPath})
		}
	}

	// 4. config file note (not an error when absent — defaults apply)
	if apogeeDir != "" {
		cfgPath := filepath.Join(apogeeDir, "config.toml")
		if _, err := os.Stat(cfgPath); err == nil {
			out = append(out, DoctorCheck{Name: "config", Severity: "ok", Message: "config at " + cfgPath})
		} else {
			out = append(out, DoctorCheck{Name: "config", Severity: "info", Message: "no config file (defaults in use)", Detail: cfgPath})
		}
	}

	// 5. DuckDB lock holder
	if dbPath != "" {
		if _, statErr := os.Stat(dbPath); statErr == nil {
			err := duckdb.CheckDBLockHolder(ctx, dbPath)
			var locked *duckdb.LockedError
			switch {
			case err == nil:
				out = append(out, DoctorCheck{Name: "db_lock", Severity: "ok", Message: "DuckDB file is unlocked"})
			case errors.As(err, &locked):
				// If the daemon is installed AND the holder pid matches the
				// daemon's pid, this is the expected good state.
				severity, msg := classifyLockHolder(ctx, locked)
				out = append(out, DoctorCheck{Name: "db_lock", Severity: severity, Message: msg, Detail: locked.Path})
			default:
				out = append(out, DoctorCheck{Name: "db_lock", Severity: "warn", Message: "DuckDB lock probe failed", Detail: err.Error()})
			}
		} else {
			out = append(out, DoctorCheck{Name: "db_lock", Severity: "info", Message: "DuckDB file does not exist yet"})
		}
	}

	// 6. Collector reachability
	out = append(out, doctorCheckCollector(daemon.DefaultAddr))

	// 7. Hook install
	out = append(out, doctorCheckHookInstall())

	return out
}

// classifyLockHolder decides whether a lock conflict is "ok" (the
// installed daemon owns the lock), "warn" (held but holder unknown),
// or "error" (held by an unknown process).
func classifyLockHolder(ctx context.Context, locked *duckdb.LockedError) (severity, message string) {
	mfn := managerFactory
	if mfn != nil {
		if m, err := mfn(); err == nil {
			if s, serr := m.Status(ctx); serr == nil && s.Running && s.PID > 0 && s.PID == locked.PID {
				return "ok", fmt.Sprintf("DuckDB locked by apogee daemon (pid %d)", locked.PID)
			}
		}
	}
	if locked.PID > 0 {
		return "error", fmt.Sprintf("DuckDB locked by unknown process (pid %d)", locked.PID)
	}
	return "warn", "DuckDB locked by an unknown process"
}

// doctorCheckCollector probes /v1/healthz on the configured addr
// with a 500 ms timeout.
func doctorCheckCollector(addr string) DoctorCheck {
	url := "http://" + addr + "/v1/healthz"
	client := &http.Client{Timeout: 500 * time.Millisecond}
	resp, err := client.Get(url)
	if err != nil {
		return DoctorCheck{Name: "collector", Severity: "warn", Message: "collector not running", Detail: url}
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode == http.StatusOK {
		return DoctorCheck{Name: "collector", Severity: "ok", Message: "collector reachable", Detail: url}
	}
	return DoctorCheck{Name: "collector", Severity: "error", Message: fmt.Sprintf("collector returned HTTP %d", resp.StatusCode), Detail: url}
}

// doctorCheckHookInstall reads ~/.claude/settings.json and verifies
// every event in HookEvents has at least one apogee-binary command
// installed. OK when every event is covered, warn for partial,
// missing when no apogee entries exist.
func doctorCheckHookInstall() DoctorCheck {
	home, err := os.UserHomeDir()
	if err != nil {
		return DoctorCheck{Name: "hook_install", Severity: "warn", Message: "home directory unavailable"}
	}
	settingsPath := filepath.Join(home, ".claude", "settings.json")
	data, err := os.ReadFile(settingsPath)
	if errors.Is(err, os.ErrNotExist) {
		return DoctorCheck{Name: "hook_install", Severity: "warn", Message: "no apogee hook entries (run `apogee init`)", Detail: settingsPath}
	}
	if err != nil {
		return DoctorCheck{Name: "hook_install", Severity: "warn", Message: "cannot read settings.json", Detail: err.Error()}
	}
	settings := map[string]any{}
	if err := json.Unmarshal(data, &settings); err != nil {
		return DoctorCheck{Name: "hook_install", Severity: "warn", Message: "settings.json is not valid JSON", Detail: err.Error()}
	}
	hooks, _ := hooksSectionOf(settings)
	missing := make([]string, 0)
	covered := 0
	for _, ev := range HookEvents {
		entries := listOf(hooks[ev])
		if hookEventCoveredByApogee(entries) {
			covered++
		} else {
			missing = append(missing, ev)
		}
	}
	switch {
	case covered == len(HookEvents):
		return DoctorCheck{Name: "hook_install", Severity: "ok", Message: fmt.Sprintf("apogee hook installed for %d/%d events", covered, len(HookEvents))}
	case covered == 0:
		return DoctorCheck{Name: "hook_install", Severity: "warn", Message: "no apogee hook entries (run `apogee init`)", Detail: settingsPath}
	default:
		return DoctorCheck{Name: "hook_install", Severity: "warn", Message: fmt.Sprintf("apogee hook installed for %d/%d events (missing: %s)", covered, len(HookEvents), strings.Join(missing, ", "))}
	}
}

// hookEventCoveredByApogee returns true if any inner hook command
// references the apogee binary's hook subcommand.
func hookEventCoveredByApogee(entries []any) bool {
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
			cmd, _ := hm["command"].(string)
			if strings.Contains(cmd, "apogee hook") || strings.Contains(cmd, "apogee-hook") {
				return true
			}
		}
	}
	return false
}

// writeDoctorText renders the doctor checks as styled lines plus a
// summary footer.
func writeDoctorText(out io.Writer, checks []DoctorCheck) error {
	fmt.Fprintln(out, renderHeading("apogee doctor"))
	fmt.Fprintln(out)
	ok, warn, errs := 0, 0, 0
	for _, c := range checks {
		fmt.Fprintln(out, formatDoctorLine(c))
		switch strings.ToLower(c.Severity) {
		case "ok":
			ok++
		case "warn":
			warn++
		case "error":
			errs++
		}
	}
	fmt.Fprintln(out)
	fmt.Fprintln(out, styleMuted.Render(fmt.Sprintf("%d ok · %d warning · %d errors", ok, warn, errs)))
	return nil
}

// formatDoctorLine renders one check as glyph + message + optional muted detail.
func formatDoctorLine(c DoctorCheck) string {
	glyph := styleGlyph(c.Severity)
	msg := c.Message
	if c.Detail != "" {
		msg = msg + " " + styleMuted.Render("("+c.Detail+")")
	}
	return fmt.Sprintf("  %s %s", glyph, msg)
}

// writeDoctorJSON serialises the checks as a JSON array.
func writeDoctorJSON(out io.Writer, checks []DoctorCheck) error {
	enc := json.NewEncoder(out)
	enc.SetIndent("", "  ")
	return enc.Encode(checks)
}

// homeSubdir returns filepath.Join(home, sub) or an error when the home
// directory is not resolvable. Extracted so tests can exercise the
// error-free path without touching $HOME.
func homeSubdir(sub string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, sub), nil
}

// checkDirWritable tries to create the directory and then write+remove a
// tempfile inside it. Returns an error describing the first problem.
func checkDirWritable(dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("mkdir: %w", err)
	}
	f, err := os.CreateTemp(dir, ".apogee-doctor-*")
	if err != nil {
		return fmt.Errorf("write probe: %w", err)
	}
	name := f.Name()
	_ = f.Close()
	_ = os.Remove(name)
	return nil
}

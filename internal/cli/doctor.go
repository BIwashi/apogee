package cli

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/spf13/cobra"
)

// NewDoctorCmd reports a quick environment summary so users can sanity-check
// their install before wiring Claude Code at it. Every check is best-effort:
// nothing the command reports blocks the collector from booting. The output
// is stable enough to eyeball in CI.
func NewDoctorCmd(stdout io.Writer) *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Run a quick environment check",
		RunE: func(_ *cobra.Command, _ []string) error {
			return runDoctor(stdout)
		},
	}
}

func runDoctor(out io.Writer) error {
	fmt.Fprintln(out, "apogee doctor — environment check")
	fmt.Fprintln(out)

	// 1. ~/.apogee/ writability
	apogeeDir, err := homeSubdir(".apogee")
	if err != nil {
		fmt.Fprintf(out, "  WARN  home directory unavailable: %v\n", err)
	} else if werr := checkDirWritable(apogeeDir); werr != nil {
		fmt.Fprintf(out, "  WARN  %s: %v\n", apogeeDir, werr)
	} else {
		fmt.Fprintf(out, "  OK    %s writable\n", apogeeDir)
	}

	// 2. python3 availability
	if path, err := exec.LookPath("python3"); err != nil {
		fmt.Fprintln(out, "  WARN  python3 not found in PATH (hooks will fail until you install Python 3)")
	} else {
		fmt.Fprintf(out, "  OK    python3 at %s\n", path)
	}

	// 3. claude CLI (used by the summarizer)
	if path, err := exec.LookPath("claude"); err != nil {
		fmt.Fprintln(out, "  WARN  claude CLI not found (summarizer will be disabled)")
	} else {
		fmt.Fprintf(out, "  OK    claude at %s\n", path)
	}

	// 4. default DuckDB path
	dbPath := filepath.Join(apogeeDir, "apogee.duckdb")
	if apogeeDir == "" {
		fmt.Fprintln(out, "  WARN  default db path not resolvable")
	} else if err := checkDirWritable(filepath.Dir(dbPath)); err != nil {
		fmt.Fprintf(out, "  WARN  %s: %v\n", dbPath, err)
	} else {
		fmt.Fprintf(out, "  OK    default db path %s\n", dbPath)
	}

	// 5. config file note (not an error when absent — defaults apply)
	cfgPath := filepath.Join(apogeeDir, "config.toml")
	if _, err := os.Stat(cfgPath); err == nil {
		fmt.Fprintf(out, "  OK    config at %s\n", cfgPath)
	} else {
		fmt.Fprintf(out, "  INFO  no config at %s (defaults in use)\n", cfgPath)
	}

	fmt.Fprintln(out)
	return nil
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

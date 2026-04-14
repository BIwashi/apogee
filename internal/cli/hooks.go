// Package cli implements apogee's command-line subcommands. The package
// is consumed from cmd/apogee/main.go and holds the logic for `apogee init`,
// `apogee hooks extract`, and their shared helpers. Everything user-facing
// goes through stdlib flag parsing and plain io.Writers so the subcommands
// are trivially testable.
package cli

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	rootembed "github.com/BIwashi/apogee"
	"github.com/BIwashi/apogee/internal/version"
)

// embeddedHooksRoot is the top-level directory inside the embedded FS that
// contains the Python hook library. Keep it in sync with hooksfs.go.
const embeddedHooksRoot = "hooks"

// hooksFS returns a sub-filesystem rooted at the embedded hooks directory.
// The returned FS has ``apogee_hook.py`` at its root, which is convenient for
// callers that want to write files directly under a destination directory.
func hooksFS() (fs.FS, error) {
	return fs.Sub(rootembed.HooksFS(), embeddedHooksRoot)
}

// ExtractHooks copies the embedded Python hook library to destDir. Files that
// already exist are only overwritten when force is true. The directory is
// created if it does not exist. The returned string slice contains the list
// of files written (relative to destDir) for caller logging.
func ExtractHooks(destDir string, force bool) ([]string, error) {
	src, err := hooksFS()
	if err != nil {
		return nil, fmt.Errorf("extract hooks: %w", err)
	}
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return nil, fmt.Errorf("extract hooks: mkdir %s: %w", destDir, err)
	}

	var written []string
	err = fs.WalkDir(src, ".", func(path string, d fs.DirEntry, werr error) error {
		if werr != nil {
			return werr
		}
		if path == "." {
			return nil
		}
		// Skip Python bytecode caches and other non-source artefacts. These
		// can creep in if a dev runs the test suite locally before building
		// the Go binary.
		base := filepath.Base(path)
		if base == "__pycache__" || strings.HasSuffix(base, ".pyc") || strings.HasSuffix(base, ".pyo") {
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		target := filepath.Join(destDir, filepath.FromSlash(path))
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		if _, err := os.Stat(target); err == nil && !force {
			// File exists and we're not forcing — leave it alone.
			return nil
		}
		data, err := fs.ReadFile(src, path)
		if err != nil {
			return fmt.Errorf("read embedded %s: %w", path, err)
		}
		mode := os.FileMode(0o644)
		if strings.HasSuffix(path, ".sh") || strings.HasSuffix(path, ".py") {
			mode = 0o755
		}
		if err := writeFileAtomic(target, data, mode); err != nil {
			return fmt.Errorf("write %s: %w", target, err)
		}
		written = append(written, path)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return written, nil
}

// DefaultHooksDir returns the canonical extraction path: ``~/.apogee/hooks/<version>``.
// A non-absolute home directory is an error — we do not fall back to the CWD
// because that would silently install to the wrong place.
func DefaultHooksDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("user home dir: %w", err)
	}
	return filepath.Join(home, ".apogee", "hooks", version.Version), nil
}

// RunHooksExtract is the `apogee hooks extract` entry point. args are the
// positional arguments after the subcommand name.
func RunHooksExtract(args []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("hooks extract", flag.ContinueOnError)
	flags.SetOutput(stderr)
	to := flags.String("to", "", "destination directory (default: ~/.apogee/hooks/<version>)")
	force := flags.Bool("force", false, "overwrite existing files")
	flags.Usage = func() {
		fmt.Fprintln(stderr, "apogee hooks extract — write the embedded Python hook library to disk")
		fmt.Fprintln(stderr)
		fmt.Fprintln(stderr, "Flags:")
		flags.PrintDefaults()
	}
	if err := flags.Parse(args); err != nil {
		return err
	}

	dest := *to
	if dest == "" {
		d, err := DefaultHooksDir()
		if err != nil {
			return err
		}
		dest = d
	}
	expanded, err := expandHome(dest)
	if err != nil {
		return err
	}

	written, err := ExtractHooks(expanded, *force)
	if err != nil {
		return err
	}
	fmt.Fprintf(stdout, "apogee hooks extract: wrote %d files to %s\n", len(written), expanded)
	return nil
}

// RunHooks dispatches `apogee hooks <verb>`.
func RunHooks(args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "usage: apogee hooks <extract> [flags]")
		return errors.New("hooks: missing subcommand")
	}
	switch args[0] {
	case "extract":
		return RunHooksExtract(args[1:], stdout, stderr)
	default:
		return fmt.Errorf("hooks: unknown subcommand %q", args[0])
	}
}

// writeFileAtomic writes data to path via a tempfile + rename so a crash
// mid-write cannot corrupt an existing file.
func writeFileAtomic(path string, data []byte, mode os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".apogee-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer func() {
		_ = os.Remove(tmpPath) // best effort; no-op if rename succeeded
	}()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Chmod(mode); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}

// expandHome replaces a leading ~ with the user's home directory.
func expandHome(p string) (string, error) {
	if p == "" {
		return p, nil
	}
	if p == "~" || strings.HasPrefix(p, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(home, strings.TrimPrefix(p, "~")), nil
	}
	return p, nil
}

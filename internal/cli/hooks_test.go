package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExtractHooksWritesExpectedFiles(t *testing.T) {
	dest := t.TempDir()
	written, err := ExtractHooks(dest, false)
	if err != nil {
		t.Fatalf("ExtractHooks: %v", err)
	}
	if len(written) == 0 {
		t.Fatal("ExtractHooks returned no files")
	}

	want := []string{
		"apogee_hook.py",
		"send_event.py",
		"install.py",
		"example_settings.json",
		"README.md",
		"__init__.py",
		"run_tests.sh",
		"smoke_test.sh",
		"tests/test_apogee_hook.py",
	}
	for _, rel := range want {
		p := filepath.Join(dest, filepath.FromSlash(rel))
		if _, err := os.Stat(p); err != nil {
			t.Errorf("expected %s to exist: %v", rel, err)
		}
	}

	// send_event.py must be executable.
	info, err := os.Stat(filepath.Join(dest, "send_event.py"))
	if err != nil {
		t.Fatalf("stat send_event.py: %v", err)
	}
	if info.Mode().Perm()&0o100 == 0 {
		t.Errorf("send_event.py is not executable: %v", info.Mode())
	}
}

func TestExtractHooksRespectsForce(t *testing.T) {
	dest := t.TempDir()
	if _, err := ExtractHooks(dest, false); err != nil {
		t.Fatalf("first extract: %v", err)
	}

	target := filepath.Join(dest, "send_event.py")
	if err := os.WriteFile(target, []byte("# overridden"), 0o644); err != nil {
		t.Fatalf("overwrite: %v", err)
	}

	if _, err := ExtractHooks(dest, false); err != nil {
		t.Fatalf("second extract (no force): %v", err)
	}
	data, _ := os.ReadFile(target)
	if string(data) != "# overridden" {
		t.Errorf("without --force the user's override should survive, got %q", string(data))
	}

	if _, err := ExtractHooks(dest, true); err != nil {
		t.Fatalf("force extract: %v", err)
	}
	data, _ = os.ReadFile(target)
	if string(data) == "# overridden" {
		t.Errorf("with --force the embedded content should replace the override")
	}
}

func TestRunHooksExtractCLI(t *testing.T) {
	dest := t.TempDir()
	var stdout, stderr bytes.Buffer
	if err := RunHooksExtract([]string{"--to", dest}, &stdout, &stderr); err != nil {
		t.Fatalf("RunHooksExtract: %v (stderr=%s)", err, stderr.String())
	}
	if !strings.Contains(stdout.String(), "wrote") {
		t.Errorf("expected wrote message, got %q", stdout.String())
	}
}

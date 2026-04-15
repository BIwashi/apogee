package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteTailReturnsLastLines(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "log.txt")
	content := "l1\nl2\nl3\nl4\nl5\nl6\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	var out bytes.Buffer
	if err := writeTail(&out, path, 3); err != nil {
		t.Fatalf("writeTail: %v", err)
	}
	got := out.String()
	if got != "l4\nl5\nl6\n" {
		t.Errorf("writeTail expected last 3 lines, got %q", got)
	}
}

func TestWriteTailLargerThanFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "log.txt")
	content := "one\ntwo\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	var out bytes.Buffer
	if err := writeTail(&out, path, 50); err != nil {
		t.Fatalf("writeTail: %v", err)
	}
	if out.String() != content {
		t.Errorf("expected full content, got %q", out.String())
	}
}

func TestRunLogsNoDaemon(t *testing.T) {
	// Temporarily relocate HOME so logPaths points at an empty dir.
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	done := make(chan struct{})
	close(done) // not used in one-shot mode but keeps the signature honest
	var out, errBuf bytes.Buffer
	err := runLogs(done, &out, &errBuf, "both", 10, false)
	if err != nil {
		t.Fatalf("runLogs: %v", err)
	}
	if !strings.Contains(errBuf.String(), "daemon is not installed") {
		t.Errorf("expected friendly message, got: %s / %s", errBuf.String(), out.String())
	}
}

func TestRunLogsOneShot(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	logDir := filepath.Join(tmp, ".apogee", "logs")
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	outPath := filepath.Join(logDir, "apogee.out.log")
	errPath := filepath.Join(logDir, "apogee.err.log")
	if err := os.WriteFile(outPath, []byte("line-1\nline-2\n"), 0o644); err != nil {
		t.Fatalf("write out: %v", err)
	}
	if err := os.WriteFile(errPath, []byte("err-1\n"), 0o644); err != nil {
		t.Fatalf("write err: %v", err)
	}
	done := make(chan struct{})
	close(done)
	var stdout, stderr bytes.Buffer
	if err := runLogs(done, &stdout, &stderr, "both", 50, false); err != nil {
		t.Fatalf("runLogs: %v", err)
	}
	out := stdout.String()
	if !strings.Contains(out, "line-1") {
		t.Errorf("missing out log: %s", out)
	}
	if !strings.Contains(out, "err-1") {
		t.Errorf("missing err log: %s", out)
	}
}

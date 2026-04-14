package summarizer

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os/exec"
	"strings"
	"time"
)

// Runner is the minimal surface the worker depends on for calling the
// `claude` CLI. Tests pass a FakeRunner; production uses CLIRunner.
type Runner interface {
	// Run executes a single prompt against the given model alias and
	// returns the model's plain-text output (the claude envelope's
	// `result` field). Implementations must honour ctx cancellation.
	Run(ctx context.Context, model string, prompt string) (string, error)
}

// CLIRunner shells out to the local `claude` binary. It is safe for
// concurrent use — exec.CommandContext returns a fresh child every call.
type CLIRunner struct {
	// BinaryPath is the path (or PATH-relative name) of the CLI. Empty
	// defaults to "claude".
	BinaryPath string
	// Timeout is the per-invocation wall-clock budget. Zero disables the
	// internal deadline; callers are still expected to pass a bounded ctx.
	Timeout time.Duration
	// Logger receives debug traces of the raw CLI output. Never nil:
	// construct via NewCLIRunner which installs a discard logger when the
	// caller passes nil.
	Logger *slog.Logger
}

// NewCLIRunner returns a CLIRunner with sensible defaults.
func NewCLIRunner(binary string, timeout time.Duration, logger *slog.Logger) *CLIRunner {
	if binary == "" {
		binary = "claude"
	}
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelError}))
	}
	return &CLIRunner{BinaryPath: binary, Timeout: timeout, Logger: logger}
}

// cliEnvelope mirrors the JSON envelope returned by `claude -p
// --output-format=json`. Only the fields we consume are typed.
type cliEnvelope struct {
	Result       string `json:"result"`
	IsError      bool   `json:"is_error"`
	NumTurns     int    `json:"num_turns"`
	SessionID    string `json:"session_id"`
	Subtype      string `json:"subtype"`
	Error        string `json:"error,omitempty"`
	ErrorMessage string `json:"error_message,omitempty"`
}

// Run implements Runner by invoking the CLI with the prompt piped on stdin.
func (r *CLIRunner) Run(ctx context.Context, model, prompt string) (string, error) {
	if r == nil {
		return "", fmt.Errorf("cli runner: nil receiver")
	}
	binary := r.BinaryPath
	if binary == "" {
		binary = "claude"
	}
	if r.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, r.Timeout)
		defer cancel()
	}

	args := []string{"-p", "--output-format=json", "--max-turns", "1"}
	if strings.TrimSpace(model) != "" {
		args = append(args, "--model", model)
	}
	cmd := exec.CommandContext(ctx, binary, args...)
	cmd.Stdin = strings.NewReader(prompt)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		// Surface both streams so operators can debug a failed invocation.
		return "", fmt.Errorf(
			"cli runner: %w (stderr=%q stdout=%q)",
			err,
			truncate(stderr.String(), 2048),
			truncate(stdout.String(), 2048),
		)
	}

	raw := bytes.TrimSpace(stdout.Bytes())
	if len(raw) == 0 {
		return "", fmt.Errorf("cli runner: empty stdout (stderr=%q)", truncate(stderr.String(), 512))
	}

	var env cliEnvelope
	if err := json.Unmarshal(raw, &env); err != nil {
		// The CLI usually emits JSON, but fall back to the raw stdout if
		// the caller happens to hand us something plainer. Log at debug
		// so the failure is visible without drowning the hot path.
		r.Logger.Debug("cli runner: envelope decode failed", "err", err, "raw", truncate(string(raw), 512))
		return string(raw), nil
	}
	if env.IsError || env.Error != "" {
		msg := env.ErrorMessage
		if msg == "" {
			msg = env.Error
		}
		return "", fmt.Errorf("cli runner: claude reported error: %s", msg)
	}
	return env.Result, nil
}

// FakeRunner is a test seam that lets suites inject canned responses
// without shelling out. Responder is called for every Run invocation.
type FakeRunner struct {
	Responder func(model, prompt string) (string, error)
	// LastModel and LastPrompt capture the arguments of the most recent
	// Run call so tests can assert prompt shape.
	LastModel  string
	LastPrompt string
	Calls      int
}

// Run implements Runner.
func (f *FakeRunner) Run(_ context.Context, model, prompt string) (string, error) {
	f.LastModel = model
	f.LastPrompt = prompt
	f.Calls++
	if f.Responder == nil {
		return "", fmt.Errorf("fake runner: no responder set")
	}
	return f.Responder(model, prompt)
}

func truncate(s string, max int) string {
	if max <= 0 || len(s) <= max {
		return s
	}
	return s[:max] + "…"
}

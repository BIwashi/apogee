package summarizer

import (
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestCLIRunnerReturnsInnerResult(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell script fixture is POSIX-only")
	}
	script, err := filepath.Abs(filepath.Join("testdata", "fake-claude.sh"))
	require.NoError(t, err)

	runner := NewCLIRunner(script, 5*time.Second, nil)
	out, err := runner.Run(t.Context(), "claude-haiku-4-5", "ignored")
	require.NoError(t, err)
	require.True(t, strings.Contains(out, "fake recap"), "got %q", out)
}

func TestFakeRunnerInjection(t *testing.T) {
	f := &FakeRunner{Responder: func(model, prompt string) (string, error) {
		require.Equal(t, "m", model)
		return "hello", nil
	}}
	out, err := f.Run(t.Context(), "m", "p")
	require.NoError(t, err)
	require.Equal(t, "hello", out)
	require.Equal(t, 1, f.Calls)
	require.Equal(t, "p", f.LastPrompt)
}

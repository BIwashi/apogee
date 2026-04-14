package summarizer

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestDefaultConfig(t *testing.T) {
	cfg := Default()
	require.True(t, cfg.Enabled)
	require.Equal(t, "claude-haiku-4-5", cfg.RecapModel)
	require.Equal(t, "claude-sonnet-4-6", cfg.RollupModel)
	require.Equal(t, 1, cfg.Concurrency)
	require.Equal(t, 120*time.Second, cfg.Timeout)
	require.Equal(t, "claude", cfg.CLIPath)
	require.Equal(t, 256, cfg.QueueSize)
	require.Equal(t, int64(1500), cfg.MinTurnDurationMs)
	require.Equal(t, 500, cfg.MaxSpanCount)
	require.Equal(t, 300, cfg.MaxLogCount)
}

func TestLoadTOMLOverrides(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	body := `
[summarizer]
enabled = false
recap_model = "custom-haiku"
rollup_model = "custom-sonnet"
concurrency = 4
timeout_seconds = 30
max_span_count = 42
max_log_count = 21
min_turn_duration_ms = 500
queue_size = 16
cli_path = "/opt/claude"
`
	require.NoError(t, os.WriteFile(path, []byte(body), 0o600))

	// Clear env so TOML wins.
	for _, k := range allEnvVars() {
		t.Setenv(k, "")
	}

	cfg, err := Load(path)
	require.NoError(t, err)
	require.False(t, cfg.Enabled)
	require.Equal(t, "custom-haiku", cfg.RecapModel)
	require.Equal(t, "custom-sonnet", cfg.RollupModel)
	require.Equal(t, 4, cfg.Concurrency)
	require.Equal(t, 30*time.Second, cfg.Timeout)
	require.Equal(t, 42, cfg.MaxSpanCount)
	require.Equal(t, 21, cfg.MaxLogCount)
	require.Equal(t, int64(500), cfg.MinTurnDurationMs)
	require.Equal(t, 16, cfg.QueueSize)
	require.Equal(t, "/opt/claude", cfg.CLIPath)
	require.Equal(t, path, cfg.ConfigPath)
}

func TestLoadEnvOverridesTOML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	require.NoError(t, os.WriteFile(path, []byte(`[summarizer]
recap_model = "file-model"
concurrency = 2
`), 0o600))

	t.Setenv("APOGEE_RECAP_MODEL", "env-model")
	t.Setenv("APOGEE_SUMMARIZER_CONCURRENCY", "7")
	t.Setenv("APOGEE_SUMMARIZER_TIMEOUT_SECONDS", "12")

	cfg, err := Load(path)
	require.NoError(t, err)
	require.Equal(t, "env-model", cfg.RecapModel)
	require.Equal(t, 7, cfg.Concurrency)
	require.Equal(t, 12*time.Second, cfg.Timeout)
}

func TestLoadMissingFileFallsBackToDefaults(t *testing.T) {
	for _, k := range allEnvVars() {
		t.Setenv(k, "")
	}
	cfg, err := Load(filepath.Join(t.TempDir(), "does-not-exist.toml"))
	require.NoError(t, err)
	require.Equal(t, Default().RecapModel, cfg.RecapModel)
}

func allEnvVars() []string {
	return []string{
		"APOGEE_SUMMARIZER_ENABLED",
		"APOGEE_RECAP_MODEL",
		"APOGEE_ROLLUP_MODEL",
		"APOGEE_SUMMARIZER_CONCURRENCY",
		"APOGEE_SUMMARIZER_TIMEOUT_SECONDS",
		"APOGEE_SUMMARIZER_CLI_PATH",
		"APOGEE_SUMMARIZER_QUEUE_SIZE",
		"APOGEE_SUMMARIZER_MIN_TURN_MS",
		"APOGEE_SUMMARIZER_MAX_SPANS",
		"APOGEE_SUMMARIZER_MAX_LOGS",
	}
}

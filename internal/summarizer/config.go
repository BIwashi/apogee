// Package summarizer runs the local `claude` CLI as an async worker that
// turns a finished apogee turn into a structured recap and writes the
// result back onto the turns row. The package is self-contained and never
// talks to the Anthropic API directly — it reuses the user's existing
// Claude Code CLI authentication by shelling out to `claude -p`.
package summarizer

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
)

// Config holds every tunable the summarizer cares about. The zero value is
// not usable — always call Default() first, then overlay with Load().
type Config struct {
	// Enabled toggles the whole subsystem. When false, NewService still
	// constructs an object but Start is a no-op.
	Enabled bool
	// RecapModel is the CLI model alias used for per-turn recaps.
	RecapModel string
	// RollupModel is reserved for the per-session rollup (PR #11).
	RollupModel string
	// NarrativeModel is the model alias used by the tier-3 phase narrative
	// worker (PR #32). Defaults to the same alias as RollupModel.
	NarrativeModel string
	// Concurrency is the number of worker goroutines consuming the queue.
	Concurrency int
	// Timeout bounds each individual CLI invocation.
	Timeout time.Duration
	// CLIPath is the path to the `claude` binary. Empty means "resolve via
	// PATH at Runner construction time".
	CLIPath string
	// QueueSize is the channel buffer for pending jobs.
	QueueSize int
	// MinTurnDurationMs skips turns that ended faster than this threshold.
	MinTurnDurationMs int64
	// MaxSpanCount caps how many spans get serialised into the prompt.
	MaxSpanCount int
	// MaxLogCount caps how many log records get serialised into the prompt.
	MaxLogCount int
	// MaxRollupTurns caps how many turns the session rollup tier loads.
	MaxRollupTurns int
	// RollupSchedulerEnabled toggles the once-an-hour background scheduler
	// that picks stale sessions and enqueues them for a rollup.
	RollupSchedulerEnabled bool
	// ConfigPath is the TOML file Load was asked to read, useful for
	// diagnostics. Populated by Load; empty when the file was absent.
	ConfigPath string
}

// Default returns a Config populated with every field's default value.
//
// Model aliases intentionally default to the empty string — the worker
// resolves them at job time via ResolveModelForUseCase(..., availability)
// so the "default model" is always the cheapest currently-available
// catalog entry rather than a hardcoded string that goes stale whenever
// Anthropic ships a new family. RecapModel / RollupModel / NarrativeModel
// stay as *explicit overrides* for operators who pin a specific alias in
// config.toml; they are no longer populated here.
func Default() Config {
	return Config{
		Enabled:                true,
		RecapModel:             "", // resolved via ResolveModelForUseCase(UseCaseRecap, ...)
		RollupModel:            "", // resolved via ResolveModelForUseCase(UseCaseRollup, ...)
		NarrativeModel:         "", // resolved via ResolveModelForUseCase(UseCaseNarrative, ...)
		Concurrency:            1,
		Timeout:                120 * time.Second,
		CLIPath:                "claude",
		QueueSize:              256,
		MinTurnDurationMs:      1500,
		MaxSpanCount:           500,
		MaxLogCount:            300,
		MaxRollupTurns:         40,
		RollupSchedulerEnabled: true,
	}
}

// tomlFile is the on-disk shape of `~/.apogee/config.toml`. Only the
// [summarizer] table is consulted; everything else is ignored.
type tomlFile struct {
	Summarizer struct {
		Enabled           *bool   `toml:"enabled"`
		RecapModel        string  `toml:"recap_model"`
		RollupModel       string  `toml:"rollup_model"`
		NarrativeModel    string  `toml:"narrative_model"`
		Concurrency       int     `toml:"concurrency"`
		TimeoutSeconds    float64 `toml:"timeout_seconds"`
		QueueSize         int     `toml:"queue_size"`
		MinTurnDurationMs int64   `toml:"min_turn_duration_ms"`
		MaxSpanCount      int     `toml:"max_span_count"`
		MaxLogCount       int     `toml:"max_log_count"`
		CLIPath           string  `toml:"cli_path"`
	} `toml:"summarizer"`
}

// DefaultConfigPath returns `~/.apogee/config.toml`. When the home directory
// cannot be resolved we fall back to an empty string so Load silently skips
// the file step.
func DefaultConfigPath() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ""
	}
	return filepath.Join(home, ".apogee", "config.toml")
}

// Load reads the TOML file at path (absent files are fine), overlays
// environment variables, and returns a Config ready to feed into NewService.
// Resolution order per field: env > toml > default.
func Load(path string) (Config, error) {
	cfg := Default()
	if path == "" {
		path = DefaultConfigPath()
	}
	if path != "" {
		if _, err := os.Stat(path); err == nil {
			var tf tomlFile
			if _, err := toml.DecodeFile(path, &tf); err != nil {
				return cfg, fmt.Errorf("summarizer: decode %s: %w", path, err)
			}
			applyTOML(&cfg, tf)
			cfg.ConfigPath = path
		} else if !os.IsNotExist(err) {
			return cfg, fmt.Errorf("summarizer: stat %s: %w", path, err)
		}
	}
	applyEnv(&cfg)
	return cfg, nil
}

func applyTOML(cfg *Config, tf tomlFile) {
	s := tf.Summarizer
	if s.Enabled != nil {
		cfg.Enabled = *s.Enabled
	}
	if v := strings.TrimSpace(s.RecapModel); v != "" {
		cfg.RecapModel = v
	}
	if v := strings.TrimSpace(s.RollupModel); v != "" {
		cfg.RollupModel = v
	}
	if v := strings.TrimSpace(s.NarrativeModel); v != "" {
		cfg.NarrativeModel = v
	}
	if s.Concurrency > 0 {
		cfg.Concurrency = s.Concurrency
	}
	if s.TimeoutSeconds > 0 {
		cfg.Timeout = time.Duration(s.TimeoutSeconds * float64(time.Second))
	}
	if s.QueueSize > 0 {
		cfg.QueueSize = s.QueueSize
	}
	if s.MinTurnDurationMs > 0 {
		cfg.MinTurnDurationMs = s.MinTurnDurationMs
	}
	if s.MaxSpanCount > 0 {
		cfg.MaxSpanCount = s.MaxSpanCount
	}
	if s.MaxLogCount > 0 {
		cfg.MaxLogCount = s.MaxLogCount
	}
	if v := strings.TrimSpace(s.CLIPath); v != "" {
		cfg.CLIPath = v
	}
}

func applyEnv(cfg *Config) {
	if v := strings.TrimSpace(os.Getenv("APOGEE_SUMMARIZER_ENABLED")); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			cfg.Enabled = b
		}
	}
	if v := strings.TrimSpace(os.Getenv("APOGEE_RECAP_MODEL")); v != "" {
		cfg.RecapModel = v
	}
	if v := strings.TrimSpace(os.Getenv("APOGEE_ROLLUP_MODEL")); v != "" {
		cfg.RollupModel = v
	}
	if v := strings.TrimSpace(os.Getenv("APOGEE_NARRATIVE_MODEL")); v != "" {
		cfg.NarrativeModel = v
	}
	if v := strings.TrimSpace(os.Getenv("APOGEE_SUMMARIZER_CONCURRENCY")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			cfg.Concurrency = n
		}
	}
	if v := strings.TrimSpace(os.Getenv("APOGEE_SUMMARIZER_TIMEOUT_SECONDS")); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil && f > 0 {
			cfg.Timeout = time.Duration(f * float64(time.Second))
		}
	}
	if v := strings.TrimSpace(os.Getenv("APOGEE_SUMMARIZER_CLI_PATH")); v != "" {
		cfg.CLIPath = v
	}
	if v := strings.TrimSpace(os.Getenv("APOGEE_SUMMARIZER_QUEUE_SIZE")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			cfg.QueueSize = n
		}
	}
	if v := strings.TrimSpace(os.Getenv("APOGEE_SUMMARIZER_MIN_TURN_MS")); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil && n >= 0 {
			cfg.MinTurnDurationMs = n
		}
	}
	if v := strings.TrimSpace(os.Getenv("APOGEE_SUMMARIZER_MAX_SPANS")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			cfg.MaxSpanCount = n
		}
	}
	if v := strings.TrimSpace(os.Getenv("APOGEE_SUMMARIZER_MAX_LOGS")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			cfg.MaxLogCount = n
		}
	}
}

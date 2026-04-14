package interventions

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
)

// tomlFile is the on-disk shape the interventions service reads out of
// `~/.apogee/config.toml`. Only the [interventions] table is consulted.
type tomlFile struct {
	Interventions struct {
		AutoExpireTTLSeconds     float64 `toml:"auto_expire_ttl_seconds"`
		SweepIntervalSeconds     float64 `toml:"sweep_interval_seconds"`
		BothFallbackAfterSeconds float64 `toml:"both_fallback_after_seconds"`
		MaxMessageChars          int     `toml:"max_message_chars"`
	} `toml:"interventions"`
}

// DefaultConfigPath returns `~/.apogee/config.toml`. Empty string when the
// home directory cannot be resolved.
func DefaultConfigPath() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ""
	}
	return filepath.Join(home, ".apogee", "config.toml")
}

// Load reads the TOML file at path (absent files are fine), overlays
// environment variables, and returns a Config. Resolution order per field:
// env > toml > default.
func Load(path string) (Config, error) {
	cfg := DefaultConfig()
	if path == "" {
		path = DefaultConfigPath()
	}
	if path != "" {
		if _, err := os.Stat(path); err == nil {
			var tf tomlFile
			if _, err := toml.DecodeFile(path, &tf); err != nil {
				return cfg, fmt.Errorf("interventions: decode %s: %w", path, err)
			}
			applyTOML(&cfg, tf)
		} else if !os.IsNotExist(err) {
			return cfg, fmt.Errorf("interventions: stat %s: %w", path, err)
		}
	}
	applyEnv(&cfg)
	return cfg, nil
}

func applyTOML(cfg *Config, tf tomlFile) {
	it := tf.Interventions
	if it.AutoExpireTTLSeconds > 0 {
		cfg.AutoExpireTTL = time.Duration(it.AutoExpireTTLSeconds * float64(time.Second))
	}
	if it.SweepIntervalSeconds > 0 {
		cfg.SweepInterval = time.Duration(it.SweepIntervalSeconds * float64(time.Second))
	}
	if it.BothFallbackAfterSeconds > 0 {
		cfg.BothFallbackAfter = time.Duration(it.BothFallbackAfterSeconds * float64(time.Second))
	}
	if it.MaxMessageChars > 0 {
		cfg.MaxMessageChars = it.MaxMessageChars
	}
}

func applyEnv(cfg *Config) {
	if v := strings.TrimSpace(os.Getenv("APOGEE_INTERVENTIONS_TTL_SECONDS")); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil && f > 0 {
			cfg.AutoExpireTTL = time.Duration(f * float64(time.Second))
		}
	}
	if v := strings.TrimSpace(os.Getenv("APOGEE_INTERVENTIONS_SWEEP_SECONDS")); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil && f > 0 {
			cfg.SweepInterval = time.Duration(f * float64(time.Second))
		}
	}
	if v := strings.TrimSpace(os.Getenv("APOGEE_INTERVENTIONS_BOTH_FALLBACK_SECONDS")); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil && f > 0 {
			cfg.BothFallbackAfter = time.Duration(f * float64(time.Second))
		}
	}
	if v := strings.TrimSpace(os.Getenv("APOGEE_INTERVENTIONS_MAX_MESSAGE_CHARS")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			cfg.MaxMessageChars = n
		}
	}
}

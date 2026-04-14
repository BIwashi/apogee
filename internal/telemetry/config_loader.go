package telemetry

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
)

// tomlFile mirrors the [telemetry] section of `~/.apogee/config.toml`.
// Only fields apogee understands are decoded; unknown keys are left
// untouched so adjacent sections (summarizer, etc) keep working.
type tomlFile struct {
	Telemetry struct {
		Enabled        *bool             `toml:"enabled"`
		Endpoint       string            `toml:"endpoint"`
		Protocol       string            `toml:"protocol"`
		Insecure       *bool             `toml:"insecure"`
		SampleRatio    *float64          `toml:"sample_ratio"`
		ServiceName    string            `toml:"service_name"`
		ServiceVersion string            `toml:"service_version"`
		Headers        map[string]string `toml:"headers"`
		Resource       map[string]string `toml:"resource"`
	} `toml:"telemetry"`
}

// DefaultConfigPath returns `~/.apogee/config.toml` for callers that
// want to honour the global apogee config file. Empty string when the
// user home directory cannot be resolved.
func DefaultConfigPath() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ""
	}
	return filepath.Join(home, ".apogee", "config.toml")
}

// LoadConfig resolves the telemetry config from defaults, the TOML
// file at path (if present) and the standard OTel/apogee env vars, in
// that order. Env > TOML > defaults.
//
// path may be empty, in which case DefaultConfigPath() is used. A
// missing TOML file is not an error.
func LoadConfig(path string) (Config, error) {
	cfg := defaultConfig()
	if path == "" {
		path = DefaultConfigPath()
	}
	if path != "" {
		if _, err := os.Stat(path); err == nil {
			var tf tomlFile
			if _, err := toml.DecodeFile(path, &tf); err != nil {
				return cfg, fmt.Errorf("telemetry: decode %s: %w", path, err)
			}
			applyTOML(&cfg, tf)
		} else if !os.IsNotExist(err) {
			return cfg, fmt.Errorf("telemetry: stat %s: %w", path, err)
		}
	}
	envCfg := LoadConfigFromEnv()
	overlayEnv(&cfg, envCfg)
	return cfg, nil
}

// defaultConfig is the in-process zero+sane-defaults Config used as
// the seed for both LoadConfig and LoadConfigFromEnv.
func defaultConfig() Config {
	return Config{
		Protocol:      ProtocolGRPC,
		ServiceName:   "apogee",
		SampleRatio:   1.0,
		Headers:       map[string]string{},
		ResourceAttrs: map[string]string{},
	}
}

func applyTOML(cfg *Config, tf tomlFile) {
	t := tf.Telemetry
	if t.Enabled != nil {
		cfg.Enabled = *t.Enabled
	}
	if v := strings.TrimSpace(t.Endpoint); v != "" {
		cfg.Endpoint = v
		// Endpoint presence implicitly enables export unless the file
		// explicitly says otherwise.
		if t.Enabled == nil {
			cfg.Enabled = true
		}
	}
	if v := strings.TrimSpace(t.Protocol); v != "" {
		cfg.Protocol = normaliseProtocol(v)
	}
	if t.Insecure != nil {
		cfg.Insecure = *t.Insecure
	}
	if t.SampleRatio != nil {
		cfg.SampleRatio = clamp01(*t.SampleRatio)
	}
	if v := strings.TrimSpace(t.ServiceName); v != "" {
		cfg.ServiceName = v
	}
	if v := strings.TrimSpace(t.ServiceVersion); v != "" {
		cfg.ServiceVersion = v
	}
	if cfg.Headers == nil {
		cfg.Headers = map[string]string{}
	}
	for k, v := range t.Headers {
		cfg.Headers[k] = v
	}
	if cfg.ResourceAttrs == nil {
		cfg.ResourceAttrs = map[string]string{}
	}
	for k, v := range t.Resource {
		cfg.ResourceAttrs[k] = v
	}
}

// overlayEnv layers env-derived values on top of cfg. Only fields that
// the env actually supplied are copied — empty values do not clear an
// existing TOML setting.
func overlayEnv(cfg *Config, env Config) {
	if env.Endpoint != "" {
		cfg.Endpoint = env.Endpoint
		cfg.Enabled = true
	}
	if env.Protocol != "" {
		cfg.Protocol = env.Protocol
	}
	if env.Insecure {
		cfg.Insecure = true
	}
	if env.ServiceName != "" && env.ServiceName != "apogee" {
		cfg.ServiceName = env.ServiceName
	} else if cfg.ServiceName == "" {
		cfg.ServiceName = env.ServiceName
	}
	if env.SampleRatio > 0 && env.SampleRatio != 1.0 {
		cfg.SampleRatio = env.SampleRatio
	}
	if env.ServiceVersion != "" {
		cfg.ServiceVersion = env.ServiceVersion
	}
	if cfg.Headers == nil {
		cfg.Headers = map[string]string{}
	}
	for k, v := range env.Headers {
		cfg.Headers[k] = v
	}
	if cfg.ResourceAttrs == nil {
		cfg.ResourceAttrs = map[string]string{}
	}
	for k, v := range env.ResourceAttrs {
		cfg.ResourceAttrs[k] = v
	}
	if env.ServiceInstanceID != "" {
		cfg.ServiceInstanceID = env.ServiceInstanceID
	}
	// APOGEE_OTLP_ENABLED — already baked into env.Enabled by
	// LoadConfigFromEnv. We only honour it as an override when the
	// caller actually set the env var, which we detect by comparing
	// the env Endpoint state with the explicit toggle.
	if v := strings.TrimSpace(os.Getenv("APOGEE_OTLP_ENABLED")); v != "" {
		// LoadConfigFromEnv already parsed it; mirror the result.
		cfg.Enabled = env.Enabled
	}
}

package cli

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/BIwashi/apogee/internal/daemon"
)

// daemonDefaults captures the default knobs used by `apogee daemon
// install` when flags are not supplied. These mirror the
// ~/.apogee/config.toml [daemon] block documented in the README.
type daemonDefaults struct {
	Label     string
	Addr      string
	DBPath    string
	LogDir    string
	WorkDir   string
	KeepAlive bool
	RunAtLoad bool
}

// defaultDaemonConfig returns the static defaults. PR #22's onboard
// wizard will extend this to parse ~/.apogee/config.toml when the
// user provides one; for PR #21 we ship the constants directly so
// the install subcommand has everything it needs.
func defaultDaemonConfig() daemonDefaults {
	return daemonDefaults{
		Label:     daemon.DefaultLabel,
		Addr:      daemon.DefaultAddr,
		DBPath:    "~/.apogee/apogee.duckdb",
		LogDir:    "~/.apogee/logs",
		WorkDir:   "~/.apogee",
		KeepAlive: true,
		RunAtLoad: true,
	}
}

// resolveDaemonConfig turns the defaults + user overrides into a
// daemon.Config ready to hand to Manager.Install. Every ~-prefixed
// path is expanded using the existing expandHome helper.
func resolveDaemonConfig(label, addr, dbPath string) (daemon.Config, error) {
	d := defaultDaemonConfig()
	if label != "" {
		d.Label = label
	}
	if addr != "" {
		d.Addr = addr
	}
	if dbPath != "" {
		d.DBPath = dbPath
	}

	resolvedDB, err := expandHome(d.DBPath)
	if err != nil {
		return daemon.Config{}, err
	}
	resolvedLogs, err := expandHome(d.LogDir)
	if err != nil {
		return daemon.Config{}, err
	}
	resolvedWork, err := expandHome(d.WorkDir)
	if err != nil {
		return daemon.Config{}, err
	}

	binary := ""
	if exe, err := os.Executable(); err == nil {
		if abs, err := filepath.Abs(exe); err == nil && abs != "" {
			binary = abs
		} else {
			binary = exe
		}
	}

	env := map[string]string{}
	if home, err := os.UserHomeDir(); err == nil {
		env["HOME"] = home
	}

	return daemon.Config{
		Label:      d.Label,
		BinaryPath: binary,
		Args: []string{
			"serve",
			"--addr", d.Addr,
			"--db", resolvedDB,
		},
		WorkingDir:  resolvedWork,
		LogDir:      resolvedLogs,
		Environment: env,
		KeepAlive:   d.KeepAlive,
		RunAtLoad:   d.RunAtLoad,
	}, nil
}

// collectorHealth POSTs a 1-second GET /v1/healthz probe and reports
// ok / latency / error. The result is rendered inline by
// daemon/status commands.
type collectorHealth struct {
	OK      bool
	Status  int
	Latency time.Duration
	Err     error
}

func probeCollector(addr string) collectorHealth {
	if addr == "" {
		addr = daemon.DefaultAddr
	}
	url := "http://" + addr + "/v1/healthz"
	client := &http.Client{Timeout: time.Second}
	start := time.Now()
	resp, err := client.Get(url)
	latency := time.Since(start)
	if err != nil {
		return collectorHealth{OK: false, Latency: latency, Err: err}
	}
	defer func() { _ = resp.Body.Close() }()
	_, _ = io.Copy(io.Discard, resp.Body)
	return collectorHealth{
		OK:      resp.StatusCode == http.StatusOK,
		Status:  resp.StatusCode,
		Latency: latency,
	}
}

// formatCollectorLine renders a single-line summary of a
// collectorHealth result.
func formatCollectorLine(h collectorHealth) string {
	if h.Err != nil {
		return fmt.Sprintf("unreachable (%s)", h.Err)
	}
	if !h.OK {
		return fmt.Sprintf("HTTP %d", h.Status)
	}
	return fmt.Sprintf("ok (HTTP %d, %d ms)", h.Status, h.Latency.Milliseconds())
}

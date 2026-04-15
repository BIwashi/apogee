package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/spf13/cobra"

	"github.com/BIwashi/apogee/internal/daemon"
)

// NewStatusCmd returns the `apogee status` subcommand: a top-level
// summary wrapping the daemon status, the collector health probe,
// and a 1-line activity snapshot drawn from the collector's
// attention and turn endpoints.
func NewStatusCmd(stdout, stderr io.Writer) *cobra.Command {
	var addr string
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show the apogee daemon and collector status at a glance",
		Long: `Print a high-level summary:

  - whether the background daemon is installed, loaded, and running;
  - the collector's /v1/healthz probe result;
  - a 1-line activity summary from the collector's read API.

This is the command to eyeball when you want to know if apogee is
alive. For the raw details, use ` + "`apogee daemon status`" + `.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			m, err := managerFactory()
			if err != nil {
				return err
			}
			s, _ := m.Status(cmd.Context())

			fmt.Fprintln(stdout, "APOGEE STATUS")
			fmt.Fprintln(stdout)

			if s.Running {
				uptime := "unknown"
				if !s.StartedAt.IsZero() {
					uptime = s.Uptime().Round(time.Second).String()
				}
				if s.PID > 0 {
					fmt.Fprintf(stdout, "Daemon:    running (pid %d, uptime %s)\n", s.PID, uptime)
				} else {
					fmt.Fprintf(stdout, "Daemon:    running (uptime %s)\n", uptime)
				}
			} else if s.Installed {
				fmt.Fprintln(stdout, "Daemon:    installed, not running")
			} else {
				fmt.Fprintln(stdout, "Daemon:    not installed")
			}

			h := probeCollector(addr)
			fmt.Fprintf(stdout, "Collector: http://%s (%s)\n", addr, formatCollectorLine(h))

			activity := probeActivity(addr, h.OK)
			fmt.Fprintf(stdout, "Activity:  %s\n", activity)

			fmt.Fprintln(stdout)
			fmt.Fprintln(stdout, "Run `apogee logs` to tail the collector logs.")
			fmt.Fprintln(stdout, "Run `apogee open` to open the dashboard.")
			return nil
		},
	}
	cmd.Flags().StringVar(&addr, "addr", daemon.DefaultAddr, "Collector address to probe")
	return cmd
}

// probeActivity asks the collector for a coarse-grained turn /
// attention rollup. Falls back to a placeholder when the collector
// is offline or the probe errors.
func probeActivity(addr string, collectorOK bool) string {
	if !collectorOK {
		return "(collector offline)"
	}
	client := &http.Client{Timeout: time.Second}
	turns := -1
	active := -1
	intervene := -1

	if resp, err := client.Get("http://" + addr + "/v1/attention/counts"); err == nil {
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode == http.StatusOK {
			var payload map[string]any
			if err := json.NewDecoder(resp.Body).Decode(&payload); err == nil {
				if v, ok := intField(payload, "intervene_now"); ok {
					intervene = v
				}
			}
		}
	}
	if resp, err := client.Get("http://" + addr + "/v1/turns/active"); err == nil {
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode == http.StatusOK {
			var payload any
			if err := json.NewDecoder(resp.Body).Decode(&payload); err == nil {
				if arr, ok := payload.([]any); ok {
					active = len(arr)
				} else if m, ok := payload.(map[string]any); ok {
					if v, ok := intField(m, "active"); ok {
						active = v
					}
					if v, ok := intField(m, "turns"); ok {
						turns = v
					}
				}
			}
		}
	}

	parts := []string{}
	if active >= 0 {
		parts = append(parts, fmt.Sprintf("%d active turns", active))
	}
	if turns >= 0 {
		parts = append(parts, fmt.Sprintf("%d sessions", turns))
	}
	if intervene >= 0 {
		parts = append(parts, fmt.Sprintf("%d intervene_now", intervene))
	}
	if len(parts) == 0 {
		return "(no data)"
	}
	return join(parts, " · ")
}

func intField(m map[string]any, key string) (int, bool) {
	v, ok := m[key]
	if !ok {
		return 0, false
	}
	switch x := v.(type) {
	case float64:
		return int(x), true
	case int:
		return x, true
	case int64:
		return int(x), true
	}
	return 0, false
}

func join(parts []string, sep string) string {
	if len(parts) == 0 {
		return ""
	}
	out := parts[0]
	for _, p := range parts[1:] {
		out += sep + p
	}
	return out
}

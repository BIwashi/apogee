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
			stdout := styledWriter(stdout)
			m, err := managerFactory()
			if err != nil {
				return err
			}
			s, _ := m.Status(cmd.Context())

			fmt.Fprintln(stdout, renderHeading("APOGEE STATUS"))
			fmt.Fprintln(stdout)

			// Daemon section: keep the "Daemon:    <state>" prefix
			// the existing tests assert on, while adding a styled
			// box underneath with the full breakdown.
			daemonLine := daemonOneLiner(s)
			fmt.Fprintln(stdout, daemonLine)
			fmt.Fprintln(stdout, daemonBox(m, s))
			fmt.Fprintln(stdout)

			h := probeCollector(addr)
			fmt.Fprintf(stdout, "Collector: http://%s (%s)\n", addr, formatCollectorLine(h))
			fmt.Fprintln(stdout, collectorBox(addr, h))
			fmt.Fprintln(stdout)

			activity := probeActivity(addr, h.OK)
			fmt.Fprintln(stdout, renderHeading("Activity"))
			fmt.Fprintf(stdout, "Activity:  %s\n", activity)
			fmt.Fprintln(stdout, boxInfo.Render(keyValueLines([][2]string{
				{"Snapshot", activity},
			})))

			fmt.Fprintln(stdout)
			fmt.Fprintln(stdout, styleMuted.Render("Run `apogee logs` to tail the collector logs."))
			fmt.Fprintln(stdout, styleMuted.Render("Run `apogee open` to open the dashboard."))
			return nil
		},
	}
	cmd.Flags().StringVar(&addr, "addr", daemon.DefaultAddr, "Collector address to probe")
	return cmd
}

// daemonOneLiner returns the legacy "Daemon:    <state>" string
// used as a prefix line above the styled daemon box. Kept stable so
// existing tests can keep matching on it.
func daemonOneLiner(s daemon.Status) string {
	if s.Running {
		uptime := "unknown"
		if !s.StartedAt.IsZero() {
			uptime = s.Uptime().Round(time.Second).String()
		}
		if s.PID > 0 {
			return fmt.Sprintf("Daemon:    running (pid %d, uptime %s)", s.PID, uptime)
		}
		return fmt.Sprintf("Daemon:    running (uptime %s)", uptime)
	}
	if s.Installed {
		return "Daemon:    installed, not running"
	}
	return "Daemon:    not installed"
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

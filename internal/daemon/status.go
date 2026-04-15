package daemon

import (
	"fmt"
	"strings"
	"time"
)

// FormatStatus renders a Status into a human-readable multi-line
// string. Used by `apogee daemon status` and `apogee status`.
// Styling (lipgloss) is applied by the caller; this function keeps
// the plain text shape stable for tests and for machine parsing.
func FormatStatus(s Status) string {
	var b strings.Builder
	label := s.Label
	if label == "" {
		label = DefaultLabel
	}
	fmt.Fprintf(&b, "Daemon: %s\n", label)
	fmt.Fprintf(&b, "  Installed:    %s\n", yesNo(s.Installed))
	fmt.Fprintf(&b, "  Loaded:       %s\n", yesNo(s.Loaded))
	if s.Running {
		if s.PID > 0 {
			fmt.Fprintf(&b, "  Running:      yes (pid %d)\n", s.PID)
		} else {
			fmt.Fprintln(&b, "  Running:      yes")
		}
	} else {
		fmt.Fprintln(&b, "  Running:      no")
	}
	if !s.StartedAt.IsZero() {
		fmt.Fprintf(&b, "  Started at:   %s (uptime %s)\n",
			s.StartedAt.Format("2006-01-02 15:04:05"),
			formatUptime(s.Uptime()),
		)
	}
	fmt.Fprintf(&b, "  Last exit:    %d\n", s.LastExitCode)
	if s.UnitPath != "" {
		fmt.Fprintf(&b, "  Unit path:    %s\n", s.UnitPath)
	}
	return b.String()
}

func yesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}

// formatUptime renders a duration as "Xh Ym Zs" style, trimming
// leading zero components. 0 → "0s".
func formatUptime(d time.Duration) string {
	if d <= 0 {
		return "0s"
	}
	d = d.Round(time.Second)
	h := int(d / time.Hour)
	d -= time.Duration(h) * time.Hour
	m := int(d / time.Minute)
	d -= time.Duration(m) * time.Minute
	s := int(d / time.Second)
	switch {
	case h > 0:
		return fmt.Sprintf("%dh %dm %ds", h, m, s)
	case m > 0:
		return fmt.Sprintf("%dm %02ds", m, s)
	default:
		return fmt.Sprintf("%ds", s)
	}
}

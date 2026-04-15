package cli

import (
	"fmt"
	"io"
	"os"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/colorprofile"
)

// styledWriter wraps w with a colorprofile-aware writer that strips
// ANSI when the destination is not a TTY or when NO_COLOR is set in
// the environment. The returned writer is safe for the lipgloss
// styled output produced by this package.
func styledWriter(w io.Writer) io.Writer {
	if w == nil {
		return os.Stdout
	}
	return colorprofile.NewWriter(w, os.Environ())
}

// theme.go holds the lipgloss style vocabulary used by the daemon /
// status / doctor subcommands. The styles map to the apogee design
// tokens documented in docs/design-tokens.md and intentionally use
// hex colors (not ANSI palette indexes) so the output looks the same
// across iTerm2, VS Code, and the macOS Terminal. lipgloss already
// degrades cleanly when NO_COLOR is set or stdout is not a TTY, so
// callers do not need to branch.

var (
	styleHeading = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#27AAE1"))
	styleSuccess = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#27E0A1"))
	styleWarn    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#E08B27"))
	styleError   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#FC3D21"))
	styleMuted   = lipgloss.NewStyle().Foreground(lipgloss.Color("#8892a6"))
	styleKey     = lipgloss.NewStyle().Foreground(lipgloss.Color("#A7A9AC"))
	styleValue   = lipgloss.NewStyle().Foreground(lipgloss.Color("#FFFFFF"))

	boxSuccess = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("#27E0A1")).
			Padding(0, 1)

	boxError = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("#FC3D21")).
			Padding(0, 1)

	boxInfo = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("#27AAE1")).
			Padding(0, 1)

	boxWarn = lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#E08B27")).
		Padding(0, 1)
)

// keyValueLines renders a two-column key:value block aligned on the
// longest key. Keys are styled with styleKey, values with styleValue.
// Empty entries render as a literal blank line so callers can use
// them as visual separators inside a box.
func keyValueLines(entries [][2]string) string {
	maxKey := 0
	for _, e := range entries {
		if e[0] == "" {
			continue
		}
		if n := lipgloss.Width(e[0]); n > maxKey {
			maxKey = n
		}
	}
	var b strings.Builder
	for i, e := range entries {
		if i > 0 {
			b.WriteByte('\n')
		}
		if e[0] == "" && e[1] == "" {
			continue
		}
		pad := strings.Repeat(" ", maxKey-lipgloss.Width(e[0]))
		b.WriteString(styleKey.Render(e[0] + ":"))
		b.WriteString(pad)
		b.WriteString("  ")
		b.WriteString(styleValue.Render(e[1]))
	}
	return b.String()
}

// statusBadge returns a short status token like "running" / "stopped"
// / "errored" / "not installed" with the appropriate colour applied.
// Anything unknown is rendered with styleMuted so we never crash on
// a typo.
func statusBadge(state string) string {
	switch strings.ToLower(strings.TrimSpace(state)) {
	case "running", "ok", "healthy", "installed":
		return styleSuccess.Render(state)
	case "stopped", "not running", "unreachable", "warn", "warning", "partial", "missing":
		return styleWarn.Render(state)
	case "errored", "error", "failed", "fatal":
		return styleError.Render(state)
	case "not installed", "uninstalled":
		return styleMuted.Render(state)
	default:
		return styleMuted.Render(state)
	}
}

// glyph constants mirror the ones in daemon.go but live here so the
// theme pack is self-contained. They are NOT emoji — U+2713,
// U+2717, U+26A0 — and pass the design-system check.
const (
	glyphCheck = "\u2713" // ✓
	glyphCross = "\u2717" // ✗
	glyphWarn  = "\u26A0" // ⚠
)

// styleGlyph wraps a glyph in the matching colour for its severity.
func styleGlyph(severity string) string {
	switch strings.ToLower(severity) {
	case "ok", "success":
		return styleSuccess.Render(glyphCheck)
	case "warn", "warning":
		return styleWarn.Render(glyphWarn)
	case "error", "fail":
		return styleError.Render(glyphCross)
	case "info":
		return styleHeading.Render(glyphCheck)
	default:
		return styleMuted.Render("·")
	}
}

// renderHeading returns a bold styled section heading suitable for
// the top of a multi-section status page.
func renderHeading(text string) string {
	return styleHeading.Render(text)
}

// renderBoxLabelled wraps body inside a coloured rounded border
// matching kind (success | error | info | warn). The first line of
// body is treated as the section heading and rendered bold.
func renderBoxLabelled(kind, heading, body string) string {
	var box lipgloss.Style
	switch kind {
	case "success":
		box = boxSuccess
	case "error":
		box = boxError
	case "warn":
		box = boxWarn
	default:
		box = boxInfo
	}
	inner := heading
	if heading != "" && body != "" {
		inner = styleHeading.Render(heading) + "\n\n" + body
	} else if heading != "" {
		inner = styleHeading.Render(heading)
	} else {
		inner = body
	}
	return box.Render(inner)
}

// formatBytesLine formats a key/value pair as a single styled line,
// suitable for a one-shot confirmation message ("daemon started").
func formatStatusLine(severity, message string) string {
	return fmt.Sprintf("%s %s", styleGlyph(severity), message)
}

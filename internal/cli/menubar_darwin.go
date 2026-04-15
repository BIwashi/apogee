//go:build darwin

package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"sync"
	"time"

	"github.com/caseymrm/menuet"
	"github.com/spf13/cobra"
)

// NewMenubarCmd returns the `apogee menubar` subcommand. It runs a native
// macOS status item (via caseymrm/menuet) that polls the local apogee
// collector and renders live counts in the menu bar.
//
// The menu bar process is a passive observer: it polls the collector's HTTP
// surface (`/v1/healthz`, `/v1/turns/active`, `/v1/attention/counts`) on a
// fixed interval and renders the result. It does not talk to launchd/systemd
// directly; the "Restart daemon" action shells out to `apogee daemon
// restart`.
func NewMenubarCmd(stdout, stderr io.Writer) *cobra.Command {
	var (
		addr          string
		refreshSecond int
	)
	cmd := &cobra.Command{
		Use:   "menubar",
		Short: "Run the apogee macOS menu bar companion",
		Long: `menubar runs a macOS status item that polls the local apogee
collector and renders live counts in the menu bar. Click the glyph to see
daemon status and quick actions.

Start it manually with:

    apogee menubar &

Or register it as a login item via the onboarding wizard.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runMenubar(cmd.Context(), menubarOptions{
				CollectorBase: "http://" + addr,
				Refresh:       time.Duration(refreshSecond) * time.Second,
				Stdout:        stdout,
				Stderr:        stderr,
			})
		},
	}
	cmd.Flags().StringVar(&addr, "addr", "127.0.0.1:4100", "Collector address to poll")
	cmd.Flags().IntVar(&refreshSecond, "refresh", 5, "Refresh interval in seconds")
	out := styledWriter(stdout)
	cmd.AddCommand(newMenubarInstallCmd(out, stderr))
	cmd.AddCommand(newMenubarUninstallCmd(out, stderr))
	cmd.AddCommand(newMenubarStatusCmd(out, stderr))
	return cmd
}

type menubarOptions struct {
	CollectorBase string
	Refresh       time.Duration
	Stdout        io.Writer
	Stderr        io.Writer
}

// menubarState is the mutable snapshot of what the menu bar should render.
// It is written by the poll loop and read by the menuet render callback,
// so every field access is guarded by the embedded RWMutex.
type menubarState struct {
	mu sync.RWMutex

	collectorOK bool
	sessions    int
	activeTurns int
	intervene   int
	lastError   string
	lastUpdate  time.Time
}

func (s *menubarState) snapshot() menubarSnapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return menubarSnapshot{
		CollectorOK: s.collectorOK,
		Sessions:    s.sessions,
		ActiveTurns: s.activeTurns,
		Intervene:   s.intervene,
		LastError:   s.lastError,
		LastUpdate:  s.lastUpdate,
	}
}

// menubarSnapshot is the immutable view handed to the menu renderer so the
// render callback never needs to touch the state mutex.
type menubarSnapshot struct {
	CollectorOK bool
	Sessions    int
	ActiveTurns int
	Intervene   int
	LastError   string
	LastUpdate  time.Time
}

func runMenubar(ctx context.Context, opts menubarOptions) error {
	state := &menubarState{}
	httpClient := &http.Client{Timeout: 1500 * time.Millisecond}

	refresh := func() {
		snap := pollCollector(httpClient, opts.CollectorBase)
		state.mu.Lock()
		state.collectorOK = snap.collectorOK
		state.sessions = snap.sessions
		state.activeTurns = snap.activeTurns
		state.intervene = snap.intervene
		state.lastError = snap.lastError
		state.lastUpdate = time.Now()
		state.mu.Unlock()
		menuet.App().MenuChanged()
	}

	menuet.App().Label = "dev.biwashi.apogee.menubar"
	menuet.App().Children = func() []menuet.MenuItem {
		return buildMenu(state.snapshot(), opts.CollectorBase)
	}
	menuet.App().AutoUpdate.Version = "v1"
	menuet.App().SetMenuState(&menuet.MenuState{
		Title: glyphTitle(state.snapshot()),
	})

	// Background refresh loop. Kicks off an immediate poll so the first
	// render reflects real state instead of the zero value.
	ticker := time.NewTicker(opts.Refresh)
	go func() {
		refresh()
		menuet.App().SetMenuState(&menuet.MenuState{
			Title: glyphTitle(state.snapshot()),
		})
		for {
			select {
			case <-ctx.Done():
				ticker.Stop()
				return
			case <-ticker.C:
				refresh()
				menuet.App().SetMenuState(&menuet.MenuState{
					Title: glyphTitle(state.snapshot()),
				})
			}
		}
	}()

	menuet.App().RunApplication() // blocks until the user quits
	return nil
}

// glyphTitle picks the text rendered in the menu bar itself. The design
// system forbids emoji, so we use typographic glyphs (`●`, `▲`) that are
// bundled with every font. The goal is to communicate attention urgency at
// a glance — a filled triangle for intervene_now, a filled circle for
// running, and a literal "offline" label when the collector is unreachable.
func glyphTitle(s menubarSnapshot) string {
	if !s.CollectorOK {
		return "apogee · offline"
	}
	if s.Intervene > 0 {
		return fmt.Sprintf("apogee · ▲ %d", s.Intervene)
	}
	if s.ActiveTurns > 0 {
		return fmt.Sprintf("apogee · ● %d", s.ActiveTurns)
	}
	return "apogee · ●"
}

// buildMenu constructs the dropdown shown when the user clicks the glyph.
// The menu mirrors the product model in docs/menubar.md: a title, live
// status lines, numeric counts, and a handful of quick actions.
func buildMenu(s menubarSnapshot, base string) []menuet.MenuItem {
	items := []menuet.MenuItem{
		{Text: "apogee", FontSize: 14, FontWeight: menuet.WeightBold},
	}
	if s.CollectorOK {
		items = append(items,
			menuet.MenuItem{Text: "Status: running"},
			menuet.MenuItem{Text: "Collector: ok"},
		)
	} else {
		items = append(items,
			menuet.MenuItem{Text: "Status: offline"},
			menuet.MenuItem{Text: "Collector: unreachable"},
		)
		if s.LastError != "" {
			items = append(items, menuet.MenuItem{Text: "  " + s.LastError})
		}
	}
	items = append(items, menuet.MenuItem{Type: menuet.Separator})
	items = append(items,
		menuet.MenuItem{Text: fmt.Sprintf("%d sessions", s.Sessions)},
		menuet.MenuItem{Text: fmt.Sprintf("%d active turns", s.ActiveTurns)},
		menuet.MenuItem{Text: fmt.Sprintf("%d intervene_now", s.Intervene)},
	)
	items = append(items, menuet.MenuItem{Type: menuet.Separator})
	items = append(items,
		menuet.MenuItem{
			Text: "Open dashboard",
			// base comes from the statically-configured dashboard URL, not
			// user input; the subprocess target is the macOS `open` binary.
			Clicked: func() { _ = exec.Command("open", base+"/").Run() }, //nolint:gosec // see comment above
		},
		menuet.MenuItem{
			Text: "Open logs",
			// Expanded from a static "~/.apogee/logs" template.
			Clicked: func() { _ = exec.Command("open", expandHomeOrEmpty("~/.apogee/logs")).Run() }, //nolint:gosec // see comment above
		},
	)
	items = append(items, menuet.MenuItem{Type: menuet.Separator})
	items = append(items,
		menuet.MenuItem{
			Text:    "Restart daemon",
			Clicked: func() { _ = exec.Command("apogee", "daemon", "restart").Run() },
		},
	)
	items = append(items, menuet.MenuItem{Type: menuet.Separator})
	items = append(items, menuet.MenuItem{
		Text: "Quit menubar",
		// menuet does not expose a graceful-quit API, so we fall back to
		// tearing down the process. The Cocoa runloop owns the main
		// thread; os.Exit is the cleanest way out.
		Clicked: func() { os.Exit(0) },
	})
	return items
}

// pollSnapshot is the intermediate result from a single poll cycle. It is
// folded into menubarState under the mutex so readers always see a coherent
// picture.
type pollSnapshot struct {
	collectorOK bool
	sessions    int
	activeTurns int
	intervene   int
	lastError   string
}

// pollCollector performs one round of HTTP calls against the collector and
// returns a pollSnapshot. It never panics — any HTTP or decode failure is
// recorded as lastError and the partially-populated snapshot is returned so
// the caller can still render something useful.
func pollCollector(client *http.Client, base string) pollSnapshot {
	snap := pollSnapshot{}

	// Health check. If healthz is down we bail out early: the rest of
	// the endpoints are useless without a live collector.
	healthURL := base + "/v1/healthz"
	resp, err := client.Get(healthURL)
	if err != nil {
		snap.lastError = err.Error()
		return snap
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		snap.lastError = fmt.Sprintf("healthz status %d", resp.StatusCode)
		return snap
	}
	snap.collectorOK = true

	// Active turns. The endpoint returns {"turns": [...]}; we only need
	// the length, not the turn bodies themselves.
	resp, err = client.Get(base + "/v1/turns/active")
	if err == nil && resp.StatusCode == http.StatusOK {
		var body struct {
			Turns []json.RawMessage `json:"turns"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&body); err == nil {
			snap.activeTurns = len(body.Turns)
		}
		resp.Body.Close()
	}

	// Attention counts. Mirrors duckdb.AttentionCounts — we only read
	// intervene_now and total, but decoding into a typed struct keeps the
	// call site honest if extra buckets are added later.
	resp, err = client.Get(base + "/v1/attention/counts?include=running")
	if err == nil && resp.StatusCode == http.StatusOK {
		var body struct {
			InterveneNow int `json:"intervene_now"`
			Watch        int `json:"watch"`
			Watchlist    int `json:"watchlist"`
			Healthy      int `json:"healthy"`
			Total        int `json:"total"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&body); err == nil {
			snap.intervene = body.InterveneNow
			snap.sessions = body.Total
		}
		resp.Body.Close()
	}

	return snap
}

// expandHomeOrEmpty is a tiny wrapper around expandHome (from fsutil.go)
// that falls back to the original path on error. We never want to block
// the menu render on an unexpected home-directory lookup failure.
func expandHomeOrEmpty(p string) string {
	out, err := expandHome(p)
	if err != nil {
		return p
	}
	return out
}

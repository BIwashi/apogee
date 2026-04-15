package collector

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/BIwashi/apogee/internal/daemon"
)

// daemonRestartDelay is how long the handler waits before asking the
// supervisor to kickstart the unit. The delay exists so the HTTP
// response has time to flush before launchd / systemd sends SIGTERM
// to the current process. In production this is ~200ms; tests
// override it via the unexported package var below.
var daemonRestartDelay = 200 * time.Millisecond

// restartRunner is the hook tests use to replace the real daemon
// Manager with a fake. In production it defers to
// daemon.NewManagerWithLabel(...).Restart(ctx).
var restartRunner = func(ctx context.Context, label string) error {
	mgr, err := daemon.NewManagerWithLabel(label)
	if err != nil {
		return err
	}
	return mgr.Restart(ctx)
}

// postDaemonRestart handles POST /v1/daemon/restart. The endpoint is
// best-effort: it responds 202 immediately, then spawns a goroutine
// that waits a brief flush window and calls the OS supervisor's
// kickstart. If the supervisor is not available (e.g. `apogee serve`
// run directly in a shell), the goroutine's error is logged and the
// running process survives.
//
// Security note: the HTTP collector binds to 127.0.0.1 by default, so
// this endpoint is reachable only from localhost. The feature has no
// authentication beyond that — do not expose the collector port on an
// untrusted network.
func (s *Server) postDaemonRestart(w http.ResponseWriter, r *http.Request) {
	label := daemon.DefaultLabel
	// Fire-and-forget: launchctl kickstart -k kills the current pid,
	// so the handler has to return before that happens or the caller
	// sees a reset connection. We use a detached goroutine with a
	// fresh context since r.Context() is about to be cancelled when
	// the handler returns.
	go func() {
		time.Sleep(daemonRestartDelay)
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := restartRunner(ctx, label); err != nil {
			// Best-effort logging — the process may be killed
			// mid-log if the call actually succeeded despite
			// returning an error, which is fine.
			if errors.Is(err, daemon.ErrNotInstalled) {
				s.logger.Warn("daemon restart requested but unit is not installed", "label", label)
				return
			}
			s.logger.Error("daemon restart failed", "label", label, "err", err)
		}
	}()
	writeJSON(w, http.StatusAccepted, map[string]any{
		"status": "restart-requested",
		"label":  label,
	})
}

// Package version exposes the apogee build identifier.
//
// Values are injected at link time via -ldflags. Example:
//
//	go build -ldflags "\
//	  -X github.com/BIwashi/apogee/internal/version.Version=0.3.0 \
//	  -X github.com/BIwashi/apogee/internal/version.Commit=$(git rev-parse --short HEAD) \
//	  -X github.com/BIwashi/apogee/internal/version.BuildDate=$(date -u +%Y-%m-%dT%H:%M:%SZ)"
//
// Defaults are chosen so an un-stamped `go install ./cmd/apogee` still prints
// something sensible.
package version

import (
	"fmt"
	"runtime"
)

// These three vars are overridden at link time. Do not remove the var blocks
// or switch them to consts — ldflags can only rewrite string variables.
var (
	// Version is the current apogee release identifier.
	Version = "0.0.0-dev"
	// Commit is the short git SHA of the build.
	Commit = "unknown"
	// BuildDate is an RFC3339 timestamp set at build time.
	BuildDate = "unknown"
)

// Short returns a compact marketing string, e.g. "apogee v0.0.0-dev".
func Short() string {
	return fmt.Sprintf("apogee v%s", Version)
}

// Full returns a detailed build string, e.g.
// "apogee v0.0.0-dev (commit abc1234, built 2026-04-14T10:20:30Z, go1.25.0)".
func Full() string {
	return fmt.Sprintf(
		"apogee v%s (commit %s, built %s, %s)",
		Version, Commit, BuildDate, runtime.Version(),
	)
}

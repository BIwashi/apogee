//go:build !darwin

// Non-darwin stub for the desktop shell. The real entry point lives in
// main.go under //go:build darwin and depends on Wails v2 + WKWebView via
// cgo, so it cannot compile on linux/windows runners. This stub keeps
// `go build ./...` green on those platforms and prints a clear "macOS
// only" error when invoked, mirroring the pattern used for the menubar
// subcommand in internal/cli/menubar_other.go.
package main

import (
	"fmt"
	"os"
)

func main() {
	fmt.Fprintln(os.Stderr, "apogee desktop: the Wails desktop shell is currently macOS-only.")
	fmt.Fprintln(os.Stderr, "Run `apogee serve` and open http://localhost:4100 in a browser instead.")
	os.Exit(1)
}

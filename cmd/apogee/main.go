// Command apogee is the single-binary entry point for the apogee
// observability dashboard. In this scaffold it only prints the current version
// string; later PRs will wire in the collector, the embedded web UI, and the
// CLI subcommands.
package main

import (
	"fmt"

	"github.com/BIwashi/apogee/internal/version"
)

func main() {
	fmt.Printf("apogee %s\n", version.Version)
}

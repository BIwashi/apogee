// Command apogee is the single-binary entry point for the apogee
// observability dashboard. The CLI surface (serve, init, hooks, version,
// doctor) is defined in internal/cli; this file is just a thin wrapper that
// wires argv to the cobra root command so the binary stays testable.
package main

import (
	"fmt"
	"os"

	"github.com/BIwashi/apogee/internal/cli"
)

func main() {
	if err := cli.Execute(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "apogee:", err)
		os.Exit(1)
	}
}

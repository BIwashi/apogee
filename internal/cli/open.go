package cli

import (
	"fmt"
	"io"
	"os/exec"
	"runtime"

	"github.com/spf13/cobra"

	"github.com/BIwashi/apogee/internal/daemon"
)

// NewOpenCmd returns the `apogee open` subcommand: open the
// dashboard URL in the user's default browser. The URL defaults to
// http://127.0.0.1:4100/ but --addr overrides the host:port.
func NewOpenCmd(stdout, stderr io.Writer) *cobra.Command {
	var addr string
	cmd := &cobra.Command{
		Use:   "open",
		Short: "Open the apogee dashboard in the default browser",
		RunE: func(cmd *cobra.Command, _ []string) error {
			url := "http://" + addr + "/"
			return openURL(stdout, stderr, url)
		},
	}
	cmd.Flags().StringVar(&addr, "addr", daemon.DefaultAddr, "Dashboard address to open")
	return cmd
}

// openURL dispatches to the right OS helper. On unknown platforms
// it prints the URL so the user can open it manually.
func openURL(stdout, stderr io.Writer, url string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "linux":
		cmd = exec.Command("xdg-open", url)
	default:
		fmt.Fprintf(stdout, "Open this URL in your browser: %s\n", url)
		return nil
	}
	if err := cmd.Start(); err != nil {
		fmt.Fprintf(stderr, "apogee open: could not spawn browser: %v\n", err)
		fmt.Fprintf(stdout, "%s\n", url)
		return nil
	}
	// Don't wait — browser lifetime is independent of the CLI.
	go func() { _ = cmd.Wait() }()
	fmt.Fprintf(stdout, "Opening %s\n", url)
	return nil
}

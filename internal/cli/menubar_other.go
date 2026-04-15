//go:build !darwin

package cli

import (
	"io"

	"github.com/spf13/cobra"
)

// NewMenubarCmd returns a stub `apogee menubar` subcommand on non-darwin
// platforms. The real implementation lives in menubar_darwin.go and depends
// on Cocoa via cgo (caseymrm/menuet), so it cannot compile on linux/windows.
// The stub keeps `apogee --help` showing the subcommand across platforms
// and returns a clear "only supported on macOS" error if invoked.
func NewMenubarCmd(stdout, stderr io.Writer) *cobra.Command {
	return &cobra.Command{
		Use:   "menubar",
		Short: "Run the apogee macOS menu bar companion (macOS only)",
		RunE: func(cmd *cobra.Command, args []string) error {
			return errMenubarUnsupported
		},
	}
}

var errMenubarUnsupported = errMenubarOnlyMacOS{}

type errMenubarOnlyMacOS struct{}

func (errMenubarOnlyMacOS) Error() string {
	return "menubar: only supported on macOS"
}

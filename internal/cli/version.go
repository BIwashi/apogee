package cli

import (
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/BIwashi/apogee/internal/version"
)

// NewVersionCmd returns the `apogee version` subcommand. Unlike the global
// `--version` flag (which prints the short string), this command prints the
// full build record — commit, build date, and the runtime Go version.
func NewVersionCmd(stdout io.Writer) *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the apogee build version",
		RunE: func(_ *cobra.Command, _ []string) error {
			_, err := fmt.Fprintln(stdout, version.Full())
			return err
		},
	}
}

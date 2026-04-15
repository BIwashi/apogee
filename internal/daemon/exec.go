package daemon

import (
	"bytes"
	"context"
	"os/exec"
)

// runExec is the os/exec implementation of commandRunner.Run. It is
// extracted into its own file so tests that want to stub it (via an
// injected commandRunner) never actually call os/exec.
func runExec(ctx context.Context, name string, args ...string) (string, string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	return stdout.String(), stderr.String(), err
}

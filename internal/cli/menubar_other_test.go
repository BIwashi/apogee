//go:build !darwin

package cli

import (
	"testing"
)

// TestMenubarUnsupported verifies that the non-darwin stub returns a
// clear error instead of silently no-oping. macOS-only features must
// fail loudly on linux/windows so users know to run the real thing on
// a Mac.
func TestMenubarUnsupported(t *testing.T) {
	cmd := NewMenubarCmd(nil, nil)
	err := cmd.RunE(cmd, nil)
	if err == nil {
		t.Error("expected error on non-darwin")
	}
}

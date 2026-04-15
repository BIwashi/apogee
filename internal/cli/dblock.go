package cli

import (
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/BIwashi/apogee/internal/store/duckdb"
)

// dbLockHolder is a best-effort description of the process currently
// holding the DuckDB sidecar lock. Command is the path to the binary
// (or COMMAND column from lsof when no path is available); PID is
// the OS pid; both fields may be empty/zero when detection fails.
type dbLockHolder struct {
	Command string
	PID     int
}

// detectDBLockHolder runs `lsof -nP <path>` (best effort, 500 ms
// timeout) and parses the first PID + COMMAND row. When lsof is not
// installed or the probe fails it falls back to the sidecar pid file
// PID supplied by the caller.
func detectDBLockHolder(dbPath string, fallbackPID int) dbLockHolder {
	holder := dbLockHolder{PID: fallbackPID}
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	bin, err := exec.LookPath("lsof")
	if err != nil {
		// fall back to /proc on linux
		if fallbackPID > 0 {
			holder.Command = lookupCommandByPID(fallbackPID)
		}
		return holder
	}
	out, err := exec.CommandContext(ctx, bin, "-nP", dbPath).Output()
	if err != nil || len(out) == 0 {
		if fallbackPID > 0 {
			holder.Command = lookupCommandByPID(fallbackPID)
		}
		return holder
	}
	// lsof header line:
	// COMMAND   PID  USER ... NAME
	lines := strings.Split(string(out), "\n")
	for i, line := range lines {
		if i == 0 || strings.TrimSpace(line) == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		holder.Command = fields[0]
		if pid, err := strconv.Atoi(fields[1]); err == nil {
			holder.PID = pid
		}
		// First match wins.
		break
	}
	if holder.Command == "" && holder.PID > 0 {
		holder.Command = lookupCommandByPID(holder.PID)
	}
	return holder
}

// lookupCommandByPID returns the binary name of the given pid via
// `ps -p <pid> -o comm=`. Best-effort — empty string on any failure.
func lookupCommandByPID(pid int) string {
	if pid <= 0 {
		return ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
	defer cancel()
	// pid has already been parsed into an int; strconv.Itoa yields a
	// pure digit run so there is no user-tainted string flowing into
	// exec here.
	out, err := exec.CommandContext(ctx, "ps", "-p", strconv.Itoa(pid), "-o", "comm=").Output() //nolint:gosec // see comment above
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// renderDBLockConflict returns the styled box explaining a DuckDB
// lock conflict, with detected holder + remediation steps. The box
// uses the boxError style and degrades cleanly when NO_COLOR is set.
func renderDBLockConflict(locked *duckdb.LockedError) string {
	holder := detectDBLockHolder(locked.Path, locked.PID)

	holderText := "unknown"
	switch {
	case holder.Command != "" && holder.PID > 0:
		holderText = fmt.Sprintf("%s (pid %d)", holder.Command, holder.PID)
	case holder.PID > 0:
		holderText = fmt.Sprintf("pid %d", holder.PID)
	case holder.Command != "":
		holderText = holder.Command
	}

	pidForFix := holder.PID
	if pidForFix == 0 {
		pidForFix = locked.PID
	}

	body := keyValueLines([][2]string{
		{"Path", locked.Path},
		{"Holder", holderText},
	})

	fix := strings.Builder{}
	fix.WriteString(styleHeading.Render("To fix:"))
	fix.WriteString("\n")
	fix.WriteString("  1. apogee daemon stop\n")
	if pidForFix > 0 {
		fmt.Fprintf(&fix, "  2. or: kill %d\n", pidForFix)
	} else {
		fix.WriteString("  2. or: kill the process holding the lock\n")
	}
	fix.WriteString("  3. or: apogee serve --db <alt path>")

	heading := "Another apogee process is already using the DuckDB file."
	inner := styleError.Render(heading) + "\n\n" + body + "\n\n" + fix.String()
	return boxError.Render(inner)
}

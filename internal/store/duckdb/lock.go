package duckdb

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"syscall"
)

// ErrDBLocked is returned by CheckDBLockHolder when another apogee
// process already holds the sidecar lock for a DuckDB file. Callers
// can errors.Is(err, ErrDBLocked) to branch on this case and render a
// friendly remediation message instead of the raw driver error.
//
// The PID field is best-effort: it is read from the sidecar
// `<db>.apogee.pid` file the holder writes on Open. PID will be 0
// when the sidecar pid file is missing or unreadable.
var ErrDBLocked = errors.New("duckdb: database file is locked by another process")

// LockedError carries the path + PID context for an ErrDBLocked
// failure. Use errors.As(err, &lockErr) to retrieve the details.
type LockedError struct {
	Path string
	PID  int
}

// Error implements the error interface.
func (e *LockedError) Error() string {
	if e.PID > 0 {
		return fmt.Sprintf("duckdb: database file %s is locked by another apogee process (pid %d)", e.Path, e.PID)
	}
	return fmt.Sprintf("duckdb: database file %s is locked by another apogee process", e.Path)
}

// Unwrap returns the sentinel so errors.Is(err, ErrDBLocked) works.
func (e *LockedError) Unwrap() error { return ErrDBLocked }

// CheckDBLockHolder returns a *LockedError wrapping ErrDBLocked when
// another process already holds the DuckDB file's sidecar exclusive
// lock. Callers can errors.Is(err, ErrDBLocked) to branch.
//
// The implementation uses syscall.Flock on a sidecar `.apogee.lock`
// file so the probe itself does not touch the DuckDB file (which
// would otherwise hold its own lock the moment we open it) and so
// the same code path works under macOS + linux.
//
// When the probe fails for any reason other than a conflicting lock
// (ENOENT, permission denied, etc.) it returns nil — the real Open
// surfaces the actual error.
func CheckDBLockHolder(ctx context.Context, dbPath string) error {
	if dbPath == "" || dbPath == ":memory:" {
		return nil
	}
	if ctx != nil {
		if err := ctx.Err(); err != nil {
			return nil
		}
	}
	lockPath := SidecarLockPath(dbPath)
	f, err := os.OpenFile(lockPath, os.O_RDWR|os.O_CREATE, 0o644)
	if err != nil {
		return nil
	}
	defer func() { _ = f.Close() }()
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		if errors.Is(err, syscall.EWOULDBLOCK) || errors.Is(err, syscall.EAGAIN) {
			pid := readSidecarPID(SidecarPIDPath(dbPath))
			return &LockedError{Path: dbPath, PID: pid}
		}
		return nil
	}
	// Release immediately — this is just a probe.
	_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
	return nil
}

// AcquireDBLock takes the sidecar lock and writes the current PID to
// `<db>.apogee.pid`. Returns a release function that closes the lock
// descriptor and removes the pid file. Call at the top of Open.
//
// Callers must invoke the returned release function exactly once.
// When the probe finds the lock already held, AcquireDBLock returns
// a *LockedError wrapping ErrDBLocked.
func AcquireDBLock(dbPath string) (release func() error, err error) {
	if dbPath == "" || dbPath == ":memory:" {
		return func() error { return nil }, nil
	}
	lockPath := SidecarLockPath(dbPath)
	pidPath := SidecarPIDPath(dbPath)

	f, err := os.OpenFile(lockPath, os.O_RDWR|os.O_CREATE, 0o644)
	if err != nil {
		return nil, fmt.Errorf("duckdb lock: open sidecar: %w", err)
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = f.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) || errors.Is(err, syscall.EAGAIN) {
			pid := readSidecarPID(pidPath)
			return nil, &LockedError{Path: dbPath, PID: pid}
		}
		return nil, fmt.Errorf("duckdb lock: flock: %w", err)
	}
	if err := os.WriteFile(pidPath, []byte(strconv.Itoa(os.Getpid())), 0o644); err != nil {
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		_ = f.Close()
		return nil, fmt.Errorf("duckdb lock: write pid: %w", err)
	}

	released := false
	return func() error {
		if released {
			return nil
		}
		released = true
		_ = os.Remove(pidPath)
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		return f.Close()
	}, nil
}

// SidecarLockPath returns the path to the sidecar lock file for
// dbPath. Exposed so doctor / integration tests can stat it.
func SidecarLockPath(dbPath string) string {
	return dbPath + ".apogee.lock"
}

// SidecarPIDPath returns the path to the sidecar pid file for
// dbPath. Exposed for doctor / integration tests.
func SidecarPIDPath(dbPath string) string {
	return dbPath + ".apogee.pid"
}

// readSidecarPID parses the integer pid from a sidecar pid file.
// Returns 0 on any error.
func readSidecarPID(path string) int {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil || pid <= 0 {
		return 0
	}
	return pid
}

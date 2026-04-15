package duckdb

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestAcquireDBLockHappyPath(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.duckdb")

	release, err := AcquireDBLock(path)
	if err != nil {
		t.Fatalf("AcquireDBLock: %v", err)
	}
	defer func() { _ = release() }()

	if _, err := os.Stat(SidecarLockPath(path)); err != nil {
		t.Errorf("sidecar lock file missing: %v", err)
	}
	if _, err := os.Stat(SidecarPIDPath(path)); err != nil {
		t.Errorf("sidecar pid file missing: %v", err)
	}
}

func TestAcquireDBLockReentrantFromSameProcessFails(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.duckdb")

	release1, err := AcquireDBLock(path)
	if err != nil {
		t.Fatalf("first AcquireDBLock: %v", err)
	}
	defer func() { _ = release1() }()

	// A second call from the same process must surface ErrDBLocked.
	_, err = AcquireDBLock(path)
	if !errors.Is(err, ErrDBLocked) {
		t.Fatalf("expected ErrDBLocked, got %v", err)
	}
	var locked *LockedError
	if !errors.As(err, &locked) {
		t.Fatalf("expected *LockedError, got %T", err)
	}
	if locked.Path != path {
		t.Errorf("locked.Path = %q, want %q", locked.Path, path)
	}
	if locked.PID != os.Getpid() {
		t.Errorf("locked.PID = %d, want %d", locked.PID, os.Getpid())
	}
}

func TestCheckDBLockHolderNoConflict(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.duckdb")

	if err := CheckDBLockHolder(context.Background(), path); err != nil {
		t.Errorf("expected nil for unlocked path, got %v", err)
	}
}

func TestCheckDBLockHolderDetectsLocked(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.duckdb")

	release, err := AcquireDBLock(path)
	if err != nil {
		t.Fatalf("AcquireDBLock: %v", err)
	}
	defer func() { _ = release() }()

	err = CheckDBLockHolder(context.Background(), path)
	if !errors.Is(err, ErrDBLocked) {
		t.Fatalf("expected ErrDBLocked, got %v", err)
	}
	var locked *LockedError
	if !errors.As(err, &locked) {
		t.Fatalf("expected *LockedError, got %T", err)
	}
	if locked.PID != os.Getpid() {
		t.Errorf("locked.PID = %d, want %d", locked.PID, os.Getpid())
	}
}

func TestAcquireDBLockMemorySentinel(t *testing.T) {
	release, err := AcquireDBLock(":memory:")
	if err != nil {
		t.Fatalf("memory sentinel: %v", err)
	}
	if err := release(); err != nil {
		t.Errorf("release: %v", err)
	}
}

func TestReleaseRemovesSidecarFiles(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.duckdb")

	release, err := AcquireDBLock(path)
	if err != nil {
		t.Fatalf("AcquireDBLock: %v", err)
	}
	if err := release(); err != nil {
		t.Errorf("release: %v", err)
	}
	if _, err := os.Stat(SidecarPIDPath(path)); !os.IsNotExist(err) {
		t.Errorf("pid file should be removed after release, got %v", err)
	}
}

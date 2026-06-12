// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package blockdb

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

const lockFileName = "LOCK"

type heldLock struct {
	path string
	file *os.File
}

type dbLocks struct {
	locks []heldLock
}

// acquireDBLocks creates indexDir and dataDir if needed and acquires exclusive
// file locks in each. Returns errDatabaseInUse if another process holds a lock.
func acquireDBLocks(indexDir, dataDir string) (*dbLocks, error) {
	idx := filepath.Clean(indexDir)
	data := filepath.Clean(dataDir)

	paths := []string{idx}
	// Lock both directories when they differ; the index and data files must
	// be used together as one database.
	if idx != data {
		paths = append(paths, data)
	}

	l := &dbLocks{locks: make([]heldLock, 0, len(paths))}
	for _, dir := range paths {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			_ = l.Release()
			return nil, fmt.Errorf("failed to create directory %s: %w", dir, err)
		}
		held, err := tryLockDir(dir)
		if err != nil {
			_ = l.Release()
			return nil, err
		}
		l.locks = append(l.locks, held)
	}
	return l, nil
}

func tryLockDir(dir string) (heldLock, error) {
	path := filepath.Join(dir, lockFileName)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDONLY, defaultFilePermissions)
	if err != nil {
		return heldLock{}, fmt.Errorf("failed to open lock file %s: %w", path, err)
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = f.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return heldLock{}, fmt.Errorf("%w: %s", errDatabaseInUse, dir)
		}
		return heldLock{}, fmt.Errorf("failed to acquire lock on %s: %w", dir, err)
	}
	return heldLock{path: path, file: f}, nil
}

// Release unlocks every lock held by l.
//
// LOCK files are intentionally not removed. If removed, a process can
// acquire the lock between release and removal, and after removal another
// process can create a new LOCK file at the same path and lock it. Both
// processes would then hold the database lock.
func (l *dbLocks) Release() error {
	if l == nil {
		return nil
	}
	var errs []error
	for _, h := range l.locks {
		if err := syscall.Flock(int(h.file.Fd()), syscall.LOCK_UN); err != nil {
			errs = append(errs, fmt.Errorf("failed to release lock on %s: %w", h.path, err))
		}
		if err := h.file.Close(); err != nil {
			errs = append(errs, fmt.Errorf("failed to close lock file %s: %w", h.path, err))
		}
	}
	l.locks = nil
	return errors.Join(errs...)
}

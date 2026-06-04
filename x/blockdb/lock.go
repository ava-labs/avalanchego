// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package blockdb

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/gofrs/flock"
)

const lockFileName = "LOCK"

type dbLocks struct {
	locks []*flock.Flock
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

	l := &dbLocks{locks: make([]*flock.Flock, 0, len(paths))}
	for _, dir := range paths {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			_ = l.Release()
			return nil, fmt.Errorf("failed to create directory %s: %w", dir, err)
		}
		fl := flock.New(filepath.Join(dir, lockFileName))
		acquired, err := fl.TryLock()
		if err != nil {
			_ = l.Release()
			return nil, fmt.Errorf("failed to acquire lock on %s: %w", dir, err)
		}
		if !acquired {
			_ = l.Release()
			return nil, fmt.Errorf("%w: %s", errDatabaseInUse, dir)
		}
		l.locks = append(l.locks, fl)
	}
	return l, nil
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
	for _, fl := range l.locks {
		if err := fl.Unlock(); err != nil {
			errs = append(errs, fmt.Errorf("failed to release lock on %s: %w", fl.Path(), err))
		}
	}
	l.locks = nil
	return errors.Join(errs...)
}

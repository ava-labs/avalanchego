// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package core

import (
	"errors"
	"fmt"
)

var (
	errRepoNotFound      = errors.New("repository not found")
	errRepoAlreadyExists = errors.New("repository already exists")
)

// ErrRepoNotFound returns an error for a repository that was not found
func ErrRepoNotFound(name string) error {
	return fmt.Errorf("%w: %s", errRepoNotFound, name)
}

// ErrDirtyWorkingDir returns an error for a dirty working directory
func ErrDirtyWorkingDir(path string) error {
	return fmt.Errorf("working directory is dirty: %s", path)
}

// ErrRepoAlreadyExists returns an error for a repository that already exists
func ErrRepoAlreadyExists(name string, path string) error {
	return fmt.Errorf("%w: %s already exists at %s (use --force to update)", errRepoAlreadyExists, name, path)
}

// IsErrRepoAlreadyExists checks if an error is a repository already exists error
func IsErrRepoAlreadyExists(err error) bool {
	return errors.Is(err, errRepoAlreadyExists)
}

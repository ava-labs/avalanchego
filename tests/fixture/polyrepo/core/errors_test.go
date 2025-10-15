// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package core

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestErrRepoNotFound(t *testing.T) {
	err := ErrRepoNotFound("somerepo")
	if err == nil {
		require.Fail(t, "expected error")
	}

	expectedMsg := "repository not found: somerepo"
	require.Equal(t, expectedMsg, err.Error())
}

func TestErrDirtyWorkingDir(t *testing.T) {
	err := ErrDirtyWorkingDir("/path/to/repo")
	if err == nil {
		require.Fail(t, "expected error")
	}

	expectedMsg := "working directory is dirty: /path/to/repo"
	require.Equal(t, expectedMsg, err.Error())
}

func TestErrRepoAlreadyExists(t *testing.T) {
	err := ErrRepoAlreadyExists("avalanchego", "/some/path")
	if err == nil {
		require.Fail(t, "expected error")
	}

	expectedMsg := "repository already exists: avalanchego already exists at /some/path (use --force to update)"
	require.Equal(t, expectedMsg, err.Error())

	// Also test the IsErrRepoAlreadyExists function
	require.True(t, IsErrRepoAlreadyExists(err))
}

func TestIsErrRepoNotFound(t *testing.T) {
	err := ErrRepoNotFound("test")
	require.ErrorIs(t, err, errRepoNotFound)
}

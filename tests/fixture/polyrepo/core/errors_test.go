// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package core

import (
	"errors"
	"testing"
)

func TestErrRepoNotFound(t *testing.T) {
	err := ErrRepoNotFound("somerepo")
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	expectedMsg := "repository not found: somerepo"
	if err.Error() != expectedMsg {
		t.Errorf("expected message '%s', got '%s'", expectedMsg, err.Error())
	}
}

func TestErrDirtyWorkingDir(t *testing.T) {
	err := ErrDirtyWorkingDir("/path/to/repo")
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	expectedMsg := "working directory is dirty: /path/to/repo"
	if err.Error() != expectedMsg {
		t.Errorf("expected message '%s', got '%s'", expectedMsg, err.Error())
	}
}

func TestErrRepoAlreadyExists(t *testing.T) {
	err := ErrRepoAlreadyExists("avalanchego", "/some/path")
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	expectedMsg := "repository avalanchego already exists at /some/path (use --force to update)"
	if err.Error() != expectedMsg {
		t.Errorf("expected message '%s', got '%s'", expectedMsg, err.Error())
	}
}

func TestIsErrRepoNotFound(t *testing.T) {
	err := ErrRepoNotFound("test")
	if !errors.Is(err, errRepoNotFound) {
		t.Error("expected errors.Is to return true for ErrRepoNotFound")
	}
}

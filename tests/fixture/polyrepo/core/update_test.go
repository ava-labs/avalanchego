// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

//go:build integration

package core

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/utils/logging"
)

func TestUpdateAvalanchego_NoGoMod_Error(t *testing.T) {
	require := require.New(t)
	log := logging.NoLog{}

	// Create a temporary directory with no go.mod
	tmpDir := t.TempDir()

	// Call UpdateAvalanchego with baseDir that has no go.mod
	err := UpdateAvalanchego(log, tmpDir, "v1.11.0")

	// Should return error about missing go.mod
	require.Error(err)
	require.Contains(err.Error(), "go.mod not found in current directory")
}

func TestUpdateAvalanchego_ExplicitVersion(t *testing.T) {
	require := require.New(t)
	log := logging.NoLog{}

	// Create a temporary directory
	tmpDir := t.TempDir()

	// Create a minimal go.mod that depends on avalanchego
	goModContent := `module github.com/ava-labs/coreth

go 1.21

require (
	github.com/ava-labs/avalanchego v1.11.0
)
`
	goModPath := filepath.Join(tmpDir, "go.mod")
	require.NoError(os.WriteFile(goModPath, []byte(goModContent), 0644))

	// Call UpdateAvalanchego with explicit version
	// Note: This will fail until the function is fully implemented
	// For now, we're just testing the signature change and validation
	err := UpdateAvalanchego(log, tmpDir, "v1.11.5")

	// The function is not fully implemented yet, so we expect an error
	// But it should NOT be the "go.mod not found" error - it should pass validation
	require.Error(err) // Still fails because function not implemented
	require.NotContains(err.Error(), "go.mod not found")
	require.Contains(err.Error(), "not fully implemented")
}

func TestUpdateAvalanchego_EmptyVersion(t *testing.T) {
	require := require.New(t)
	log := logging.NoLog{}

	// Create a temporary directory
	tmpDir := t.TempDir()

	// Create a minimal go.mod that depends on avalanchego
	goModContent := `module github.com/ava-labs/coreth

go 1.21

require (
	github.com/ava-labs/avalanchego v1.11.0
)
`
	goModPath := filepath.Join(tmpDir, "go.mod")
	require.NoError(os.WriteFile(goModPath, []byte(goModContent), 0644))

	// Call UpdateAvalanchego with empty version (should use current version from go.mod)
	err := UpdateAvalanchego(log, tmpDir, "")

	// The function is not fully implemented yet, so we expect an error
	// But it should NOT be the "go.mod not found" error - it should pass validation
	require.Error(err) // Still fails because function not implemented
	require.NotContains(err.Error(), "go.mod not found")
	require.Contains(err.Error(), "not fully implemented")
}

func TestUpdateAvalanchego_FromAvalanchegoRepo_Error(t *testing.T) {
	require := require.New(t)
	log := logging.NoLog{}

	// Create a temporary directory
	tmpDir := t.TempDir()

	// Create a go.mod for avalanchego itself
	goModContent := `module github.com/ava-labs/avalanchego

go 1.21
`
	goModPath := filepath.Join(tmpDir, "go.mod")
	require.NoError(os.WriteFile(goModPath, []byte(goModContent), 0644))

	// Call UpdateAvalanchego from avalanchego repo (should fail)
	err := UpdateAvalanchego(log, tmpDir, "v1.11.0")

	// Should return error about running from avalanchego repository
	require.Error(err)
	require.Contains(err.Error(), "cannot be run from avalanchego repository")
}

func TestUpdateAvalanchego_GoModIsDirectory_Error(t *testing.T) {
	require := require.New(t)
	log := logging.NoLog{}

	// Create a temporary directory
	tmpDir := t.TempDir()

	// Create go.mod as a directory (not a file) - this is an edge case
	// The os.Stat will succeed, but reading the file will fail
	goModPath := filepath.Join(tmpDir, "go.mod")
	require.NoError(os.Mkdir(goModPath, 0755))

	// Call UpdateAvalanchego
	err := UpdateAvalanchego(log, tmpDir, "v1.11.0")

	// Should return error (the stat succeeds, but reading fails)
	require.Error(err)
	// Error will be from GetModulePath trying to read a directory
	require.Contains(err.Error(), "failed to get module path")
}

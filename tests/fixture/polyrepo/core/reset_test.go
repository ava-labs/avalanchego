// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package core

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/utils/logging"
)

// TestReset_NoGoMod_Error tests that Reset returns an error when go.mod doesn't exist
func TestReset_NoGoMod_Error(t *testing.T) {
	require := require.New(t)
	log := logging.NoLog{}

	// Create a temporary directory without go.mod
	tmpDir := t.TempDir()

	// Call Reset - should fail because go.mod doesn't exist
	err := Reset(log, tmpDir, []string{})
	require.ErrorIs(err, ErrGoModNotFound)
}

// TestReset_AllRepos tests that Reset removes all polyrepo replace directives when no repos specified
func TestReset_AllRepos(t *testing.T) {
	require := require.New(t)
	log := logging.NoLog{}

	// Create a temporary directory with go.mod
	tmpDir := t.TempDir()
	goModPath := filepath.Join(tmpDir, "go.mod")

	// Create a go.mod with replace directives
	goModContent := `module github.com/ava-labs/avalanchego

go 1.24

require (
	github.com/ava-labs/coreth v0.13.8
	github.com/ava-labs/firewood/ffi v0.2.0
)

replace (
	github.com/ava-labs/coreth => ./coreth
	github.com/ava-labs/firewood/ffi => ./firewood/ffi/result/ffi
)
`
	require.NoError(os.WriteFile(goModPath, []byte(goModContent), 0o600))

	// Call Reset with no repos (should remove all)
	require.NoError(Reset(log, tmpDir, []string{}))

	// Verify replace directives were removed
	content, err := os.ReadFile(goModPath)
	require.NoError(err)

	// Should not contain replace directives for coreth or firewood
	require.NotContains(string(content), "github.com/ava-labs/coreth =>")
	require.NotContains(string(content), "github.com/ava-labs/firewood/ffi =>")
}

// TestReset_SpecificRepos tests that Reset removes only specified repos' replace directives
func TestReset_SpecificRepos(t *testing.T) {
	require := require.New(t)
	log := logging.NoLog{}

	// Create a temporary directory with go.mod
	tmpDir := t.TempDir()
	goModPath := filepath.Join(tmpDir, "go.mod")

	// Create a go.mod with replace directives for both coreth and firewood
	goModContent := `module github.com/ava-labs/avalanchego

go 1.24

require (
	github.com/ava-labs/coreth v0.13.8
	github.com/ava-labs/firewood/ffi v0.2.0
)

replace (
	github.com/ava-labs/coreth => ./coreth
	github.com/ava-labs/firewood/ffi => ./firewood/ffi/result/ffi
)
`
	require.NoError(os.WriteFile(goModPath, []byte(goModContent), 0o600))

	// Call Reset with only coreth
	require.NoError(Reset(log, tmpDir, []string{"coreth"}))

	// Verify only coreth replace directive was removed
	content, err := os.ReadFile(goModPath)
	require.NoError(err)

	// Should not contain coreth replace
	require.NotContains(string(content), "github.com/ava-labs/coreth => ./coreth")
	// Should still contain firewood replace
	require.Contains(string(content), "github.com/ava-labs/firewood/ffi => ./firewood")
}

// TestReset_EmptyRepoList tests that Reset handles empty repo list correctly by resetting all
func TestReset_EmptyRepoList(t *testing.T) {
	require := require.New(t)
	log := logging.NoLog{}

	// Create a temporary directory with go.mod
	tmpDir := t.TempDir()
	goModPath := filepath.Join(tmpDir, "go.mod")

	// Create a go.mod with replace directive for avalanchego (to ensure all three repos are tested)
	goModContent := `module github.com/ava-labs/coreth

go 1.24

require (
	github.com/ava-labs/avalanchego v1.11.0
)

replace (
	github.com/ava-labs/avalanchego => ../avalanchego
)
`
	require.NoError(os.WriteFile(goModPath, []byte(goModContent), 0o600))

	// Call Reset with empty list - should remove all polyrepo replace directives
	require.NoError(Reset(log, tmpDir, []string{}))

	// Verify replace directive was removed
	content, err := os.ReadFile(goModPath)
	require.NoError(err)
	require.NotContains(string(content), "github.com/ava-labs/avalanchego =>")
}

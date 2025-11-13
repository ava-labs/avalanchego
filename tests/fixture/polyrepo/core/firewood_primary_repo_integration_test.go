// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

//go:build integration
// +build integration

package core

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestSync_FromFirewood_CLI_PrimaryRepoMode tests that polyrepo sync works correctly
// when executed from a cloned firewood repository (with go.mod at ffi/go.mod).
//
// This test validates the fix for the bug where firewood wasn't detected as the primary
// repo because the tool only looked for go.mod at the root, not at ffi/go.mod.
//
// Test scenario:
// 1. Clone firewood to a temp directory
// 2. cd into firewood directory
// 3. Execute `polyrepo sync avalanchego` via CLI
// 4. Verify avalanchego is cloned
// 5. Verify avalanchego/go.mod has replace directive pointing to ../ffi
func TestSync_FromFirewood_CLI_PrimaryRepoMode(t *testing.T) {
	// Create temp directory for test workspace
	tmpDir := t.TempDir()

	// Clone firewood
	firewoodConfig, err := GetRepoConfig("firewood")
	require.NoError(t, err, "failed to get firewood config")

	// Use exec.Command to clone firewood (can't use CloneRepo because it's not available in this test context)
	cloneCmd := exec.Command("git", "clone", "--depth", "1", "--branch", firewoodConfig.DefaultBranch, firewoodConfig.GitRepo, "firewood")
	cloneCmd.Dir = tmpDir
	var cloneStderr strings.Builder
	cloneCmd.Stderr = &cloneStderr
	err = cloneCmd.Run()
	require.NoError(t, err, "failed to clone firewood: %s", cloneStderr.String())

	firewoodPath := filepath.Join(tmpDir, "firewood")

	// Verify firewood was cloned and has ffi/go.mod
	ffiGoModPath := filepath.Join(firewoodPath, "ffi", "go.mod")
	_, err = os.Stat(ffiGoModPath)
	require.NoError(t, err, "expected ffi/go.mod to exist after cloning firewood")

	// Build polyrepo binary
	polyrepoBinDir := t.TempDir()
	polyrepoBin := filepath.Join(polyrepoBinDir, "polyrepo")

	// Get the polyrepo source directory (one level up from current package)
	cwd, err := os.Getwd()
	require.NoError(t, err, "failed to get current working directory")
	polyrepoDir := filepath.Join(cwd, "..")

	buildCmd := exec.Command("go", "build", "-o", polyrepoBin, ".")
	buildCmd.Dir = polyrepoDir
	var buildStderr strings.Builder
	buildCmd.Stderr = &buildStderr
	err = buildCmd.Run()
	require.NoError(t, err, "failed to build polyrepo: %s", buildStderr.String())

	// Execute: polyrepo sync avalanchego from within firewood directory
	// This is the critical test - it should detect firewood as primary repo
	syncCmd := exec.Command(polyrepoBin, "sync", "avalanchego", "--depth", "1")
	syncCmd.Dir = firewoodPath
	var syncStdout, syncStderr strings.Builder
	syncCmd.Stdout = &syncStdout
	syncCmd.Stderr = &syncStderr
	err = syncCmd.Run()

	// Log output for debugging
	t.Logf("Sync stdout:\n%s", syncStdout.String())
	t.Logf("Sync stderr:\n%s", syncStderr.String())

	require.NoError(t, err, "polyrepo sync avalanchego failed from firewood directory: %s", syncStderr.String())

	// Validate: Check that avalanchego was cloned
	avalanchegoPath := filepath.Join(firewoodPath, "avalanchego")
	_, err = os.Stat(avalanchegoPath)
	require.NoError(t, err, "expected avalanchego to be cloned in firewood directory")

	// Validate: Check that avalanchego/go.mod has replace directive pointing to ../ffi
	avalanchegoGoModPath := filepath.Join(avalanchegoPath, "go.mod")
	goModContent, err := os.ReadFile(avalanchegoGoModPath)
	require.NoError(t, err, "failed to read avalanchego/go.mod")

	goModStr := string(goModContent)

	// Check for replace directive
	// The firewood module is "github.com/ava-labs/firewood-go-ethhash/ffi"
	// and should be replaced with "../ffi" (relative path from avalanchego to firewood/ffi)
	require.Contains(t, goModStr, "replace github.com/ava-labs/firewood-go-ethhash/ffi",
		"expected replace directive for firewood in avalanchego/go.mod")

	// The replace should point to ../ffi (since avalanchego is synced into firewood/avalanchego)
	// OR ../ffi/result/ffi if nix build was run
	require.True(t,
		strings.Contains(goModStr, "=> ../ffi\n") ||
			strings.Contains(goModStr, "=> ../ffi/result/ffi\n") ||
			strings.Contains(goModStr, "=> ../ffi\r\n") ||
			strings.Contains(goModStr, "=> ../ffi/result/ffi\r\n"),
		"expected replace directive to point to ../ffi or ../ffi/result/ffi, got:\n%s", goModStr)

	t.Logf("Test passed: firewood was correctly detected as primary repo, avalanchego was synced with correct replace directive")
}

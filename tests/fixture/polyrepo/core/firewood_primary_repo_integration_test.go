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

	// Clone firewood at a specific commit that's compatible with available Rust version
	// Using an older commit that works with Rust 1.80 (avoiding main which requires 1.91)
	cloneCmd := exec.Command("git", "clone", firewoodConfig.GitRepo, "firewood")
	cloneCmd.Dir = tmpDir
	var cloneStderr strings.Builder
	cloneCmd.Stderr = &cloneStderr
	err = cloneCmd.Run()
	require.NoError(t, err, "failed to clone firewood: %s", cloneStderr.String())

	// Checkout a known-good commit (has ffi/flake.nix, compatible with Rust 1.80)
	// This commit is from before the Rust 1.91 requirement
	checkoutCmd := exec.Command("git", "checkout", "bdf58f91b8efda6b6778a1da548d589f152f677d")
	checkoutCmd.Dir = filepath.Join(tmpDir, "firewood")
	err = checkoutCmd.Run()
	require.NoError(t, err, "failed to checkout firewood commit")

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

	// Check if ffi/flake.nix exists (determines if nix build should run)
	flakeNixPath := filepath.Join(firewoodPath, "ffi", "flake.nix")
	_, flakeNixErr := os.Stat(flakeNixPath)
	hasFlakeNix := flakeNixErr == nil

	if hasFlakeNix {
		// If flake.nix exists, nix build should have run
		// Verify that ffi/result/ffi/ directory exists (nix build output)
		nixOutputPath := filepath.Join(firewoodPath, "ffi", "result", "ffi")
		_, err = os.Stat(nixOutputPath)
		require.NoError(t, err, "expected ffi/result/ffi/ to exist after nix build (flake.nix present)")

		// Verify replace directive uses the nix output path
		require.True(t,
			strings.Contains(goModStr, "=> ../ffi/result/ffi\n") ||
				strings.Contains(goModStr, "=> ../ffi/result/ffi\r\n"),
			"expected replace directive to point to ../ffi/result/ffi (nix build output), got:\n%s", goModStr)

		t.Logf("Test passed: firewood was correctly detected as primary repo, nix build ran, avalanchego uses correct nix output path")
	} else {
		// If no flake.nix, cargo build fallback should be used
		// The replace should point to ../ffi (cargo build path)
		require.True(t,
			strings.Contains(goModStr, "=> ../ffi\n") ||
				strings.Contains(goModStr, "=> ../ffi\r\n"),
			"expected replace directive to point to ../ffi (cargo build path), got:\n%s", goModStr)

		t.Logf("Test passed: firewood was correctly detected as primary repo, cargo build used (no flake.nix), avalanchego uses correct cargo path")
	}
}

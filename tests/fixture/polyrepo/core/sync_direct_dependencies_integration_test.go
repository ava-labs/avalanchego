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

// TestSync_FromFirewood_NoArgs_OnlyAvalanchego tests that when running
// `polyrepo sync` with no arguments from a firewood primary repo, only
// avalanchego (the direct dependency) is synced, not coreth (transitive).
func TestSync_FromFirewood_NoArgs_OnlyAvalanchego(t *testing.T) {
	// Create temp directory for test workspace
	tmpDir := t.TempDir()

	// Clone firewood
	firewoodConfig, err := GetRepoConfig("firewood")
	require.NoError(t, err, "failed to get firewood config")

	cloneCmd := exec.Command("git", "clone", "--depth", "1", "--branch", firewoodConfig.DefaultBranch, firewoodConfig.GitRepo, "firewood")
	cloneCmd.Dir = tmpDir
	var cloneStderr strings.Builder
	cloneCmd.Stderr = &cloneStderr
	err = cloneCmd.Run()
	require.NoError(t, err, "failed to clone firewood: %s", cloneStderr.String())

	firewoodPath := filepath.Join(tmpDir, "firewood")

	// Build polyrepo binary
	polyrepoBinDir := t.TempDir()
	polyrepoBin := filepath.Join(polyrepoBinDir, "polyrepo")

	cwd, err := os.Getwd()
	require.NoError(t, err, "failed to get current working directory")
	polyrepoDir := filepath.Join(cwd, "..")

	buildCmd := exec.Command("go", "build", "-o", polyrepoBin, ".")
	buildCmd.Dir = polyrepoDir
	var buildStderr strings.Builder
	buildCmd.Stderr = &buildStderr
	err = buildCmd.Run()
	require.NoError(t, err, "failed to build polyrepo: %s", buildStderr.String())

	// Execute: polyrepo sync (no args) from within firewood directory
	syncCmd := exec.Command(polyrepoBin, "sync", "--depth", "1")
	syncCmd.Dir = firewoodPath
	var syncStdout, syncStderr strings.Builder
	syncCmd.Stdout = &syncStdout
	syncCmd.Stderr = &syncStderr
	err = syncCmd.Run()

	// Log output for debugging
	t.Logf("Sync stdout:\n%s", syncStdout.String())
	t.Logf("Sync stderr:\n%s", syncStderr.String())

	require.NoError(t, err, "polyrepo sync failed from firewood directory: %s", syncStderr.String())

	// Validate: avalanchego should be synced (always, unconditionally)
	avalanchegoPath := filepath.Join(firewoodPath, "avalanchego")
	_, err = os.Stat(avalanchegoPath)
	require.NoError(t, err, "expected avalanchego to be synced (always synced from firewood)")

	// Validate: coreth should NOT be cloned (only avalanchego is synced)
	corethPath := filepath.Join(firewoodPath, "coreth")
	_, err = os.Stat(corethPath)
	require.True(t, os.IsNotExist(err), "coreth should NOT be cloned (only avalanchego is synced)")

	t.Logf("Test passed: only avalanchego was synced from firewood, not coreth")
}

// TestSync_FromCoreth_NoArgs_OnlyAvalanchego tests that when running
// `polyrepo sync` with no arguments from a coreth primary repo, only
// avalanchego (the direct dependency) is synced, not firewood (transitive).
func TestSync_FromCoreth_NoArgs_OnlyAvalanchego(t *testing.T) {
	// Create temp directory for test workspace
	tmpDir := t.TempDir()

	// Clone coreth
	corethConfig, err := GetRepoConfig("coreth")
	require.NoError(t, err, "failed to get coreth config")

	cloneCmd := exec.Command("git", "clone", "--depth", "1", "--branch", corethConfig.DefaultBranch, corethConfig.GitRepo, "coreth")
	cloneCmd.Dir = tmpDir
	var cloneStderr strings.Builder
	cloneCmd.Stderr = &cloneStderr
	err = cloneCmd.Run()
	require.NoError(t, err, "failed to clone coreth: %s", cloneStderr.String())

	corethPath := filepath.Join(tmpDir, "coreth")

	// Build polyrepo binary
	polyrepoBinDir := t.TempDir()
	polyrepoBin := filepath.Join(polyrepoBinDir, "polyrepo")

	cwd, err := os.Getwd()
	require.NoError(t, err, "failed to get current working directory")
	polyrepoDir := filepath.Join(cwd, "..")

	buildCmd := exec.Command("go", "build", "-o", polyrepoBin, ".")
	buildCmd.Dir = polyrepoDir
	var buildStderr strings.Builder
	buildCmd.Stderr = &buildStderr
	err = buildCmd.Run()
	require.NoError(t, err, "failed to build polyrepo: %s", buildStderr.String())

	// Execute: polyrepo sync (no args) from within coreth directory
	syncCmd := exec.Command(polyrepoBin, "sync", "--depth", "1")
	syncCmd.Dir = corethPath
	var syncStdout, syncStderr strings.Builder
	syncCmd.Stdout = &syncStdout
	syncCmd.Stderr = &syncStderr
	err = syncCmd.Run()

	// Log output for debugging
	t.Logf("Sync stdout:\n%s", syncStdout.String())
	t.Logf("Sync stderr:\n%s", syncStderr.String())

	require.NoError(t, err, "polyrepo sync failed from coreth directory: %s", syncStderr.String())

	// Validate: avalanchego should be cloned (direct dependency)
	avalanchegoPath := filepath.Join(corethPath, "avalanchego")
	_, err = os.Stat(avalanchegoPath)
	require.NoError(t, err, "expected avalanchego to be cloned (direct dependency)")

	// Validate: firewood should NOT be cloned (we only sync direct dependencies)
	// Note: coreth has firewood as a direct dependency too, so this test validates
	// that we're ONLY syncing avalanchego, not ALL direct dependencies.
	// Actually, on second thought, coreth DOES have firewood as a direct dependency,
	// so firewood SHOULD be synced. Let me reconsider this test...

	// Actually, reviewing the requirements again:
	// - Firewood primary repo: sync only avalanchego (correct)
	// - Coreth primary repo: sync only avalanchego (correct - even though firewood is also a direct dep)
	// This means coreth should ONLY sync avalanchego, not firewood

	firewoodPath := filepath.Join(corethPath, "firewood")
	_, err = os.Stat(firewoodPath)
	require.True(t, os.IsNotExist(err), "firewood should NOT be cloned (per requirements, only avalanchego should be synced from coreth)")

	t.Logf("Test passed: only avalanchego was synced from coreth, not firewood")
}

// TestSync_FromAvalanchego_NoArgs_ReturnsError tests that when running
// `polyrepo sync` with no arguments from an avalanchego primary repo, an error
// is returned since avalanchego is a root repo and requires explicit arguments.
func TestSync_FromAvalanchego_NoArgs_ReturnsError(t *testing.T) {
	// Create temp directory for test workspace
	tmpDir := t.TempDir()

	// Clone avalanchego
	avalanchegoConfig, err := GetRepoConfig("avalanchego")
	require.NoError(t, err, "failed to get avalanchego config")

	cloneCmd := exec.Command("git", "clone", "--depth", "1", "--branch", avalanchegoConfig.DefaultBranch, avalanchegoConfig.GitRepo, "avalanchego")
	cloneCmd.Dir = tmpDir
	var cloneStderr strings.Builder
	cloneCmd.Stderr = &cloneStderr
	err = cloneCmd.Run()
	require.NoError(t, err, "failed to clone avalanchego: %s", cloneStderr.String())

	avalanchegoPath := filepath.Join(tmpDir, "avalanchego")

	// Build polyrepo binary
	polyrepoBinDir := t.TempDir()
	polyrepoBin := filepath.Join(polyrepoBinDir, "polyrepo")

	cwd, err := os.Getwd()
	require.NoError(t, err, "failed to get current working directory")
	polyrepoDir := filepath.Join(cwd, "..")

	buildCmd := exec.Command("go", "build", "-o", polyrepoBin, ".")
	buildCmd.Dir = polyrepoDir
	var buildStderr strings.Builder
	buildCmd.Stderr = &buildStderr
	err = buildCmd.Run()
	require.NoError(t, err, "failed to build polyrepo: %s", buildStderr.String())

	// Execute: polyrepo sync (no args) from within avalanchego directory
	// This should FAIL with an error
	syncCmd := exec.Command(polyrepoBin, "sync")
	syncCmd.Dir = avalanchegoPath
	var syncStdout, syncStderr strings.Builder
	syncCmd.Stdout = &syncStdout
	syncCmd.Stderr = &syncStderr
	err = syncCmd.Run()

	// Log output for debugging
	t.Logf("Sync stdout:\n%s", syncStdout.String())
	t.Logf("Sync stderr:\n%s", syncStderr.String())

	// Validate: command should have failed
	require.Error(t, err, "expected polyrepo sync to fail from avalanchego without explicit args")

	// Validate: error message should mention that avalanchego requires explicit args
	combinedOutput := syncStdout.String() + syncStderr.String()
	require.Contains(t, combinedOutput, "requires explicit",
		"expected error message to mention that explicit arguments are required")

	t.Logf("Test passed: avalanchego correctly returned error when sync run without args")
}

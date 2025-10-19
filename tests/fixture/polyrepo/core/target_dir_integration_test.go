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
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

var (
	// polyrepoSourceDir is the absolute path to the polyrepo source directory
	polyrepoSourceDir     string
	polyrepoSourceDirOnce sync.Once
)

// getPolyrepoSourceDir returns the absolute path to the polyrepo source directory
// It's calculated once and cached for all tests in the package
func getPolyrepoSourceDir(t *testing.T) string {
	t.Helper()

	polyrepoSourceDirOnce.Do(func() {
		// Get current working directory (should be tests/fixture/polyrepo/core/)
		cwd, err := os.Getwd()
		require.NoError(t, err, "failed to get current working directory")

		// Go up one directory to tests/fixture/polyrepo/
		polyrepoSourceDir = filepath.Join(cwd, "..")
	})

	return polyrepoSourceDir
}

// runPolyrepo runs the polyrepo command with the given arguments
// Returns stdout, stderr, and error
func runPolyrepo(t *testing.T, args ...string) (string, string, error) {
	t.Helper()

	// Create a single temp directory for the binary
	tmpDir := t.TempDir()
	polyrepoBin := filepath.Join(tmpDir, "polyrepo")

	// Get the polyrepo source directory (cached, calculated once)
	polyrepoDir := getPolyrepoSourceDir(t)

	// Build the polyrepo binary
	cmd := exec.Command("go", "build", "-o", polyrepoBin, ".")
	cmd.Dir = polyrepoDir
	var buildStderr strings.Builder
	cmd.Stderr = &buildStderr
	err := cmd.Run()
	require.NoError(t, err, "failed to build polyrepo: %s", buildStderr.String())

	// Run the polyrepo command
	runCmd := exec.Command(polyrepoBin, args...)

	var stdout, stderr strings.Builder
	runCmd.Stdout = &stdout
	runCmd.Stderr = &stderr

	err = runCmd.Run()
	return stdout.String(), stderr.String(), err
}

// TestTargetDir_AbsolutePath tests --target-dir with an absolute path
func TestTargetDir_AbsolutePath(t *testing.T) {
	tmpDir := t.TempDir()

	// Create avalanchego go.mod in target directory
	goModPath := filepath.Join(tmpDir, "go.mod")
	goModContent := `module github.com/ava-labs/avalanchego

go 1.24

require (
	github.com/ava-labs/coreth v0.15.4
	github.com/ava-labs/firewood-go-ethhash v0.0.12
)
`
	err := os.WriteFile(goModPath, []byte(goModContent), 0o600)
	require.NoError(t, err, "failed to write go.mod")

	// Run status command with absolute target-dir
	stdout, stderr, err := runPolyrepo(t, "--target-dir", tmpDir, "status")
	require.NoError(t, err, "polyrepo status failed: stderr=%s", stderr)
	require.Contains(t, stdout, "Primary Repository: avalanchego", "expected avalanchego to be detected as primary")
}

// TestTargetDir_RelativePath tests --target-dir with a relative path
func TestTargetDir_RelativePath(t *testing.T) {
	tmpDir := t.TempDir()

	// Create a subdirectory
	targetDir := filepath.Join(tmpDir, "myproject")
	err := os.MkdirAll(targetDir, 0o755)
	require.NoError(t, err, "failed to create target directory")

	// Create avalanchego go.mod in target directory
	goModPath := filepath.Join(targetDir, "go.mod")
	goModContent := `module github.com/ava-labs/avalanchego

go 1.24
`
	err = os.WriteFile(goModPath, []byte(goModContent), 0o600)
	require.NoError(t, err, "failed to write go.mod")

	// Change to parent directory
	originalWd, err := os.Getwd()
	require.NoError(t, err, "failed to get working directory")
	defer func() {
		err := os.Chdir(originalWd)
		require.NoError(t, err, "failed to restore working directory")
	}()

	err = os.Chdir(tmpDir)
	require.NoError(t, err, "failed to change to temp directory")

	// Run status command with relative target-dir
	stdout, stderr, err := runPolyrepo(t, "--target-dir", "myproject", "status")
	require.NoError(t, err, "polyrepo status failed: stderr=%s", stderr)
	require.Contains(t, stdout, "Primary Repository: avalanchego", "expected avalanchego to be detected as primary")
}

// TestTargetDir_NonExistentDir tests --target-dir with a non-existent directory
func TestTargetDir_NonExistentDir(t *testing.T) {
	tmpDir := t.TempDir()
	nonExistentDir := filepath.Join(tmpDir, "does-not-exist")

	// Run status command with non-existent target-dir
	_, stderr, err := runPolyrepo(t, "--target-dir", nonExistentDir, "status")
	require.Error(t, err, "expected error for non-existent directory")
	require.Contains(t, stderr, "failed to access target directory", "expected error message about accessing directory")
}

// TestTargetDir_FileNotDir tests --target-dir with a file instead of directory
func TestTargetDir_FileNotDir(t *testing.T) {
	tmpDir := t.TempDir()

	// Create a file
	filePath := filepath.Join(tmpDir, "notadir")
	err := os.WriteFile(filePath, []byte("test"), 0o600)
	require.NoError(t, err, "failed to create file")

	// Run status command with file as target-dir
	_, stderr, err := runPolyrepo(t, "--target-dir", filePath, "status")
	require.Error(t, err, "expected error for file instead of directory")
	require.Contains(t, stderr, "is not a directory", "expected error message about not being a directory")
}

// TestTargetDir_DefaultBehavior tests that omitting --target-dir uses current directory
func TestTargetDir_DefaultBehavior(t *testing.T) {
	tmpDir := t.TempDir()

	// Create avalanchego go.mod
	goModPath := filepath.Join(tmpDir, "go.mod")
	goModContent := `module github.com/ava-labs/avalanchego

go 1.24
`
	err := os.WriteFile(goModPath, []byte(goModContent), 0o600)
	require.NoError(t, err, "failed to write go.mod")

	// Change to temp directory
	originalWd, err := os.Getwd()
	require.NoError(t, err, "failed to get working directory")
	defer func() {
		err := os.Chdir(originalWd)
		require.NoError(t, err, "failed to restore working directory")
	}()

	err = os.Chdir(tmpDir)
	require.NoError(t, err, "failed to change to temp directory")

	// Run status command without --target-dir (should use current directory)
	stdout, stderr, err := runPolyrepo(t, "status")
	require.NoError(t, err, "polyrepo status failed: stderr=%s", stderr)
	require.Contains(t, stdout, "Primary Repository: avalanchego", "expected avalanchego to be detected as primary")
}

// TestTargetDir_WithSyncCommand tests --target-dir with the sync command
func TestTargetDir_WithSyncCommand(t *testing.T) {
	t.Skip("Skipping sync command test - requires network access and is slow")

	tmpDir := t.TempDir()

	// Create avalanchego go.mod in target directory
	goModPath := filepath.Join(tmpDir, "go.mod")
	goModContent := `module github.com/ava-labs/avalanchego

go 1.24

require (
	github.com/ava-labs/firewood-go-ethhash v0.0.12
)
`
	err := os.WriteFile(goModPath, []byte(goModContent), 0o600)
	require.NoError(t, err, "failed to write go.mod")

	// Run sync command with target-dir
	stdout, stderr, err := runPolyrepo(t, "--target-dir", tmpDir, "sync", "firewood@main", "--depth", "1")
	require.NoError(t, err, "polyrepo sync failed: stderr=%s", stderr)

	// Verify firewood was cloned in target directory
	firewoodPath := filepath.Join(tmpDir, "firewood")
	_, err = os.Stat(firewoodPath)
	require.NoError(t, err, "expected firewood to be cloned in target directory")

	// Verify status shows firewood as cloned
	stdout, stderr, err = runPolyrepo(t, "--target-dir", tmpDir, "status")
	require.NoError(t, err, "polyrepo status failed: stderr=%s", stderr)
	require.Contains(t, stdout, "firewood:", "expected firewood in status output")
}

// TestTargetDir_WithResetCommand tests --target-dir with the reset command
func TestTargetDir_WithResetCommand(t *testing.T) {
	tmpDir := t.TempDir()

	// Create avalanchego go.mod with replace directive
	goModPath := filepath.Join(tmpDir, "go.mod")
	goModContent := `module github.com/ava-labs/avalanchego

go 1.24

require (
	github.com/ava-labs/coreth v0.15.4
)

replace github.com/ava-labs/coreth => ./coreth
`
	err := os.WriteFile(goModPath, []byte(goModContent), 0o600)
	require.NoError(t, err, "failed to write go.mod")

	// Run reset command with target-dir
	_, stderr, err := runPolyrepo(t, "--target-dir", tmpDir, "reset")
	require.NoError(t, err, "polyrepo reset failed: stderr=%s", stderr)

	// Verify replace directive was removed
	content, err := os.ReadFile(goModPath)
	require.NoError(t, err, "failed to read go.mod")
	require.NotContains(t, string(content), "replace github.com/ava-labs/coreth", "expected replace directive to be removed")
}

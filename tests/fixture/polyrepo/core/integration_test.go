// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

//go:build integration
// +build integration

package core

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/stretchr/testify/require"
)

// TestCloneRepo_ActualClone tests cloning a real repository
func TestCloneRepo_ActualClone(t *testing.T) {
	log := logging.NoLog{}
	tmpDir := t.TempDir()
	clonePath := filepath.Join(tmpDir, "firewood")

	// Get firewood config
	config, err := GetRepoConfig("firewood")
	require.NoError(t, err, "failed to get config")

	// Clone with default branch and shallow depth
	err = CloneRepo(log, config.GitRepo, clonePath, config.DefaultBranch, 1)
	require.NoError(t, err, "failed to clone")

	// Verify the repo was cloned
	_, err = os.Stat(filepath.Join(clonePath, ".git"))
	require.False(t, os.IsNotExist(err), "expected .git directory to exist")

	// Verify we can get the current ref
	currentRef, err := GetCurrentRef(log, clonePath)
	require.NoError(t, err, "failed to get current ref")
	require.Equal(t, config.DefaultBranch, currentRef)
}

// TestCloneOrUpdateRepo_WithSHA tests cloning with a specific commit SHA
func TestCloneOrUpdateRepo_WithSHA(t *testing.T) {
	log := logging.NoLog{}
	tmpDir := t.TempDir()
	clonePath := filepath.Join(tmpDir, "firewood")

	// Get firewood config
	config, err := GetRepoConfig("firewood")
	require.NoError(t, err, "failed to get config")

	// Known good commit SHA from firewood repo
	commitSHA := "57a74c3a7fd7dcdda24f49a237bfa9fa69f26a85"

	// Clone with SHA and shallow depth (should fallback to full clone)
	err = CloneOrUpdateRepo(log, config.GitRepo, clonePath, commitSHA, 1, false)
	require.NoError(t, err, "failed to clone with SHA")

	// Verify the repo was cloned
	_, err = os.Stat(filepath.Join(clonePath, ".git"))
	require.False(t, os.IsNotExist(err), "expected .git directory to exist")

	// Verify we're at the correct commit
	currentRef, err := GetCurrentRef(log, clonePath)
	require.NoError(t, err, "failed to get current ref")
	require.Equal(t, commitSHA, currentRef)
}

// TestIsRepoDirty_Clean tests dirty detection on a clean repo
func TestIsRepoDirty_Clean(t *testing.T) {
	log := logging.NoLog{}
	tmpDir := t.TempDir()
	clonePath := filepath.Join(tmpDir, "firewood")

	config, err := GetRepoConfig("firewood")
	require.NoError(t, err, "failed to get config")

	// Clone repo
	err = CloneRepo(log, config.GitRepo, clonePath, config.DefaultBranch, 1)
	require.NoError(t, err, "failed to clone")

	// Should not be dirty
	isDirty, err := IsRepoDirty(log, clonePath)
	require.NoError(t, err, "failed to check dirty status")
	require.False(t, isDirty, "expected clean repo, got dirty")
}

// TestIsRepoDirty_WithChanges tests dirty detection with uncommitted changes
func TestIsRepoDirty_WithChanges(t *testing.T) {
	log := logging.NoLog{}
	tmpDir := t.TempDir()
	clonePath := filepath.Join(tmpDir, "firewood")

	config, err := GetRepoConfig("firewood")
	require.NoError(t, err, "failed to get config")

	// Clone repo
	err = CloneRepo(log, config.GitRepo, clonePath, config.DefaultBranch, 1)
	require.NoError(t, err, "failed to clone")

	// Make a change to the repo
	testFile := filepath.Join(clonePath, "test_file.txt")
	err = os.WriteFile(testFile, []byte("test content"), 0o600)
	require.NoError(t, err, "failed to write test file")

	// Should be dirty now
	isDirty, err := IsRepoDirty(log, clonePath)
	require.NoError(t, err, "failed to check dirty status")
	require.True(t, isDirty, "expected dirty repo, got clean")
}

// TestCloneOrUpdateRepo_ExistingRepoWithoutForce tests that cloning fails when repo exists without force
func TestCloneOrUpdateRepo_ExistingRepoWithoutForce(t *testing.T) {
	log := logging.NoLog{}
	tmpDir := t.TempDir()
	clonePath := filepath.Join(tmpDir, "firewood")

	config, err := GetRepoConfig("firewood")
	require.NoError(t, err, "failed to get config")

	// Clone repo first time
	err = CloneOrUpdateRepo(log, config.GitRepo, clonePath, config.DefaultBranch, 1, false)
	require.NoError(t, err, "failed to clone")

	// Try to clone again without force - should fail
	err = CloneOrUpdateRepo(log, config.GitRepo, clonePath, config.DefaultBranch, 1, false)
	require.Error(t, err, "expected error when cloning to existing path without force")
	require.True(t, IsErrRepoAlreadyExists(err), "expected ErrRepoAlreadyExists")
}

// TestCloneOrUpdateRepo_ExistingRepoWithForce tests that cloning succeeds when repo exists with force
func TestCloneOrUpdateRepo_ExistingRepoWithForce(t *testing.T) {
	log := logging.NoLog{}
	tmpDir := t.TempDir()
	clonePath := filepath.Join(tmpDir, "firewood")

	config, err := GetRepoConfig("firewood")
	require.NoError(t, err, "failed to get config")

	// Clone repo first time
	err = CloneOrUpdateRepo(log, config.GitRepo, clonePath, config.DefaultBranch, 1, false)
	require.NoError(t, err, "failed to clone")

	// Try to clone again with force - should succeed
	err = CloneOrUpdateRepo(log, config.GitRepo, clonePath, config.DefaultBranch, 1, true)
	require.NoError(t, err, "expected success when cloning to existing path with force")
}

// TestGetRepoStatus_NotCloned tests status for a repo that hasn't been cloned
func TestGetRepoStatus_NotCloned(t *testing.T) {
	log := logging.NoLog{}
	tmpDir := t.TempDir()

	status, err := GetRepoStatus(log, "firewood", tmpDir, "")
	require.NoError(t, err, "failed to get status")
	require.False(t, status.Exists, "expected repo to not exist")
	require.Equal(t, "firewood", status.Name)
}

// TestGetRepoStatus_Cloned tests status for a cloned repo
func TestGetRepoStatus_Cloned(t *testing.T) {
	log := logging.NoLog{}
	tmpDir := t.TempDir()
	clonePath := filepath.Join(tmpDir, "firewood")

	config, err := GetRepoConfig("firewood")
	require.NoError(t, err, "failed to get config")

	// Clone repo
	err = CloneRepo(log, config.GitRepo, clonePath, config.DefaultBranch, 1)
	require.NoError(t, err, "failed to clone")

	status, err := GetRepoStatus(log, "firewood", tmpDir, "")
	require.NoError(t, err, "failed to get status")
	require.True(t, status.Exists, "expected repo to exist")
	require.Equal(t, config.DefaultBranch, status.CurrentRef)
	require.False(t, status.IsDirty, "expected clean repo")
}

// TestGetRepoStatus_WithReplaceDirective tests status shows replace directive
func TestGetRepoStatus_WithReplaceDirective(t *testing.T) {
	log := logging.NoLog{}
	tmpDir := t.TempDir()
	clonePath := filepath.Join(tmpDir, "firewood")

	config, err := GetRepoConfig("firewood")
	require.NoError(t, err, "failed to get config")

	// Clone repo
	err = CloneRepo(log, config.GitRepo, clonePath, config.DefaultBranch, 1)
	require.NoError(t, err, "failed to clone")

	// Create a go.mod file
	goModPath := filepath.Join(tmpDir, "go.mod")
	goModContent := `module example.com/test

go 1.21

require github.com/ava-labs/firewood/ffi v0.0.0
`
	err = os.WriteFile(goModPath, []byte(goModContent), 0o600)
	require.NoError(t, err, "failed to write go.mod")

	// Add replace directive
	replacePath := "./firewood/ffi/result/ffi"
	err = AddReplaceDirective(log, goModPath, config.GoModule, replacePath)
	require.NoError(t, err, "failed to add replace directive")

	// Get status
	status, err := GetRepoStatus(log, "firewood", tmpDir, goModPath)
	require.NoError(t, err, "failed to get status")
	require.True(t, status.HasReplace, "expected replace directive to be detected")
	require.Equal(t, replacePath, status.ReplacePath)
}

// TestFormatRepoStatus tests the status formatting
func TestFormatRepoStatus(t *testing.T) {
	tests := []struct {
		name     string
		status   *RepoStatus
		contains string
	}{
		{
			name: "not cloned",
			status: &RepoStatus{
				Name:   "firewood",
				Exists: false,
			},
			contains: "not cloned",
		},
		{
			name: "cloned with replace",
			status: &RepoStatus{
				Name:        "firewood",
				Path:        "/tmp/firewood",
				Exists:      true,
				HasReplace:  true,
				ReplacePath: "./firewood",
			},
			contains: "replace:",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			output := FormatRepoStatus(tt.status)
			require.NotEmpty(t, output, "expected non-empty output")
			if tt.contains != "" {
				require.True(t, contains(output, tt.contains), "expected output to contain %q, got: %s", tt.contains, output)
			}
		})
	}
}

// Helper function since strings.Contains isn't imported
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 ||
		(len(s) > 0 && (s[0:len(substr)] == substr || contains(s[1:], substr))))
}

// TestGetDefaultRefForRepo_WithPseudoVersion tests that pseudo-versions from go.mod
// are properly converted to commit hashes and can be used to clone repos
func TestGetDefaultRefForRepo_WithPseudoVersion(t *testing.T) {
	log := logging.NoLog{}
	tmpDir := t.TempDir()

	// Create a go.mod with a pseudo-version dependency on avalanchego
	// Using a known commit from avalanchego's history
	goModPath := filepath.Join(tmpDir, "go.mod")
	goModContent := `module github.com/ava-labs/coreth

go 1.21

require github.com/ava-labs/avalanchego v1.13.6-0.20251007213349-63cc1a166a56
`
	err := os.WriteFile(goModPath, []byte(goModContent), 0o600)
	require.NoError(t, err, "failed to write go.mod")

	// Get the ref that should be used for syncing
	ref, err := GetDefaultRefForRepo(log, "coreth", "avalanchego", goModPath)
	require.NoError(t, err, "GetDefaultRefForRepo failed")

	// The ref should be the extracted commit hash, not the full pseudo-version
	expectedHash := "63cc1a166a56"
	require.Equal(t, expectedHash, ref)

	// Now test that we can actually clone the repo with this ref
	avalanchegoConfig, err := GetRepoConfig("avalanchego")
	require.NoError(t, err, "failed to get avalanchego config")

	clonePath := filepath.Join(tmpDir, "avalanchego")

	// Clone with the extracted commit hash (should work even with shallow clone attempt)
	err = CloneOrUpdateRepo(log, avalanchegoConfig.GitRepo, clonePath, ref, 1, false)
	require.NoError(t, err, "failed to clone avalanchego with pseudo-version ref")

	// Verify the repo was cloned
	_, err = os.Stat(filepath.Join(clonePath, ".git"))
	require.False(t, os.IsNotExist(err), "expected .git directory to exist")

	// Verify we're at the correct commit (the hash should be a prefix of the full commit)
	currentRef, err := GetCurrentRef(log, clonePath)
	require.NoError(t, err, "failed to get current ref")

	// CurrentRef returns the full 40-char hash, our extracted hash is only 12 chars
	// So we check if the current ref starts with our hash
	require.True(t, len(currentRef) >= len(expectedHash), "current ref too short")
	require.Equal(t, expectedHash, currentRef[:len(expectedHash)], "expected commit starting with %s, got %s", expectedHash, currentRef)
}

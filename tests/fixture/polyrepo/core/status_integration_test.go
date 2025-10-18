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

	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Helper functions for status integration tests

// setupTempRepo creates a temporary git repository with an initial commit
func setupTempRepo(t *testing.T) (repoPath string, cleanup func()) {
	t.Helper()
	tmpDir := t.TempDir()

	// Initialize git repo
	cmd := exec.Command("git", "init")
	cmd.Dir = tmpDir
	err := cmd.Run()
	require.NoError(t, err, "failed to init git repo")

	// Configure git user for commits
	cmd = exec.Command("git", "config", "user.email", "test@example.com")
	cmd.Dir = tmpDir
	err = cmd.Run()
	require.NoError(t, err, "failed to config git user email")

	cmd = exec.Command("git", "config", "user.name", "Test User")
	cmd.Dir = tmpDir
	err = cmd.Run()
	require.NoError(t, err, "failed to config git user name")

	// Create initial commit
	testFile := filepath.Join(tmpDir, "README.md")
	err = os.WriteFile(testFile, []byte("# Test Repo\n"), 0o600)
	require.NoError(t, err, "failed to write test file")

	cmd = exec.Command("git", "add", ".")
	cmd.Dir = tmpDir
	err = cmd.Run()
	require.NoError(t, err, "failed to git add")

	cmd = exec.Command("git", "commit", "-m", "Initial commit")
	cmd.Dir = tmpDir
	err = cmd.Run()
	require.NoError(t, err, "failed to commit")

	cleanup = func() {
		// t.TempDir() handles cleanup automatically
	}

	return tmpDir, cleanup
}

// addTag creates a tag at current HEAD
func addTag(t *testing.T, repoPath, tagName string) {
	t.Helper()
	cmd := exec.Command("git", "tag", tagName)
	cmd.Dir = repoPath
	err := cmd.Run()
	require.NoError(t, err, "failed to create tag %s", tagName)
}

// createBranch creates a branch at current HEAD
func createBranch(t *testing.T, repoPath, branchName string) {
	t.Helper()
	cmd := exec.Command("git", "branch", branchName)
	cmd.Dir = repoPath
	err := cmd.Run()
	require.NoError(t, err, "failed to create branch %s", branchName)
}

// checkoutDetached checks out a specific commit (detached HEAD)
func checkoutDetached(t *testing.T, repoPath, commitSHA string) {
	t.Helper()
	cmd := exec.Command("git", "checkout", commitSHA)
	cmd.Dir = repoPath
	err := cmd.Run()
	require.NoError(t, err, "failed to checkout commit %s", commitSHA)
}

// checkoutBranch checks out a specific branch
func checkoutBranch(t *testing.T, repoPath, branchName string) {
	t.Helper()
	cmd := exec.Command("git", "checkout", branchName)
	cmd.Dir = repoPath
	err := cmd.Run()
	require.NoError(t, err, "failed to checkout branch %s", branchName)
}

// createWorktree creates a git worktree
func createWorktree(t *testing.T, repoPath, worktreeName, ref string) (worktreePath string, cleanup func()) {
	t.Helper()

	worktreePath = filepath.Join(filepath.Dir(repoPath), worktreeName)

	cmd := exec.Command("git", "worktree", "add", worktreePath, ref)
	cmd.Dir = repoPath
	output, err := cmd.CombinedOutput()
	require.NoError(t, err, "failed to create worktree: %s", string(output))

	cleanup = func() {
		// Remove worktree
		cmd := exec.Command("git", "worktree", "remove", worktreePath, "--force")
		cmd.Dir = repoPath
		_ = cmd.Run() // Ignore errors on cleanup
	}

	return worktreePath, cleanup
}

// createGoModWithModule creates a go.mod file with specified module path
func createGoModWithModule(t *testing.T, dir, modulePath string) string {
	t.Helper()
	goModPath := filepath.Join(dir, "go.mod")
	content := "module " + modulePath + "\n\ngo 1.24\n"
	err := os.WriteFile(goModPath, []byte(content), 0o600)
	require.NoError(t, err, "failed to write go.mod")
	return goModPath
}

// addReplaceDirectiveToFile adds a replace directive to a go.mod file
func addReplaceDirectiveToFile(t *testing.T, goModPath, oldPath, newPath string) {
	t.Helper()
	log := logging.NoLog{}
	err := AddReplaceDirective(log, goModPath, oldPath, newPath)
	require.NoError(t, err, "failed to add replace directive")
}

// commitAll commits all changes in repo and returns the commit SHA
func commitAll(t *testing.T, repoPath, message string) string {
	t.Helper()

	cmd := exec.Command("git", "add", ".")
	cmd.Dir = repoPath
	err := cmd.Run()
	require.NoError(t, err, "failed to git add")

	cmd = exec.Command("git", "commit", "-m", message)
	cmd.Dir = repoPath
	err = cmd.Run()
	require.NoError(t, err, "failed to commit")

	// Get commit SHA
	cmd = exec.Command("git", "rev-parse", "HEAD")
	cmd.Dir = repoPath
	output, err := cmd.Output()
	require.NoError(t, err, "failed to get commit SHA")

	return strings.TrimSpace(string(output))
}

// getCurrentCommitSHA returns the current commit SHA
func getCurrentCommitSHA(t *testing.T, repoPath string) string {
	t.Helper()
	cmd := exec.Command("git", "rev-parse", "HEAD")
	cmd.Dir = repoPath
	output, err := cmd.Output()
	require.NoError(t, err, "failed to get commit SHA")
	return strings.TrimSpace(string(output))
}

// High Priority Test Cases - Normal Repositories

// TestGetRepoStatus_NormalRepo_DetachedHead tests status on a normal repo with detached HEAD
func TestGetRepoStatus_NormalRepo_DetachedHead(t *testing.T) {
	log := logging.NoLog{}
	repoPath, cleanup := setupTempRepo(t)
	defer cleanup()

	// Get current commit SHA
	commitSHA := getCurrentCommitSHA(t, repoPath)

	// Checkout to detached HEAD
	checkoutDetached(t, repoPath, commitSHA)

	// Get status
	status, err := GetRepoStatus(log, "test-repo", repoPath, "", true)
	require.NoError(t, err)

	// Verify
	assert.True(t, status.Exists)
	assert.Equal(t, commitSHA[:8], status.CommitSHA)
	assert.Equal(t, commitSHA, status.CurrentRef) // Detached HEAD shows full SHA
	assert.Empty(t, status.Tags)
	assert.NotEmpty(t, status.Branches) // Should have at least master/main
}

// TestGetRepoStatus_NormalRepo_OnBranch tests status on a normal repo on a branch
func TestGetRepoStatus_NormalRepo_OnBranch(t *testing.T) {
	log := logging.NoLog{}
	repoPath, cleanup := setupTempRepo(t)
	defer cleanup()

	// Get current commit SHA
	commitSHA := getCurrentCommitSHA(t, repoPath)

	// Should be on master or main branch by default
	status, err := GetRepoStatus(log, "test-repo", repoPath, "", true)
	require.NoError(t, err)

	// Verify
	assert.True(t, status.Exists)
	assert.Equal(t, commitSHA[:8], status.CommitSHA)
	assert.True(t, status.CurrentRef == "master" || status.CurrentRef == "main")
	assert.Empty(t, status.Tags)
	assert.NotEmpty(t, status.Branches) // Should show the current branch
}

// TestGetRepoStatus_NormalRepo_WithSingleTag tests status with one tag
func TestGetRepoStatus_NormalRepo_WithSingleTag(t *testing.T) {
	log := logging.NoLog{}
	repoPath, cleanup := setupTempRepo(t)
	defer cleanup()

	// Add a tag
	addTag(t, repoPath, "v1.0.0")

	commitSHA := getCurrentCommitSHA(t, repoPath)

	// Get status
	status, err := GetRepoStatus(log, "test-repo", repoPath, "", true)
	require.NoError(t, err)

	// Verify
	assert.True(t, status.Exists)
	assert.Equal(t, commitSHA[:8], status.CommitSHA)
	assert.Contains(t, status.Tags, "v1.0.0")
}

// TestGetRepoStatus_NormalRepo_WithMultipleTags tests status with multiple tags
func TestGetRepoStatus_NormalRepo_WithMultipleTags(t *testing.T) {
	log := logging.NoLog{}
	repoPath, cleanup := setupTempRepo(t)
	defer cleanup()

	// Add multiple tags at same commit
	addTag(t, repoPath, "v1.0.0")
	addTag(t, repoPath, "v1.0.1")
	addTag(t, repoPath, "latest")

	commitSHA := getCurrentCommitSHA(t, repoPath)

	// Get status
	status, err := GetRepoStatus(log, "test-repo", repoPath, "", true)
	require.NoError(t, err)

	// Verify
	assert.True(t, status.Exists)
	assert.Equal(t, commitSHA[:8], status.CommitSHA)
	assert.Len(t, status.Tags, 3)
	assert.Contains(t, status.Tags, "v1.0.0")
	assert.Contains(t, status.Tags, "v1.0.1")
	assert.Contains(t, status.Tags, "latest")
}

// TestGetRepoStatus_NormalRepo_NoTags tests status with no tags
func TestGetRepoStatus_NormalRepo_NoTags(t *testing.T) {
	log := logging.NoLog{}
	repoPath, cleanup := setupTempRepo(t)
	defer cleanup()

	commitSHA := getCurrentCommitSHA(t, repoPath)

	// Get status (no tags created)
	status, err := GetRepoStatus(log, "test-repo", repoPath, "", true)
	require.NoError(t, err)

	// Verify
	assert.True(t, status.Exists)
	assert.Equal(t, commitSHA[:8], status.CommitSHA)
	assert.Empty(t, status.Tags)
}

// TestGetRepoStatus_NormalRepo_WithMultipleBranches tests status with multiple branches at same commit
func TestGetRepoStatus_NormalRepo_WithMultipleBranches(t *testing.T) {
	log := logging.NoLog{}
	repoPath, cleanup := setupTempRepo(t)
	defer cleanup()

	// Create additional branches at current HEAD
	createBranch(t, repoPath, "feature1")
	createBranch(t, repoPath, "feature2")

	commitSHA := getCurrentCommitSHA(t, repoPath)

	// Get status
	status, err := GetRepoStatus(log, "test-repo", repoPath, "", true)
	require.NoError(t, err)

	// Verify
	assert.True(t, status.Exists)
	assert.Equal(t, commitSHA[:8], status.CommitSHA)
	assert.GreaterOrEqual(t, len(status.Branches), 3) // At least master/main, feature1, feature2
	assert.Contains(t, status.Branches, "feature1")
	assert.Contains(t, status.Branches, "feature2")
}

// TestGetRepoStatus_NormalRepo_NoBranches tests orphan commit with no branches
func TestGetRepoStatus_NormalRepo_NoBranches(t *testing.T) {
	log := logging.NoLog{}
	repoPath, cleanup := setupTempRepo(t)
	defer cleanup()

	// Create a commit and move all branches away from it
	commitSHA := getCurrentCommitSHA(t, repoPath)

	// Create a new commit
	testFile := filepath.Join(repoPath, "test.txt")
	require.NoError(t, os.WriteFile(testFile, []byte("new content"), 0o600))
	commitAll(t, repoPath, "New commit")

	// Now checkout back to the old commit (detached)
	checkoutDetached(t, repoPath, commitSHA)

	// Get status for the old commit (orphaned)
	status, err := GetRepoStatus(log, "test-repo", repoPath, "", true)
	require.NoError(t, err)

	// Verify
	assert.True(t, status.Exists)
	assert.Equal(t, commitSHA[:8], status.CommitSHA)
	// This commit should have no branches pointing to it now
	assert.Empty(t, status.Branches)
}

// High Priority Test Cases - Git Worktrees

// TestGetRepoStatus_Worktree_DetachedHead tests status on a worktree with detached HEAD
func TestGetRepoStatus_Worktree_DetachedHead(t *testing.T) {
	log := logging.NoLog{}
	repoPath, cleanup := setupTempRepo(t)
	defer cleanup()

	commitSHA := getCurrentCommitSHA(t, repoPath)

	// Create worktree at current commit
	worktreePath, cleanupWorktree := createWorktree(t, repoPath, "test-worktree", commitSHA)
	defer cleanupWorktree()

	// Get status from worktree
	status, err := GetRepoStatus(log, "test-repo", worktreePath, "", true)
	require.NoError(t, err)

	// Verify
	assert.True(t, status.Exists)
	assert.Equal(t, commitSHA[:8], status.CommitSHA)
	assert.Equal(t, commitSHA, status.CurrentRef) // Detached HEAD
}

// TestGetRepoStatus_Worktree_OnBranch tests status on a worktree on a branch
func TestGetRepoStatus_Worktree_OnBranch(t *testing.T) {
	log := logging.NoLog{}
	repoPath, cleanup := setupTempRepo(t)
	defer cleanup()

	// Create a new branch
	createBranch(t, repoPath, "worktree-branch")

	// Create worktree on the new branch
	worktreePath, cleanupWorktree := createWorktree(t, repoPath, "test-worktree", "worktree-branch")
	defer cleanupWorktree()

	commitSHA := getCurrentCommitSHA(t, worktreePath)

	// Get status from worktree
	status, err := GetRepoStatus(log, "test-repo", worktreePath, "", true)
	require.NoError(t, err)

	// Verify
	assert.True(t, status.Exists)
	assert.Equal(t, commitSHA[:8], status.CommitSHA)
	assert.Equal(t, "worktree-branch", status.CurrentRef)
	assert.Contains(t, status.Branches, "worktree-branch")
}

// TestGetRepoStatus_Worktree_WithTags tests that tags are correctly resolved from commondir
func TestGetRepoStatus_Worktree_WithTags(t *testing.T) {
	log := logging.NoLog{}
	repoPath, cleanup := setupTempRepo(t)
	defer cleanup()

	// Add tags in main repo
	addTag(t, repoPath, "v1.0.0")
	addTag(t, repoPath, "v1.0.1")

	commitSHA := getCurrentCommitSHA(t, repoPath)

	// Create worktree
	worktreePath, cleanupWorktree := createWorktree(t, repoPath, "test-worktree", "HEAD")
	defer cleanupWorktree()

	// Get status from worktree
	status, err := GetRepoStatus(log, "test-repo", worktreePath, "", true)
	require.NoError(t, err)

	// Verify tags are visible from worktree
	assert.True(t, status.Exists)
	assert.Equal(t, commitSHA[:8], status.CommitSHA)
	assert.Contains(t, status.Tags, "v1.0.0")
	assert.Contains(t, status.Tags, "v1.0.1")
}

// TestGetRepoStatus_Worktree_CommonDirResolution tests that commondir is correctly found
func TestGetRepoStatus_Worktree_CommonDirResolution(t *testing.T) {
	log := logging.NoLog{}
	repoPath, cleanup := setupTempRepo(t)
	defer cleanup()

	// Create worktree
	worktreePath, cleanupWorktree := createWorktree(t, repoPath, "test-worktree", "HEAD")
	defer cleanupWorktree()

	// Verify .git is a file in worktree
	gitPath := filepath.Join(worktreePath, ".git")
	info, err := os.Stat(gitPath)
	require.NoError(t, err)
	assert.False(t, info.IsDir(), ".git should be a file in worktree, not a directory")

	// Read .git file content
	content, err := os.ReadFile(gitPath)
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(string(content), "gitdir:"), ".git file should contain gitdir reference")

	// Get status should work despite worktree structure
	status, err := GetRepoStatus(log, "test-repo", worktreePath, "", true)
	require.NoError(t, err)
	assert.True(t, status.Exists)
}

// High Priority Test Cases - Replace Directives

// TestGetRepoStatus_NoReplaces tests status with no replace directives
func TestGetRepoStatus_NoReplaces(t *testing.T) {
	log := logging.NoLog{}
	repoPath, cleanup := setupTempRepo(t)
	defer cleanup()

	// Create go.mod without replace directives
	goModPath := createGoModWithModule(t, repoPath, "github.com/example/test")

	// Get status
	status, err := GetRepoStatus(log, "test-repo", repoPath, goModPath, true)
	require.NoError(t, err)

	// Verify
	assert.True(t, status.Exists)
	assert.Empty(t, status.Replacements)
}

// TestGetRepoStatus_WithLocalReplaces tests status with local path replace directives
func TestGetRepoStatus_WithLocalReplaces(t *testing.T) {
	log := logging.NoLog{}
	repoPath, cleanup := setupTempRepo(t)
	defer cleanup()

	// Create go.mod with dependencies
	goModPath := filepath.Join(repoPath, "go.mod")
	content := `module github.com/example/test

go 1.24

require (
	github.com/ava-labs/coreth v0.15.4
	github.com/ava-labs/firewood-go-ethhash v0.0.12
)
`
	require.NoError(t, os.WriteFile(goModPath, []byte(content), 0o600))

	// Add replace directives for local paths
	addReplaceDirectiveToFile(t, goModPath, "github.com/ava-labs/coreth", "./coreth")
	addReplaceDirectiveToFile(t, goModPath, "github.com/ava-labs/firewood-go-ethhash", "./firewood/ffi")

	// Get status
	status, err := GetRepoStatus(log, "test-repo", repoPath, goModPath, true)
	require.NoError(t, err)

	// Verify
	assert.True(t, status.Exists)
	assert.Len(t, status.Replacements, 2)
	assert.Equal(t, "./coreth", status.Replacements["github.com/ava-labs/coreth"])
	assert.Equal(t, "./firewood/ffi", status.Replacements["github.com/ava-labs/firewood-go-ethhash"])
}

// TestGetRepoStatus_WithVersionReplaces tests status with version replace directives
func TestGetRepoStatus_WithVersionReplaces(t *testing.T) {
	log := logging.NoLog{}
	repoPath, cleanup := setupTempRepo(t)
	defer cleanup()

	// Create go.mod with dependencies
	goModPath := filepath.Join(repoPath, "go.mod")
	content := `module github.com/example/test

go 1.24

require github.com/ava-labs/coreth v0.15.4
`
	require.NoError(t, os.WriteFile(goModPath, []byte(content), 0o600))

	// Add replace directive with version
	addReplaceDirectiveToFile(t, goModPath, "github.com/ava-labs/coreth", "github.com/ava-labs/coreth v0.15.5")

	// Get status
	status, err := GetRepoStatus(log, "test-repo", repoPath, goModPath, true)
	require.NoError(t, err)

	// Verify
	assert.True(t, status.Exists)
	assert.Len(t, status.Replacements, 1)
	assert.Equal(t, "github.com/ava-labs/coreth v0.15.5", status.Replacements["github.com/ava-labs/coreth"])
}

// TestGetRepoStatus_MixedReplaces tests status with both local and version replace directives
func TestGetRepoStatus_MixedReplaces(t *testing.T) {
	log := logging.NoLog{}
	repoPath, cleanup := setupTempRepo(t)
	defer cleanup()

	// Create go.mod with dependencies
	goModPath := filepath.Join(repoPath, "go.mod")
	content := `module github.com/example/test

go 1.24

require (
	github.com/ava-labs/coreth v0.15.4
	github.com/ava-labs/firewood-go-ethhash v0.0.12
)
`
	require.NoError(t, os.WriteFile(goModPath, []byte(content), 0o600))

	// Add mixed replace directives
	addReplaceDirectiveToFile(t, goModPath, "github.com/ava-labs/coreth", "./coreth")
	addReplaceDirectiveToFile(t, goModPath, "github.com/ava-labs/firewood-go-ethhash", "github.com/ava-labs/firewood-go-ethhash v0.0.13")

	// Get status
	status, err := GetRepoStatus(log, "test-repo", repoPath, goModPath, true)
	require.NoError(t, err)

	// Verify
	assert.True(t, status.Exists)
	assert.Len(t, status.Replacements, 2)
	assert.Equal(t, "./coreth", status.Replacements["github.com/ava-labs/coreth"])
	assert.Equal(t, "github.com/ava-labs/firewood-go-ethhash v0.0.13", status.Replacements["github.com/ava-labs/firewood-go-ethhash"])
}

// TestGetRepoStatus_PrimaryVsSyncedReplacements tests that primary and synced repos
// read their own go.mod files correctly
func TestGetRepoStatus_PrimaryVsSyncedReplacements(t *testing.T) {
	log := logging.NoLog{}
	tmpDir := t.TempDir()

	// Create primary repo go.mod with replace
	primaryGoModPath := filepath.Join(tmpDir, "go.mod")
	primaryContent := `module github.com/ava-labs/avalanchego

go 1.24

require github.com/ava-labs/coreth v0.15.4
`
	require.NoError(t, os.WriteFile(primaryGoModPath, []byte(primaryContent), 0o600))
	addReplaceDirectiveToFile(t, primaryGoModPath, "github.com/ava-labs/coreth", "./coreth")

	// Create synced coreth repo with go.mod and replace
	corethDir := filepath.Join(tmpDir, "coreth")
	require.NoError(t, os.MkdirAll(corethDir, 0o755))

	corethGoModPath := filepath.Join(corethDir, "go.mod")
	corethContent := `module github.com/ava-labs/coreth

go 1.24

require github.com/ava-labs/avalanchego v1.13.6
`
	require.NoError(t, os.WriteFile(corethGoModPath, []byte(corethContent), 0o600))
	addReplaceDirectiveToFile(t, corethGoModPath, "github.com/ava-labs/avalanchego", "..")

	// Get status for primary repo (isPrimary = true)
	primaryStatus, err := GetRepoStatus(log, "avalanchego", tmpDir, primaryGoModPath, true)
	require.NoError(t, err)

	// Verify primary repo replacements
	assert.Equal(t, tmpDir, primaryStatus.Path)
	assert.Len(t, primaryStatus.Replacements, 1)
	assert.Equal(t, "./coreth", primaryStatus.Replacements["github.com/ava-labs/coreth"])

	// Get status for synced repo (isPrimary = false)
	syncedStatus, err := GetRepoStatus(log, "coreth", tmpDir, primaryGoModPath, false)
	require.NoError(t, err)

	// Verify synced repo replacements
	assert.Equal(t, corethDir, syncedStatus.Path)
	assert.Len(t, syncedStatus.Replacements, 1)
	assert.Equal(t, "..", syncedStatus.Replacements["github.com/ava-labs/avalanchego"])
}

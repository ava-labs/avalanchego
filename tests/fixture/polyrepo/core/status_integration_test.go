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

	"github.com/ava-labs/avalanchego/tests/fixture/polyrepo/internal/logging"
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

// TestGetRepoStatus_NormalRepo tests status on normal repositories with various git states
func TestGetRepoStatus_NormalRepo(t *testing.T) {
	log := logging.NoLog{}

	tests := []struct {
		name           string
		setupGit       func(t *testing.T, repoPath string) string // Returns expected commitSHA
		validateStatus func(t *testing.T, status *RepoStatus, commitSHA string)
	}{
		{
			name: "detached HEAD",
			setupGit: func(t *testing.T, repoPath string) string {
				commitSHA := getCurrentCommitSHA(t, repoPath)
				checkoutDetached(t, repoPath, commitSHA)
				return commitSHA
			},
			validateStatus: func(t *testing.T, status *RepoStatus, commitSHA string) {
				assert.True(t, status.Exists)
				assert.Equal(t, commitSHA[:8], status.CommitSHA)
				assert.Equal(t, commitSHA, status.CurrentRef) // Detached HEAD shows full SHA
				assert.Empty(t, status.Tags)
				assert.NotEmpty(t, status.Branches) // Should have at least master/main
			},
		},
		{
			name: "on branch",
			setupGit: func(t *testing.T, repoPath string) string {
				// No setup needed - default state is on master/main
				return getCurrentCommitSHA(t, repoPath)
			},
			validateStatus: func(t *testing.T, status *RepoStatus, commitSHA string) {
				assert.True(t, status.Exists)
				assert.Equal(t, commitSHA[:8], status.CommitSHA)
				assert.True(t, status.CurrentRef == "master" || status.CurrentRef == "main")
				assert.Empty(t, status.Tags)
				assert.NotEmpty(t, status.Branches) // Should show the current branch
			},
		},
		{
			name: "with single tag",
			setupGit: func(t *testing.T, repoPath string) string {
				addTag(t, repoPath, "v1.0.0")
				return getCurrentCommitSHA(t, repoPath)
			},
			validateStatus: func(t *testing.T, status *RepoStatus, commitSHA string) {
				assert.True(t, status.Exists)
				assert.Equal(t, commitSHA[:8], status.CommitSHA)
				assert.Contains(t, status.Tags, "v1.0.0")
			},
		},
		{
			name: "with multiple tags",
			setupGit: func(t *testing.T, repoPath string) string {
				addTag(t, repoPath, "v1.0.0")
				addTag(t, repoPath, "v1.0.1")
				addTag(t, repoPath, "latest")
				return getCurrentCommitSHA(t, repoPath)
			},
			validateStatus: func(t *testing.T, status *RepoStatus, commitSHA string) {
				assert.True(t, status.Exists)
				assert.Equal(t, commitSHA[:8], status.CommitSHA)
				assert.Len(t, status.Tags, 3)
				assert.Contains(t, status.Tags, "v1.0.0")
				assert.Contains(t, status.Tags, "v1.0.1")
				assert.Contains(t, status.Tags, "latest")
			},
		},
		{
			name: "no tags",
			setupGit: func(t *testing.T, repoPath string) string {
				// No tags added
				return getCurrentCommitSHA(t, repoPath)
			},
			validateStatus: func(t *testing.T, status *RepoStatus, commitSHA string) {
				assert.True(t, status.Exists)
				assert.Equal(t, commitSHA[:8], status.CommitSHA)
				assert.Empty(t, status.Tags)
			},
		},
		{
			name: "with multiple branches",
			setupGit: func(t *testing.T, repoPath string) string {
				createBranch(t, repoPath, "feature1")
				createBranch(t, repoPath, "feature2")
				return getCurrentCommitSHA(t, repoPath)
			},
			validateStatus: func(t *testing.T, status *RepoStatus, commitSHA string) {
				assert.True(t, status.Exists)
				assert.Equal(t, commitSHA[:8], status.CommitSHA)
				assert.GreaterOrEqual(t, len(status.Branches), 3) // At least master/main, feature1, feature2
				assert.Contains(t, status.Branches, "feature1")
				assert.Contains(t, status.Branches, "feature2")
			},
		},
		{
			name: "no branches (orphan commit)",
			setupGit: func(t *testing.T, repoPath string) string {
				// Create a commit and move all branches away from it
				commitSHA := getCurrentCommitSHA(t, repoPath)

				// Create a new commit
				testFile := filepath.Join(repoPath, "test.txt")
				require.NoError(t, os.WriteFile(testFile, []byte("new content"), 0o600))
				commitAll(t, repoPath, "New commit")

				// Now checkout back to the old commit (detached)
				checkoutDetached(t, repoPath, commitSHA)

				return commitSHA
			},
			validateStatus: func(t *testing.T, status *RepoStatus, commitSHA string) {
				assert.True(t, status.Exists)
				assert.Equal(t, commitSHA[:8], status.CommitSHA)
				// This commit should have no branches pointing to it now
				assert.Empty(t, status.Branches)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repoPath, cleanup := setupTempRepo(t)
			defer cleanup()

			commitSHA := tt.setupGit(t, repoPath)

			status, err := GetRepoStatus(log, "avalanchego", repoPath, "", true)
			require.NoError(t, err)

			tt.validateStatus(t, status, commitSHA)
		})
	}
}

// High Priority Test Cases - Git Worktrees

// TestGetRepoStatus_Worktree tests status on git worktrees with various configurations
func TestGetRepoStatus_Worktree(t *testing.T) {
	log := logging.NoLog{}

	tests := []struct {
		name           string
		setupGit       func(t *testing.T, repoPath string) (worktreePath string, cleanup func())
		validateStatus func(t *testing.T, status *RepoStatus, repoPath string)
	}{
		{
			name: "detached HEAD",
			setupGit: func(t *testing.T, repoPath string) (string, func()) {
				commitSHA := getCurrentCommitSHA(t, repoPath)
				worktreePath, cleanup := createWorktree(t, repoPath, "test-worktree", commitSHA)
				return worktreePath, cleanup
			},
			validateStatus: func(t *testing.T, status *RepoStatus, repoPath string) {
				commitSHA := getCurrentCommitSHA(t, repoPath)
				assert.True(t, status.Exists)
				assert.Equal(t, commitSHA[:8], status.CommitSHA)
				assert.Equal(t, commitSHA, status.CurrentRef) // Detached HEAD
			},
		},
		{
			name: "on branch",
			setupGit: func(t *testing.T, repoPath string) (string, func()) {
				createBranch(t, repoPath, "worktree-branch")
				worktreePath, cleanup := createWorktree(t, repoPath, "test-worktree", "worktree-branch")
				return worktreePath, cleanup
			},
			validateStatus: func(t *testing.T, status *RepoStatus, repoPath string) {
				commitSHA := getCurrentCommitSHA(t, repoPath)
				assert.True(t, status.Exists)
				assert.Equal(t, commitSHA[:8], status.CommitSHA)
				assert.Equal(t, "worktree-branch", status.CurrentRef)
				assert.Contains(t, status.Branches, "worktree-branch")
			},
		},
		{
			name: "with tags visible from commondir",
			setupGit: func(t *testing.T, repoPath string) (string, func()) {
				addTag(t, repoPath, "v1.0.0")
				addTag(t, repoPath, "v1.0.1")
				worktreePath, cleanup := createWorktree(t, repoPath, "test-worktree", "HEAD")
				return worktreePath, cleanup
			},
			validateStatus: func(t *testing.T, status *RepoStatus, repoPath string) {
				commitSHA := getCurrentCommitSHA(t, repoPath)
				assert.True(t, status.Exists)
				assert.Equal(t, commitSHA[:8], status.CommitSHA)
				assert.Contains(t, status.Tags, "v1.0.0")
				assert.Contains(t, status.Tags, "v1.0.1")
			},
		},
		{
			name: "commondir resolution",
			setupGit: func(t *testing.T, repoPath string) (string, func()) {
				worktreePath, cleanup := createWorktree(t, repoPath, "test-worktree", "HEAD")
				return worktreePath, cleanup
			},
			validateStatus: func(t *testing.T, status *RepoStatus, repoPath string) {
				// Verify .git is a file in worktree
				gitPath := filepath.Join(status.Path, ".git")
				info, err := os.Stat(gitPath)
				require.NoError(t, err)
				assert.False(t, info.IsDir(), ".git should be a file in worktree, not a directory")

				// Read .git file content
				content, err := os.ReadFile(gitPath)
				require.NoError(t, err)
				assert.True(t, strings.HasPrefix(string(content), "gitdir:"), ".git file should contain gitdir reference")

				// Status should work despite worktree structure
				assert.True(t, status.Exists)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repoPath, cleanup := setupTempRepo(t)
			defer cleanup()

			worktreePath, cleanupWorktree := tt.setupGit(t, repoPath)
			defer cleanupWorktree()

			status, err := GetRepoStatus(log, "avalanchego", worktreePath, "", true)
			require.NoError(t, err)

			tt.validateStatus(t, status, repoPath)
		})
	}
}

// High Priority Test Cases - Replace Directives

// TestGetRepoStatus_ReplaceDirectives tests status with various replace directive configurations
func TestGetRepoStatus_ReplaceDirectives(t *testing.T) {
	log := logging.NoLog{}

	tests := []struct {
		name           string
		setupGoMod     func(t *testing.T, repoPath string) string // Returns goModPath
		validateStatus func(t *testing.T, status *RepoStatus)
	}{
		{
			name: "no replaces",
			setupGoMod: func(t *testing.T, repoPath string) string {
				return createGoModWithModule(t, repoPath, "github.com/example/test")
			},
			validateStatus: func(t *testing.T, status *RepoStatus) {
				assert.True(t, status.Exists)
				assert.Empty(t, status.Replacements)
			},
		},
		{
			name: "with local replaces",
			setupGoMod: func(t *testing.T, repoPath string) string {
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

				return goModPath
			},
			validateStatus: func(t *testing.T, status *RepoStatus) {
				assert.True(t, status.Exists)
				assert.Len(t, status.Replacements, 2)
				assert.Equal(t, "./coreth", status.Replacements["github.com/ava-labs/coreth"])
				assert.Equal(t, "./firewood/ffi", status.Replacements["github.com/ava-labs/firewood-go-ethhash"])
			},
		},
		{
			name: "with version replaces",
			setupGoMod: func(t *testing.T, repoPath string) string {
				goModPath := filepath.Join(repoPath, "go.mod")
				content := `module github.com/example/test

go 1.24

require github.com/ava-labs/coreth v0.15.4
`
				require.NoError(t, os.WriteFile(goModPath, []byte(content), 0o600))

				// Add replace directive with version
				addReplaceDirectiveToFile(t, goModPath, "github.com/ava-labs/coreth", "github.com/ava-labs/coreth v0.15.5")

				return goModPath
			},
			validateStatus: func(t *testing.T, status *RepoStatus) {
				assert.True(t, status.Exists)
				assert.Len(t, status.Replacements, 1)
				assert.Equal(t, "github.com/ava-labs/coreth v0.15.5", status.Replacements["github.com/ava-labs/coreth"])
			},
		},
		{
			name: "mixed replaces",
			setupGoMod: func(t *testing.T, repoPath string) string {
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

				return goModPath
			},
			validateStatus: func(t *testing.T, status *RepoStatus) {
				assert.True(t, status.Exists)
				assert.Len(t, status.Replacements, 2)
				assert.Equal(t, "./coreth", status.Replacements["github.com/ava-labs/coreth"])
				assert.Equal(t, "github.com/ava-labs/firewood-go-ethhash v0.0.13", status.Replacements["github.com/ava-labs/firewood-go-ethhash"])
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repoPath, cleanup := setupTempRepo(t)
			defer cleanup()

			goModPath := tt.setupGoMod(t, repoPath)

			status, err := GetRepoStatus(log, "avalanchego", repoPath, goModPath, true)
			require.NoError(t, err)

			tt.validateStatus(t, status)
		})
	}
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

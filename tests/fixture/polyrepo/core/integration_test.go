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

// TestGitOperations_SmokeTest is a comprehensive smoke test that validates
// the core git operations work with real repositories:
// - Clone a real repo (firewood, smallest repo)
// - Detect repo status (current ref, clean/dirty)
// - Update to a different ref
// - Handle branches, tags, and commit SHAs
func TestGitOperations_SmokeTest(t *testing.T) {
	log := logging.NoLog{}
	tmpDir := t.TempDir()
	clonePath := filepath.Join(tmpDir, "firewood")

	config, err := GetRepoConfig("firewood")
	require.NoError(t, err, "failed to get config")

	// 1. Clone with default branch and shallow depth
	err = CloneRepo(log, config.GitRepo, clonePath, config.DefaultBranch, 1)
	require.NoError(t, err, "failed to clone")

	// Verify the repo was cloned
	_, err = os.Stat(filepath.Join(clonePath, ".git"))
	require.False(t, os.IsNotExist(err), "expected .git directory to exist")

	// 2. Verify we can get the current ref
	currentRef, err := GetCurrentRef(log, clonePath)
	require.NoError(t, err, "failed to get current ref")
	require.Equal(t, config.DefaultBranch, currentRef)

	// 3. Check dirty status (should be clean)
	isDirty, err := IsRepoDirty(log, clonePath)
	require.NoError(t, err, "failed to check dirty status")
	require.False(t, isDirty, "expected clean repo")

	// 4. Make a change to the repo
	testFile := filepath.Join(clonePath, "test_file.txt")
	err = os.WriteFile(testFile, []byte("test content"), 0o600)
	require.NoError(t, err, "failed to write test file")

	// Check dirty status (should be dirty now)
	isDirty, err = IsRepoDirty(log, clonePath)
	require.NoError(t, err, "failed to check dirty status")
	require.True(t, isDirty, "expected dirty repo")

	// 5. Update to a known tag (with force to handle dirty repo)
	tag := "v0.0.12"
	err = CloneOrUpdateRepo(log, config.GitRepo, clonePath, tag, 1, true)
	require.NoError(t, err, "failed to update to tag")

	// Verify we're at a different ref
	updatedRef, err := GetCurrentRef(log, clonePath)
	require.NoError(t, err, "failed to get updated ref")
	require.NotEqual(t, currentRef, updatedRef, "ref should have changed")

	// Verify the tag is present
	tags, err := GetTagsForCommit(log, clonePath, updatedRef)
	require.NoError(t, err, "failed to get tags")
	require.Contains(t, tags, tag, "expected commit to have tag %s", tag)
}

// TestCloneRepo_WithTag tests cloning a real repository using a tag reference
// This validates the bug fix where tags were treated as branches (refs/heads/ vs refs/tags/)
// CRITICAL: This test validates a specific bug fix and should not be removed
func TestCloneRepo_WithTag(t *testing.T) {
	log := logging.NoLog{}
	tmpDir := t.TempDir()
	clonePath := filepath.Join(tmpDir, "coreth")

	// Get coreth config
	config, err := GetRepoConfig("coreth")
	require.NoError(t, err, "failed to get config")

	// Use a known tag from coreth repository
	// This was the actual tag that failed in the user's report
	tag := "v0.15.4-rc.4"

	// Clone with tag reference and shallow depth
	// This should work but previously failed with "couldn't find remote ref refs/heads/v0.15.4-rc.4"
	err = CloneRepo(log, config.GitRepo, clonePath, tag, 1)
	require.NoError(t, err, "failed to clone with tag reference")

	// Verify the repo was cloned
	_, err = os.Stat(filepath.Join(clonePath, ".git"))
	require.False(t, os.IsNotExist(err), "expected .git directory to exist")

	// Verify we can get the current ref
	// When checked out to a tag, GetCurrentRef returns the commit SHA, not the tag name
	currentRef, err := GetCurrentRef(log, clonePath)
	require.NoError(t, err, "failed to get current ref")
	require.NotEmpty(t, currentRef, "expected non-empty current ref")

	// Verify the commit has the expected tag
	tags, err := GetTagsForCommit(log, clonePath, currentRef)
	require.NoError(t, err, "failed to get tags for commit")
	require.Contains(t, tags, tag, "expected commit to have tag %s", tag)
}

// TestCloneOrUpdateRepo_ShallowToTagWithForce tests updating from a shallow clone to a specific tag with force
// This simulates: polyrepo sync firewood (shallow clone main) then polyrepo sync firewood@v0.0.12 --force
// CRITICAL: This test validates an important edge case and should not be removed
func TestCloneOrUpdateRepo_ShallowToTagWithForce(t *testing.T) {
	log := logging.NoLog{}
	tmpDir := t.TempDir()
	clonePath := filepath.Join(tmpDir, "firewood")

	config, err := GetRepoConfig("firewood")
	require.NoError(t, err, "failed to get config")

	// Step 1: Clone with default branch and shallow depth (simulates: polyrepo sync firewood)
	err = CloneOrUpdateRepo(log, config.GitRepo, clonePath, config.DefaultBranch, 1, false)
	require.NoError(t, err, "failed to do initial shallow clone")

	// Verify it's a shallow clone
	require.True(t, isShallowRepo(clonePath), "expected shallow clone")

	// Get initial ref
	initialRef, err := GetCurrentRef(log, clonePath)
	require.NoError(t, err, "failed to get initial ref")

	// Step 2: Update to a specific tag with force (simulates: polyrepo sync firewood@v0.0.12 --force)
	tag := "v0.0.12"
	err = CloneOrUpdateRepo(log, config.GitRepo, clonePath, tag, 1, true)
	require.NoError(t, err, "failed to update shallow clone to tag with force")

	// Verify we're at a different commit
	currentRef, err := GetCurrentRef(log, clonePath)
	require.NoError(t, err, "failed to get current ref")
	require.NotEqual(t, initialRef, currentRef, "ref should have changed")

	// Verify the tag is on this commit
	tags, err := GetTagsForCommit(log, clonePath, currentRef)
	require.NoError(t, err, "failed to get tags")
	require.Contains(t, tags, tag, "expected commit to have tag %s", tag)

	t.Logf("Test passed: successfully updated shallow clone from %s to tag %s", initialRef, tag)
}

// TestGetDefaultRefForRepo_WithPseudoVersion tests that pseudo-versions from go.mod
// are properly converted to commit hashes and can be used to clone repos
// CRITICAL: This test validates pseudo-version parsing and should not be removed
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

// TestSync_EndToEnd_SmokeTest is a comprehensive smoke test that validates
// the complete sync workflow from primary repo mode:
// - Creates a primary repo (avalanchego-like go.mod)
// - Syncs coreth and firewood
// - Verifies repos are cloned at correct versions
// - Verifies replace directives are correctly set in all repos
func TestSync_EndToEnd_SmokeTest(t *testing.T) {
	log := logging.NoLog{}
	tmpDir := t.TempDir()

	// Setup: Create avalanchego-like go.mod in tmpDir
	goModPath := filepath.Join(tmpDir, "go.mod")
	goModContent := `module github.com/ava-labs/avalanchego

go 1.21

require (
	github.com/ava-labs/coreth v0.13.8
	github.com/ava-labs/firewood-go-ethhash/ffi v0.0.12
)
`
	err := os.WriteFile(goModPath, []byte(goModContent), 0o600)
	require.NoError(t, err, "failed to write go.mod")

	// Execute: Run full sync with explicit versions
	// Note: We skip nix build for firewood in tests (too slow)
	avalanchegoConfig, _ := GetRepoConfig("avalanchego")
	corethConfig, _ := GetRepoConfig("coreth")
	firewoodConfig, _ := GetRepoConfig("firewood")

	// Clone coreth
	corethPath := filepath.Join(tmpDir, "coreth")
	err = CloneOrUpdateRepo(log, corethConfig.GitRepo, corethPath, "v0.13.8", 1, false)
	require.NoError(t, err, "failed to clone coreth")

	// Clone firewood (skip nix build)
	firewoodPath := filepath.Join(tmpDir, "firewood")
	err = CloneOrUpdateRepo(log, firewoodConfig.GitRepo, firewoodPath, "main", 1, false)
	require.NoError(t, err, "failed to clone firewood")

	// Create firewood's ffi/go.mod (needed for replace directive detection)
	// Note: firewood's go.mod uses the "internal" module name
	firewoodFFIDir := filepath.Join(firewoodPath, "ffi")
	require.NoError(t, os.MkdirAll(firewoodFFIDir, 0o755))
	firewoodGoModPath := filepath.Join(firewoodFFIDir, "go.mod")
	firewoodGoModContent := `module github.com/ava-labs/firewood-go-ethhash/ffi

go 1.21
`
	err = os.WriteFile(firewoodGoModPath, []byte(firewoodGoModContent), 0o600)
	require.NoError(t, err, "failed to write firewood go.mod")

	// Create coreth's go.mod with avalanchego and firewood dependencies
	corethGoModPath := filepath.Join(corethPath, "go.mod")
	corethGoModContent := `module github.com/ava-labs/coreth

go 1.21

require (
	github.com/ava-labs/avalanchego v1.13.6
	github.com/ava-labs/firewood-go-ethhash/ffi v0.0.12
)
`
	err = os.WriteFile(corethGoModPath, []byte(corethGoModContent), 0o600)
	require.NoError(t, err, "failed to write coreth go.mod")

	// Update all replace directives (the critical function being tested)
	syncedRepos := []string{"coreth", "firewood"}
	err = UpdateAllReplaceDirectives(log, tmpDir, syncedRepos)
	require.NoError(t, err, "UpdateAllReplaceDirectives failed")

	// Validate: Check multiple outcomes

	// 1. Repos were cloned
	require.DirExists(t, corethPath, "coreth should be cloned")
	require.DirExists(t, firewoodPath, "firewood should be cloned")

	// 2. Coreth is at correct version
	corethRef, err := GetCurrentRef(log, corethPath)
	require.NoError(t, err, "failed to get coreth ref")
	// v0.13.8 tag should be on this commit
	tags, err := GetTagsForCommit(log, corethPath, corethRef)
	require.NoError(t, err, "failed to get tags")
	require.Contains(t, tags, "v0.13.8", "coreth should be at v0.13.8")

	// 3. Primary repo (avalanchego) has replace directives
	primaryMod, err := ReadGoMod(log, goModPath)
	require.NoError(t, err, "failed to read primary go.mod")

	primaryReplaces := make(map[string]string)
	for _, r := range primaryMod.Replace {
		primaryReplaces[r.Old.Path] = r.New.Path
	}

	require.NotContains(t, primaryReplaces, avalanchegoConfig.GoModule, "should NOT have avalanchego replace (we are avalanchego)")
	require.Contains(t, primaryReplaces, corethConfig.GoModule, "avalanchego should have coreth replace")
	require.Equal(t, "./coreth", primaryReplaces[corethConfig.GoModule])
	require.Contains(t, primaryReplaces, firewoodConfig.GoModule, "avalanchego should have firewood replace")
	require.Equal(t, "./firewood/ffi/result/ffi", primaryReplaces[firewoodConfig.GoModule])

	// 4. Coreth has replace directives for avalanchego and firewood
	corethMod, err := ReadGoMod(log, corethGoModPath)
	require.NoError(t, err, "failed to read coreth go.mod")

	corethReplaces := make(map[string]string)
	for _, r := range corethMod.Replace {
		corethReplaces[r.Old.Path] = r.New.Path
	}

	require.Contains(t, corethReplaces, avalanchegoConfig.GoModule, "coreth should have avalanchego replace")
	require.Equal(t, "..", corethReplaces[avalanchegoConfig.GoModule])
	require.Contains(t, corethReplaces, firewoodConfig.GoModule, "coreth should have firewood replace")
	require.Equal(t, "../firewood/ffi/result/ffi", corethReplaces[firewoodConfig.GoModule])

	// 5. Firewood has no replace directives (no dependencies on other repos)
	firewoodMod, err := ReadGoMod(log, firewoodGoModPath)
	require.NoError(t, err, "failed to read firewood go.mod")
	require.Empty(t, firewoodMod.Replace, "firewood should have no replace directives")

	t.Logf("Sync end-to-end test passed: all repos cloned, all replace directives correct")
}

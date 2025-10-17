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

// TestCloneOrUpdateRepo_ShallowToSHAWithForce tests updating from a shallow clone to a specific SHA with force
// This simulates: polyrepo sync firewood (shallow clone main) then polyrepo sync firewood@SHA --force
func TestCloneOrUpdateRepo_ShallowToSHAWithForce(t *testing.T) {
	// Use real logging to see what's happening
	logFactory := logging.NewFactory(logging.Config{
		DisplayLevel: logging.Debug,
		LogLevel:     logging.Debug,
	})
	log, err := logFactory.Make("test")
	require.NoError(t, err, "failed to create logger")

	tmpDir := t.TempDir()
	clonePath := filepath.Join(tmpDir, "firewood")

	config, err := GetRepoConfig("firewood")
	require.NoError(t, err, "failed to get config")

	// Step 1: Clone with default branch and shallow depth (simulates: polyrepo sync firewood)
	err = CloneOrUpdateRepo(log, config.GitRepo, clonePath, config.DefaultBranch, 1, false)
	require.NoError(t, err, "failed to do initial shallow clone")

	// Verify it's a shallow clone
	require.True(t, isShallowRepo(clonePath), "expected shallow clone")

	// Step 2: Update to a specific SHA with force (simulates: polyrepo sync firewood@SHA --force)
	// Using a known commit from firewood that has the nix flake
	commitSHA := "7cd05ccda8baba48617de19684db7da9ba73f8ba"
	err = CloneOrUpdateRepo(log, config.GitRepo, clonePath, commitSHA, 1, true)
	require.NoError(t, err, "failed to update shallow clone to SHA with force")

	// Verify we're at the correct commit
	currentRef, err := GetCurrentRef(log, clonePath)
	require.NoError(t, err, "failed to get current ref")
	require.Equal(t, commitSHA, currentRef, "expected to be at the specified SHA")

	// Check shallow status and log details
	isShallow := isShallowRepo(clonePath)
	shallowPath := filepath.Join(clonePath, ".git", "shallow")
	shallowFileInfo, _ := os.Stat(shallowPath)

	// Read shallow file contents
	shallowContents := []byte{}
	if shallowFileInfo != nil {
		shallowContents, _ = os.ReadFile(shallowPath)
	}

	t.Logf("isShallow=%v, shallowPath=%s, shallowFileExists=%v, shallowContents=%q",
		isShallow, shallowPath, shallowFileInfo != nil, string(shallowContents))

	// The key test: verify the SHA was successfully checked out
	// Note: The repo may still be shallow if go-git's fetch was able to get the commit
	// without needing to unshallow. This is actually fine - the important thing is that
	// the operation succeeded when it previously would have failed.
	t.Logf("Test passed: successfully updated shallow clone to SHA %s", commitSHA)
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

// TestUpdateAllReplaceDirectives_FromAvalanchego tests that syncing from avalanchego
// properly updates replace directives in all repos including synced repos
func TestUpdateAllReplaceDirectives_FromAvalanchego(t *testing.T) {
	log := logging.NoLog{}
	tmpDir := t.TempDir()

	// Create a primary avalanchego go.mod
	primaryGoModPath := filepath.Join(tmpDir, "go.mod")
	primaryGoModContent := `module github.com/ava-labs/avalanchego

go 1.24

require (
	github.com/ava-labs/coreth v0.15.4
	github.com/ava-labs/firewood/ffi v0.0.12
)
`
	err := os.WriteFile(primaryGoModPath, []byte(primaryGoModContent), 0o600)
	require.NoError(t, err, "failed to write primary go.mod")

	// Create coreth directory with go.mod
	corethDir := filepath.Join(tmpDir, "coreth")
	err = os.MkdirAll(corethDir, 0o755)
	require.NoError(t, err, "failed to create coreth dir")

	corethGoModPath := filepath.Join(corethDir, "go.mod")
	corethGoModContent := `module github.com/ava-labs/coreth

go 1.24

require (
	github.com/ava-labs/avalanchego v1.13.6
	github.com/ava-labs/firewood/ffi v0.0.12
)
`
	err = os.WriteFile(corethGoModPath, []byte(corethGoModContent), 0o600)
	require.NoError(t, err, "failed to write coreth go.mod")

	// Create firewood directory with go.mod in ffi subdir
	firewoodFFIDir := filepath.Join(tmpDir, "firewood", "ffi")
	err = os.MkdirAll(firewoodFFIDir, 0o755)
	require.NoError(t, err, "failed to create firewood ffi dir")

	firewoodGoModPath := filepath.Join(firewoodFFIDir, "go.mod")
	firewoodGoModContent := `module github.com/ava-labs/firewood/ffi

go 1.24
`
	err = os.WriteFile(firewoodGoModPath, []byte(firewoodGoModContent), 0o600)
	require.NoError(t, err, "failed to write firewood go.mod")

	// Call UpdateAllReplaceDirectives
	syncedRepos := []string{"coreth", "firewood"}
	err = UpdateAllReplaceDirectives(log, tmpDir, syncedRepos)
	require.NoError(t, err, "UpdateAllReplaceDirectives failed")

	// Verify avalanchego go.mod has replace directives for coreth and firewood
	avalanchegoMod, err := ReadGoMod(log, primaryGoModPath)
	require.NoError(t, err, "failed to read avalanchego go.mod")

	hasCorethReplace := false
	hasFirewoodReplace := false
	for _, r := range avalanchegoMod.Replace {
		if r.Old.Path == "github.com/ava-labs/coreth" {
			hasCorethReplace = true
			require.Equal(t, "./coreth", r.New.Path, "unexpected coreth replace path")
		}
		if r.Old.Path == "github.com/ava-labs/firewood/ffi" {
			hasFirewoodReplace = true
			require.Equal(t, "./firewood/ffi/result/ffi", r.New.Path, "unexpected firewood replace path")
		}
	}
	require.True(t, hasCorethReplace, "avalanchego should have replace for coreth")
	require.True(t, hasFirewoodReplace, "avalanchego should have replace for firewood")

	// Verify coreth go.mod has replace directives for avalanchego and firewood
	corethMod, err := ReadGoMod(log, corethGoModPath)
	require.NoError(t, err, "failed to read coreth go.mod")

	hasAvalanchegoReplace := false
	hasFirewoodReplaceInCoreth := false
	for _, r := range corethMod.Replace {
		if r.Old.Path == "github.com/ava-labs/avalanchego" {
			hasAvalanchegoReplace = true
			require.Equal(t, "..", r.New.Path, "unexpected avalanchego replace path from coreth")
		}
		if r.Old.Path == "github.com/ava-labs/firewood/ffi" {
			hasFirewoodReplaceInCoreth = true
			require.Equal(t, "../firewood/ffi/result/ffi", r.New.Path, "unexpected firewood replace path from coreth")
		}
	}
	require.True(t, hasAvalanchegoReplace, "coreth should have replace for avalanchego")
	require.True(t, hasFirewoodReplaceInCoreth, "coreth should have replace for firewood")
}

// TestUpdateAllReplaceDirectives_MultipleRepos tests all repo combinations
func TestUpdateAllReplaceDirectives_MultipleRepos(t *testing.T) {
	testCases := []struct {
		name          string
		primaryRepo   string
		syncedRepos   []string
		primaryModule string
		primaryDeps   []string
		corethDeps    []string
		verifyFunc    func(t *testing.T, tmpDir string)
	}{
		{
			name:          "sync from avalanchego",
			primaryRepo:   "avalanchego",
			syncedRepos:   []string{"coreth", "firewood"},
			primaryModule: "github.com/ava-labs/avalanchego",
			primaryDeps:   []string{"github.com/ava-labs/coreth", "github.com/ava-labs/firewood/ffi"},
			corethDeps:    []string{"github.com/ava-labs/avalanchego", "github.com/ava-labs/firewood/ffi"},
			verifyFunc: func(t *testing.T, tmpDir string) {
				log := logging.NoLog{}

				// Check avalanchego has replaces for coreth and firewood
				avalanchegoMod, err := ReadGoMod(log, filepath.Join(tmpDir, "go.mod"))
				require.NoError(t, err)

				replaces := make(map[string]string)
				for _, r := range avalanchegoMod.Replace {
					replaces[r.Old.Path] = r.New.Path
				}

				require.Contains(t, replaces, "github.com/ava-labs/coreth", "avalanchego should have coreth replace")
				require.Contains(t, replaces, "github.com/ava-labs/firewood/ffi", "avalanchego should have firewood replace")

				// Check coreth has replaces for avalanchego and firewood
				corethMod, err := ReadGoMod(log, filepath.Join(tmpDir, "coreth", "go.mod"))
				require.NoError(t, err)

				corethReplaces := make(map[string]string)
				for _, r := range corethMod.Replace {
					corethReplaces[r.Old.Path] = r.New.Path
				}

				require.Contains(t, corethReplaces, "github.com/ava-labs/avalanchego", "coreth should have avalanchego replace")
				require.Contains(t, corethReplaces, "github.com/ava-labs/firewood/ffi", "coreth should have firewood replace")
				require.Equal(t, "..", corethReplaces["github.com/ava-labs/avalanchego"], "coreth should point to parent dir for avalanchego")
			},
		},
		{
			name:          "sync firewood only from avalanchego",
			primaryRepo:   "avalanchego",
			syncedRepos:   []string{"firewood"},
			primaryModule: "github.com/ava-labs/avalanchego",
			primaryDeps:   []string{"github.com/ava-labs/firewood/ffi"},
			corethDeps:    nil, // coreth not synced
			verifyFunc: func(t *testing.T, tmpDir string) {
				log := logging.NoLog{}

				// Check avalanchego has replace for firewood
				avalanchegoMod, err := ReadGoMod(log, filepath.Join(tmpDir, "go.mod"))
				require.NoError(t, err)

				hasFirewood := false
				for _, r := range avalanchegoMod.Replace {
					if r.Old.Path == "github.com/ava-labs/firewood/ffi" {
						hasFirewood = true
					}
				}
				require.True(t, hasFirewood, "avalanchego should have firewood replace")
			},
		},
		{
			name:          "sync coreth only from avalanchego",
			primaryRepo:   "avalanchego",
			syncedRepos:   []string{"coreth"},
			primaryModule: "github.com/ava-labs/avalanchego",
			primaryDeps:   []string{"github.com/ava-labs/coreth"},
			corethDeps:    []string{"github.com/ava-labs/avalanchego"},
			verifyFunc: func(t *testing.T, tmpDir string) {
				log := logging.NoLog{}

				// Check avalanchego has replace for coreth
				avalanchegoMod, err := ReadGoMod(log, filepath.Join(tmpDir, "go.mod"))
				require.NoError(t, err)

				hasCoreth := false
				for _, r := range avalanchegoMod.Replace {
					if r.Old.Path == "github.com/ava-labs/coreth" {
						hasCoreth = true
					}
				}
				require.True(t, hasCoreth, "avalanchego should have coreth replace")

				// Check coreth has replace for avalanchego
				corethMod, err := ReadGoMod(log, filepath.Join(tmpDir, "coreth", "go.mod"))
				require.NoError(t, err)

				hasAvalanchego := false
				for _, r := range corethMod.Replace {
					if r.Old.Path == "github.com/ava-labs/avalanchego" {
						hasAvalanchego = true
						require.Equal(t, "..", r.New.Path)
					}
				}
				require.True(t, hasAvalanchego, "coreth should have avalanchego replace")
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			log := logging.NoLog{}
			tmpDir := t.TempDir()

			// Create primary repo go.mod
			primaryGoModPath := filepath.Join(tmpDir, "go.mod")
			primaryGoModContent := "module " + tc.primaryModule + "\n\ngo 1.24\n\nrequire (\n"
			for _, dep := range tc.primaryDeps {
				primaryGoModContent += "\t" + dep + " v0.0.1\n"
			}
			primaryGoModContent += ")\n"
			err := os.WriteFile(primaryGoModPath, []byte(primaryGoModContent), 0o600)
			require.NoError(t, err, "failed to write primary go.mod")

			// Create synced repos
			for _, repoName := range tc.syncedRepos {
				config, err := GetRepoConfig(repoName)
				require.NoError(t, err)

				var repoDir string
				var goModPath string
				if repoName == "firewood" {
					repoDir = filepath.Join(tmpDir, "firewood", "ffi")
					err = os.MkdirAll(repoDir, 0o755)
					require.NoError(t, err)
					goModPath = filepath.Join(repoDir, "go.mod")
				} else {
					repoDir = filepath.Join(tmpDir, repoName)
					err = os.MkdirAll(repoDir, 0o755)
					require.NoError(t, err)
					goModPath = filepath.Join(repoDir, "go.mod")
				}

				goModContent := "module " + config.GoModule + "\n\ngo 1.24\n"
				if repoName == "coreth" && tc.corethDeps != nil {
					goModContent += "\nrequire (\n"
					for _, dep := range tc.corethDeps {
						goModContent += "\t" + dep + " v0.0.1\n"
					}
					goModContent += ")\n"
				}

				err = os.WriteFile(goModPath, []byte(goModContent), 0o600)
				require.NoError(t, err, "failed to write %s go.mod", repoName)
			}

			// Call UpdateAllReplaceDirectives
			err = UpdateAllReplaceDirectives(log, tmpDir, tc.syncedRepos)
			require.NoError(t, err, "UpdateAllReplaceDirectives failed")

			// Run custom verification
			if tc.verifyFunc != nil {
				tc.verifyFunc(t, tmpDir)
			}
		})
	}
}

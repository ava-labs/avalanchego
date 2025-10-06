// Copyright (C) 2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

//go:build integration

package dep

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestCloneIntegration tests comprehensive clone scenarios
func TestCloneIntegration(t *testing.T) {
	// Check if git is available
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available, skipping integration test")
	}

	// Check if DEPCTL_KEEP_TEMP is set to retain test directories
	keepTemp := os.Getenv("DEPCTL_KEEP_TEMP") == "1"

	tests := []struct {
		name           string
		setupGoMod     string
		cloneOpts      CloneOptions
		wantVersion    string // expected git describe or tag
		checkVersion   bool   // whether to check exact version
		skipValidation bool   // skip version validation (for default branches)
		wantErr        bool   // expect an error
	}{
		{
			name: "clone avalanchego with no version (uses default)",
			setupGoMod: `module github.com/ava-labs/test

go 1.21

require (
	github.com/ava-labs/coreth v1.2.3
)
`,
			cloneOpts: CloneOptions{
				Target:  TargetAvalanchego,
				Shallow: true,
			},
			skipValidation: true, // master is a branch, not a tag
		},
		{
			name: "clone firewood with version in avalanchego",
			setupGoMod: `module github.com/ava-labs/avalanchego

go 1.21

require (
	github.com/ava-labs/coreth v0.13.8
)

require (
	github.com/ava-labs/firewood-go-ethhash/ffi v0.0.12 // indirect
)
`,
			cloneOpts: CloneOptions{
				Target:  TargetFirewood,
				Shallow: true,
			},
			wantVersion: "v0.0.12",
			checkVersion: true,
		},
		{
			name: "clone coreth with version in avalanchego",
			setupGoMod: `module github.com/ava-labs/avalanchego

go 1.21

require (
	github.com/ava-labs/coreth v0.13.8
)
`,
			cloneOpts: CloneOptions{
				Target:  TargetCoreth,
				Shallow: true,
			},
			wantVersion: "v0.13.8",
			checkVersion: true,
		},
		{
			name: "clone avalanchego with version in coreth",
			setupGoMod: `module github.com/ava-labs/coreth

go 1.21

require (
	github.com/ava-labs/avalanchego v1.11.11
)
`,
			cloneOpts: CloneOptions{
				Target:  TargetAvalanchego,
				Shallow: true,
			},
			wantVersion: "v1.11.11",
			checkVersion: true,
		},
		{
			name: "clone firewood with no version in coreth (uses default)",
			setupGoMod: `module github.com/ava-labs/coreth

go 1.21

require (
	github.com/ava-labs/avalanchego v1.11.11
)
`,
			cloneOpts: CloneOptions{
				Target:  TargetFirewood,
				Shallow: true,
			},
			skipValidation: true, // main is a branch, not a tag
		},
		{
			name: "clone with commit hash (shallow fallback)",
			setupGoMod: `module github.com/ava-labs/avalanchego

go 1.21
`,
			cloneOpts: CloneOptions{
				Target:  TargetAvalanchego,
				Version: "e4a8e93f7", // Commit hash from recent history
				Shallow: true,
			},
			checkVersion:   true,
			skipValidation: false,
			wantVersion:    "e4a8e93f7",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create temporary directory
			tmpDir := t.TempDir()
			if keepTemp {
				t.Logf("Test directory: %s", tmpDir)
			}

			// Write go.mod
			goModPath := filepath.Join(tmpDir, "go.mod")
			if err := os.WriteFile(goModPath, []byte(tt.setupGoMod), 0o644); err != nil {
				t.Fatalf("failed to write test go.mod: %v", err)
			}

			// Change to temp directory
			oldWd, err := os.Getwd()
			if err != nil {
				t.Fatalf("failed to get working directory: %v", err)
			}
			defer func() {
				if err := os.Chdir(oldWd); err != nil {
					t.Fatalf("failed to restore working directory: %v", err)
				}
				if keepTemp && !t.Failed() {
					// If keeping temp and test passed, log success
					t.Logf("Test passed, keeping directory: %s", tmpDir)
				}
			}()
			if err := os.Chdir(tmpDir); err != nil {
				t.Fatalf("failed to change to temp directory: %v", err)
			}

			// Set clone path
			if tt.cloneOpts.Path == "" {
				tt.cloneOpts.Path = string(tt.cloneOpts.Target)
			}

			// Perform clone
			result, err := Clone(tt.cloneOpts)

			// Check for expected errors
			if tt.wantErr {
				if err == nil {
					t.Fatalf("Clone() expected error but got none")
				}
				// Test passed - we got the expected error
				return
			}

			if err != nil {
				t.Fatalf("Clone() failed: %v", err)
			}

			// Verify result path matches expected
			if result.Path != tt.cloneOpts.Path {
				t.Errorf("Clone() returned path %s, want %s", result.Path, tt.cloneOpts.Path)
			}

			// Verify clone exists
			clonePath := filepath.Join(tmpDir, tt.cloneOpts.Path)
			if _, err := os.Stat(clonePath); os.IsNotExist(err) {
				t.Errorf("cloned directory does not exist: %s", clonePath)
			}

			// Verify .git directory exists
			gitDir := filepath.Join(clonePath, ".git")
			if _, err := os.Stat(gitDir); os.IsNotExist(err) {
				t.Errorf("git directory does not exist: %s", gitDir)
			}

			// Check version if requested
			if tt.checkVersion && tt.wantVersion != "" {
				// Change to clone directory
				if err := os.Chdir(clonePath); err != nil {
					t.Fatalf("failed to change to clone directory: %v", err)
				}

				// Get current commit
				cmd := exec.Command("git", "rev-parse", "HEAD")
				headOutput, err := cmd.Output()
				if err != nil {
					t.Fatalf("failed to get HEAD: %v", err)
				}
				headCommit := strings.TrimSpace(string(headOutput))

				// Get commit for expected tag (dereference if it's an annotated tag)
				cmd = exec.Command("git", "rev-parse", tt.wantVersion+"^{commit}")
				tagOutput, err := cmd.Output()
				if err != nil {
					// Try without ^{commit} in case it's a lightweight tag
					cmd = exec.Command("git", "rev-parse", tt.wantVersion)
					tagOutput, err = cmd.Output()
					if err != nil {
						t.Fatalf("failed to get tag %s: %v", tt.wantVersion, err)
					}
				}
				tagCommit := strings.TrimSpace(string(tagOutput))

				// Compare commits
				if headCommit != tagCommit {
					t.Errorf("HEAD commit %s does not match tag %s commit %s", headCommit, tt.wantVersion, tagCommit)
				}

				// Verify we're on the expected branch
				cmd = exec.Command("git", "branch", "--show-current")
				branchOutput, err := cmd.Output()
				if err != nil {
					t.Fatalf("failed to get current branch: %v", err)
				}
				currentBranch := strings.TrimSpace(string(branchOutput))
				expectedBranch := "local/" + tt.wantVersion
				if currentBranch != expectedBranch {
					t.Errorf("current branch %s does not match expected %s", currentBranch, expectedBranch)
				}
			}

			// If not skipping validation and no specific version check, at least verify we can get HEAD
			if !tt.skipValidation && !tt.checkVersion {
				if err := os.Chdir(clonePath); err != nil {
					t.Fatalf("failed to change to clone directory: %v", err)
				}

				cmd := exec.Command("git", "rev-parse", "HEAD")
				if _, err := cmd.Output(); err != nil {
					t.Errorf("failed to get HEAD in clone: %v", err)
				}
			}
		})
	}
}

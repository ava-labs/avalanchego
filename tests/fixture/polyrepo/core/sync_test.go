// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package core

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/utils/logging"
)

func TestDetectCurrentRepo(t *testing.T) {
	tests := []struct {
		name          string
		goModContent  string
		goModSubPath  string // if set, create go.mod in this subdir
		expectedRepo  string
		expectedError bool
	}{
		{
			name: "avalanchego repo",
			goModContent: `module github.com/ava-labs/avalanchego

go 1.21
`,
			expectedRepo:  "avalanchego",
			expectedError: false,
		},
		{
			name: "coreth repo",
			goModContent: `module github.com/ava-labs/coreth

go 1.21
`,
			expectedRepo:  "coreth",
			expectedError: false,
		},
		{
			name:         "firewood repo",
			goModSubPath: "ffi",
			goModContent: `module github.com/ava-labs/firewood/ffi

go 1.21
`,
			expectedRepo:  "firewood",
			expectedError: false,
		},
		{
			name: "unknown repo",
			goModContent: `module github.com/other/repo

go 1.21
`,
			expectedRepo:  "",
			expectedError: false,
		},
		{
			name:          "no go.mod",
			expectedRepo:  "",
			expectedError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()

			if tt.goModContent != "" {
				goModPath := filepath.Join(tmpDir, "go.mod")
				if tt.goModSubPath != "" {
					subDir := filepath.Join(tmpDir, tt.goModSubPath)
					err := os.MkdirAll(subDir, 0o755)
					require.NoError(t, err, "failed to create subdir")
					goModPath = filepath.Join(subDir, "go.mod")
				}

				err := os.WriteFile(goModPath, []byte(tt.goModContent), 0o600)
				require.NoError(t, err, "failed to write go.mod")
			}

			log := logging.NoLog{}
			repo, err := DetectCurrentRepo(log, tmpDir)

			if tt.expectedError {
				if err == nil {
					require.Fail(t, "expected error but got nil")
				}
				return
			}

			require.NoError(t, err)
			require.Equal(t, tt.expectedRepo, repo)
		})
	}
}

func TestGetReposToSync(t *testing.T) {
	tests := []struct {
		name          string
		currentRepo   string
		expectedRepos []string
	}{
		{
			name:          "from avalanchego",
			currentRepo:   "avalanchego",
			expectedRepos: []string{"coreth", "firewood"},
		},
		{
			name:          "from coreth",
			currentRepo:   "coreth",
			expectedRepos: []string{"avalanchego", "firewood"},
		},
		{
			name:          "from firewood",
			currentRepo:   "firewood",
			expectedRepos: []string{"avalanchego", "coreth"},
		},
		{
			name:          "from unknown/none",
			currentRepo:   "",
			expectedRepos: []string{"avalanchego", "coreth", "firewood"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repos := GetReposToSync(tt.currentRepo)

			require.Len(t, repos, len(tt.expectedRepos))

			for i, expectedRepo := range tt.expectedRepos {
				require.Equal(t, expectedRepo, repos[i])
			}
		})
	}
}

func TestGetDefaultRefForRepo(t *testing.T) {
	tests := []struct {
		name         string
		currentRepo  string
		targetRepo   string
		goModContent string
		expectedRef  string
		expectError  bool
	}{
		{
			name:        "avalanchego depends on coreth",
			currentRepo: "avalanchego",
			targetRepo:  "coreth",
			goModContent: `module github.com/ava-labs/avalanchego

go 1.21

require github.com/ava-labs/coreth v0.13.8
`,
			expectedRef: "v0.13.8",
			expectError: false,
		},
		{
			name:        "avalanchego depends on firewood with pseudo-version",
			currentRepo: "avalanchego",
			targetRepo:  "firewood",
			goModContent: `module github.com/ava-labs/avalanchego

go 1.21

require github.com/ava-labs/firewood-go-ethhash v0.0.0-20240101120000-abc123def456
`,
			expectedRef: "abc123def456",
			expectError: false,
		},
		{
			name:        "coreth depends on avalanchego",
			currentRepo: "coreth",
			targetRepo:  "avalanchego",
			goModContent: `module github.com/ava-labs/coreth

go 1.21

require github.com/ava-labs/avalanchego v1.11.11
`,
			expectedRef: "v1.11.11",
			expectError: false,
		},
		{
			name:        "avalanchego without firewood dependency - should error",
			currentRepo: "avalanchego",
			targetRepo:  "firewood",
			goModContent: `module github.com/ava-labs/avalanchego

go 1.21

require github.com/ava-labs/coreth v0.13.8
`,
			expectedRef: "",
			expectError: true,
		},
		{
			name:        "coreth without firewood dependency - should error",
			currentRepo: "coreth",
			targetRepo:  "firewood",
			goModContent: `module github.com/ava-labs/coreth

go 1.21

require github.com/ava-labs/avalanchego v1.11.11
`,
			expectedRef: "",
			expectError: true,
		},
		{
			name:         "no current repo - uses default branch",
			currentRepo:  "",
			targetRepo:   "firewood",
			goModContent: "",
			expectedRef:  "main",
			expectError:  false,
		},
		{
			name:         "no go.mod path - uses default branch",
			currentRepo:  "avalanchego",
			targetRepo:   "coreth",
			goModContent: "",
			expectedRef:  "master",
			expectError:  false,
		},
		{
			name:         "unknown target repo",
			currentRepo:  "avalanchego",
			targetRepo:   "unknown",
			goModContent: "",
			expectedRef:  "",
			expectError:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			goModPath := ""

			if tt.goModContent != "" {
				goModPath = filepath.Join(tmpDir, "go.mod")
				err := os.WriteFile(goModPath, []byte(tt.goModContent), 0o600)
				require.NoError(t, err, "failed to write go.mod")
			}

			log := logging.NoLog{}
			ref, err := GetDefaultRefForRepo(log, tt.currentRepo, tt.targetRepo, goModPath)

			if tt.expectError {
				if err == nil {
					require.Fail(t, "expected error")
				}
				return
			}

			require.NoError(t, err)
			require.Equal(t, tt.expectedRef, ref)
		})
	}
}

// TestSync_PrimaryMode_NoArgs tests that from a primary repo (e.g., avalanchego),
// calling Sync() with no args syncs other repos using versions from go.mod
func TestSync_PrimaryMode_NoArgs(t *testing.T) {
	// This is a unit test - not integration
	// It should verify the function orchestrates correctly but not do actual git operations
	t.Skip("TODO: Implement after Sync() function exists")
}

// TestSync_PrimaryMode_ExplicitRefs tests that explicit refs are used when provided
func TestSync_PrimaryMode_ExplicitRefs(t *testing.T) {
	t.Skip("TODO: Implement after Sync() function exists")
}

// TestSync_PrimaryMode_PartialRefs tests mixed explicit and discovered refs
func TestSync_PrimaryMode_PartialRefs(t *testing.T) {
	t.Skip("TODO: Implement after Sync() function exists")
}

// TestSync_CannotSyncIntoItself_Error tests validation that prevents syncing a repo into itself
func TestSync_CannotSyncIntoItself_Error(t *testing.T) {
	t.Skip("TODO: Implement after Sync() function exists")
}

// TestSync_StandaloneMode_AvalanchegoCoreth tests version discovery in standalone mode
// This is THE KEY TEST that would have caught the bug!
func TestSync_StandaloneMode_AvalanchegoCoreth(t *testing.T) {
	t.Skip("TODO: Implement after Sync() function exists")
}

// TestSync_StandaloneMode_NoArgs_Error tests that standalone mode requires explicit repos
func TestSync_StandaloneMode_NoArgs_Error(t *testing.T) {
	tmpDir := t.TempDir()
	log := logging.NoLog{}

	// No go.mod exists - standalone mode
	// No repos specified - should error
	err := Sync(log, tmpDir, []string{}, 1, false)

	require.ErrorIs(t, err, errStandaloneModeNeedsRepos)
}

// TestSync_StandaloneMode_ExplicitRefs tests standalone mode with all explicit refs
func TestSync_StandaloneMode_ExplicitRefs(t *testing.T) {
	t.Skip("TODO: Implement after Sync() function exists")
}

// TestSync_StandaloneMode_FirewoodOnly tests syncing only firewood in standalone mode
func TestSync_StandaloneMode_FirewoodOnly(t *testing.T) {
	t.Skip("TODO: Implement after Sync() function exists")
}

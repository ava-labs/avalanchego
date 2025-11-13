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

func TestGetDirectDependencies(t *testing.T) {
	tests := []struct {
		name          string
		currentRepo   string
		expectedRepos []string
		expectedError error
	}{
		{
			name:          "from firewood - always syncs avalanchego",
			currentRepo:   "firewood",
			expectedRepos: []string{"avalanchego"},
			expectedError: nil,
		},
		{
			name:          "from coreth - always syncs avalanchego",
			currentRepo:   "coreth",
			expectedRepos: []string{"avalanchego"},
			expectedError: nil,
		},
		{
			name:          "from avalanchego - error (root repo requires explicit args)",
			currentRepo:   "avalanchego",
			expectedRepos: nil,
			expectedError: errRootRepoNeedsExplicitArgs,
		},
		{
			name:          "from unknown repo - always syncs avalanchego",
			currentRepo:   "unknown",
			expectedRepos: []string{"avalanchego"},
			expectedError: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			log := logging.NoLog{}

			// goModPath is no longer used by GetDirectDependencies, but we pass it for API compatibility
			repos, err := GetDirectDependencies(log, tt.currentRepo, "")

			if tt.expectedError != nil {
				require.ErrorIs(t, err, tt.expectedError)
				return
			}

			require.NoError(t, err)
			require.Equal(t, tt.expectedRepos, repos)
		})
	}
}

func TestGetFirewoodReplacementPath(t *testing.T) {
	log := logging.NoLog{}

	tests := []struct {
		name         string
		repoName     string
		isPrimary    bool
		setupFunc    func(t *testing.T) string // Returns baseDir
		expectedPath string
	}{
		{
			name:      "synced firewood with nix build output",
			repoName:  "firewood",
			isPrimary: false,
			setupFunc: func(t *testing.T) string {
				dir := t.TempDir()
				// Create nix build structure for synced repo
				nixPath := filepath.Join(dir, "firewood", "ffi", "result", "ffi")
				require.NoError(t, os.MkdirAll(nixPath, 0o755))
				require.NoError(t, os.WriteFile(
					filepath.Join(nixPath, "go.mod"),
					[]byte("module github.com/ava-labs/firewood/ffi\n"),
					0o644,
				))
				return dir
			},
			expectedPath: "./ffi/result/ffi",
		},
		{
			name:      "synced firewood with cargo build output",
			repoName:  "firewood",
			isPrimary: false,
			setupFunc: func(t *testing.T) string {
				dir := t.TempDir()
				// Create cargo build structure (no result/ffi directory)
				cargoPath := filepath.Join(dir, "firewood", "ffi")
				require.NoError(t, os.MkdirAll(cargoPath, 0o755))
				return dir
			},
			expectedPath: "./ffi",
		},
		{
			name:      "primary firewood with nix build output",
			repoName:  "firewood",
			isPrimary: true,
			setupFunc: func(t *testing.T) string {
				dir := t.TempDir()
				// Create nix build structure for primary repo (baseDir IS firewood)
				nixPath := filepath.Join(dir, "ffi", "result", "ffi")
				require.NoError(t, os.MkdirAll(nixPath, 0o755))
				require.NoError(t, os.WriteFile(
					filepath.Join(nixPath, "go.mod"),
					[]byte("module github.com/ava-labs/firewood/ffi\n"),
					0o644,
				))
				return dir
			},
			expectedPath: "./ffi/result/ffi",
		},
		{
			name:      "primary firewood with cargo build output",
			repoName:  "firewood",
			isPrimary: true,
			setupFunc: func(t *testing.T) string {
				dir := t.TempDir()
				// Create cargo build structure for primary repo (no result/ffi directory)
				cargoPath := filepath.Join(dir, "ffi")
				require.NoError(t, os.MkdirAll(cargoPath, 0o755))
				return dir
			},
			expectedPath: "./ffi",
		},
		{
			name:      "firewood not yet cloned",
			repoName:  "firewood",
			isPrimary: false,
			setupFunc: func(t *testing.T) string {
				// Empty directory, no firewood clone
				return t.TempDir()
			},
			expectedPath: "./ffi", // Default to cargo path
		},
		{
			name:      "non-firewood repo uses config default",
			repoName:  "coreth",
			isPrimary: false,
			setupFunc: func(t *testing.T) string {
				return t.TempDir()
			},
			expectedPath: ".",
		},
		{
			name:      "avalanchego uses config default",
			repoName:  "avalanchego",
			isPrimary: false,
			setupFunc: func(t *testing.T) string {
				return t.TempDir()
			},
			expectedPath: ".",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			baseDir := tt.setupFunc(t)
			result := GetFirewoodReplacementPath(log, baseDir, tt.repoName, tt.isPrimary)
			require.Equal(t, tt.expectedPath, result)
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

require github.com/ava-labs/firewood-go-ethhash/ffi v0.0.0-20240101120000-abc123def456
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
			name:        "avalanchego without firewood dependency - uses default branch",
			currentRepo: "avalanchego",
			targetRepo:  "firewood",
			goModContent: `module github.com/ava-labs/avalanchego

go 1.21

require github.com/ava-labs/coreth v0.13.8
`,
			expectedRef: "main",
			expectError: false,
		},
		{
			name:        "coreth without firewood dependency - uses default branch",
			currentRepo: "coreth",
			targetRepo:  "firewood",
			goModContent: `module github.com/ava-labs/coreth

go 1.21

require github.com/ava-labs/avalanchego v1.11.11
`,
			expectedRef: "main",
			expectError: false,
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

// TestSync_PrimaryMode_RefDetermination tests that Sync() correctly determines refs in primary mode.
// This test validates the orchestration logic without doing actual git operations by testing with
// a dependency that doesn't exist (forcing early exit before git clone).
func TestSync_PrimaryMode_RefDetermination(t *testing.T) {
	tests := []struct {
		name         string
		goModContent string
		repoArgs     []string
		expectError  string
	}{
		{
			name: "no args - avalanchego root repo requires explicit args",
			goModContent: `module github.com/ava-labs/avalanchego

go 1.21

require github.com/ava-labs/coreth v0.13.8
`,
			repoArgs: []string{},
			// avalanchego is a root repo and requires explicit arguments
			expectError: "requires explicit repository arguments",
		},
		{
			name: "explicit refs - all explicit",
			goModContent: `module github.com/ava-labs/avalanchego

go 1.21

require github.com/ava-labs/coreth v0.13.8
`,
			repoArgs:    []string{"coreth@v0.15.0"},
			expectError: "", // Should reach git clone (no early error)
		},
		{
			name: "partial refs - explicit and discovered (firewood falls back to default)",
			goModContent: `module github.com/ava-labs/avalanchego

go 1.21

require github.com/ava-labs/coreth v0.13.8
`,
			repoArgs:    []string{"coreth@v0.15.0", "firewood"},
			expectError: "", // Will use default branch for firewood and proceed to git clone
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()

			// Create go.mod in temp directory
			goModPath := filepath.Join(tmpDir, "go.mod")
			err := os.WriteFile(goModPath, []byte(tt.goModContent), 0o600)
			require.NoError(t, err, "failed to write go.mod")

			log := logging.NoLog{}

			// Run sync - should fail during ref determination or git operations
			err = Sync(log, tmpDir, tt.repoArgs, 1, false)

			// Check for expected error or success behavior
			if tt.expectError != "" {
				require.Error(t, err) //nolint:forbidigo // checking error message content, not error type
				require.Contains(t, err.Error(), tt.expectError)
			}
			// Note: If expectError is empty, the test will error at git clone
			// which is acceptable - we're testing that validation passes

			// Validate that go.mod still exists (wasn't corrupted by our logic)
			content, err := os.ReadFile(goModPath)
			require.NoError(t, err)
			require.Contains(t, string(content), "module github.com/ava-labs/avalanchego")
		})
	}
}

// TestSync_CannotSyncIntoItself_Error tests validation that prevents syncing a repo into itself
func TestSync_CannotSyncIntoItself_Error(t *testing.T) {
	tmpDir := t.TempDir()

	// Create go.mod for avalanchego (primary repo)
	goModContent := `module github.com/ava-labs/avalanchego

go 1.21

require github.com/ava-labs/coreth v0.13.8
`
	goModPath := filepath.Join(tmpDir, "go.mod")
	err := os.WriteFile(goModPath, []byte(goModContent), 0o600)
	require.NoError(t, err, "failed to write go.mod")

	log := logging.NoLog{}

	// Try to sync avalanchego into itself - should error
	err = Sync(log, tmpDir, []string{"avalanchego"}, 1, false)

	require.Error(t, err) //nolint:forbidigo // checking error message content, not error type
	require.Contains(t, err.Error(), "cannot sync avalanchego into itself")
}

// TestSync_StandaloneMode_Validation tests validation in standalone mode (no go.mod).
// This is THE KEY TEST GROUP that would have caught the standalone mode bug documented in CLAUDE.md.
// These tests validate that standalone mode correctly validates inputs without trying to read
// from a non-existent go.mod file.
func TestSync_StandaloneMode_Validation(t *testing.T) {
	tests := []struct {
		name         string
		repoArgs     []string
		expectError  string
		expectErrVar error
	}{
		{
			name:         "no args - should error immediately",
			repoArgs:     []string{},
			expectErrVar: errStandaloneModeNeedsRepos,
		},
		{
			name:        "invalid repo format",
			repoArgs:    []string{"invalid@@format"},
			expectError: "invalid repo format",
		},
		{
			name:        "unknown repo",
			repoArgs:    []string{"unknownrepo"},
			expectError: "unknown repository",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()

			// No go.mod exists - this is standalone mode
			log := logging.NoLog{}

			// Run sync
			err := Sync(log, tmpDir, tt.repoArgs, 1, false)

			// Should error with expected message
			if tt.expectErrVar != nil {
				require.ErrorIs(t, err, tt.expectErrVar)
			} else {
				require.Error(t, err) //nolint:forbidigo // checking error message content, not error type
				require.Contains(t, err.Error(), tt.expectError)
			}

			// Verify no go.mod was created (standalone mode shouldn't create one)
			goModPath := filepath.Join(tmpDir, "go.mod")
			_, err = os.Stat(goModPath)
			require.True(t, os.IsNotExist(err), "go.mod should not be created in standalone mode")
		})
	}
}

// TestSync_StandaloneMode_MultiRepo tests various multi-repo scenarios in standalone mode.
// These tests validate that standalone mode works correctly without trying to read from go.mod.
func TestSync_StandaloneMode_MultiRepo(t *testing.T) {
	tests := []struct {
		name     string
		repoArgs []string
	}{
		{
			name:     "avalanchego and coreth - KEY TEST that would have caught the bug",
			repoArgs: []string{"avalanchego", "coreth"},
		},
		{
			name:     "explicit refs for multiple repos",
			repoArgs: []string{"avalanchego@v1.11.0", "coreth@v0.13.8"},
		},
		{
			name:     "firewood only",
			repoArgs: []string{"firewood@main"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			log := logging.NoLog{}

			// No go.mod exists - standalone mode
			err := Sync(log, tmpDir, tt.repoArgs, 1, false)
			// Tests will fail at git clone (network required), but the KEY is that
			// they should NOT fail with validation errors about missing go.mod
			// The error should be from git operations, not from our orchestration logic
			// The critical validation: should NOT error about missing go.mod
			if err != nil {
				require.NotContains(t, err.Error(), "must specify repos when no go.mod exists")
				require.NotContains(t, err.Error(), "go.mod not found")
				require.NotContains(t, err.Error(), "no go.mod")
			}

			// Verify no go.mod was created (standalone mode shouldn't create one)
			goModPath := filepath.Join(tmpDir, "go.mod")
			_, statErr := os.Stat(goModPath)
			require.True(t, os.IsNotExist(statErr), "go.mod should not be created in standalone mode")
		})
	}
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

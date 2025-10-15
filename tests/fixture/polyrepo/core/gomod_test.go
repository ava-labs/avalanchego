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

func TestReadGoMod(t *testing.T) {
	// Create a temporary go.mod file
	tmpDir := t.TempDir()
	goModPath := filepath.Join(tmpDir, "go.mod")

	goModContent := `module github.com/test/repo

go 1.21

require (
	github.com/ava-labs/avalanchego v1.11.11
	github.com/ava-labs/coreth v0.13.8
)
`
	err := os.WriteFile(goModPath, []byte(goModContent), 0o600)
	require.NoError(t, err, "failed to write test go.mod")

	// Test reading the go.mod
	log := logging.NoLog{}
	modFile, err := ReadGoMod(log, goModPath)
	require.NoError(t, err, "ReadGoMod failed")

	require.NotNil(t, modFile.Module, "module should not be nil")
	require.Equal(t, "github.com/test/repo", modFile.Module.Mod.Path)

	// Check that we can find dependencies
	foundAvalanchego := false
	foundCoreth := false
	for _, req := range modFile.Require {
		if req.Mod.Path == "github.com/ava-labs/avalanchego" {
			foundAvalanchego = true
			require.Equal(t, "v1.11.11", req.Mod.Version, "avalanchego version")
		}
		if req.Mod.Path == "github.com/ava-labs/coreth" {
			foundCoreth = true
			require.Equal(t, "v0.13.8", req.Mod.Version, "coreth version")
		}
	}

	require.True(t, foundAvalanchego, "did not find avalanchego requirement")
	require.True(t, foundCoreth, "did not find coreth requirement")
}

func TestGetModulePath(t *testing.T) {
	// Create a temporary go.mod file
	tmpDir := t.TempDir()
	goModPath := filepath.Join(tmpDir, "go.mod")

	goModContent := `module github.com/test/myrepo

go 1.21
`
	err := os.WriteFile(goModPath, []byte(goModContent), 0o600)
	require.NoError(t, err, "failed to write test go.mod")

	// Test getting the module path
	log := logging.NoLog{}
	modulePath, err := GetModulePath(log, goModPath)
	require.NoError(t, err, "GetModulePath failed")

	require.Equal(t, "github.com/test/myrepo", modulePath)
}

func TestAddReplaceDirective(t *testing.T) {
	// Create a temporary go.mod file
	tmpDir := t.TempDir()
	goModPath := filepath.Join(tmpDir, "go.mod")

	goModContent := `module github.com/test/repo

go 1.21

require (
	github.com/ava-labs/avalanchego v1.11.11
)
`
	err := os.WriteFile(goModPath, []byte(goModContent), 0o600)
	require.NoError(t, err, "failed to write test go.mod")

	// Add a replace directive
	log := logging.NoLog{}
	err = AddReplaceDirective(log, goModPath, "github.com/ava-labs/avalanchego", "./avalanchego")
	require.NoError(t, err, "AddReplaceDirective failed")

	// Read back and verify
	modFile, err := ReadGoMod(log, goModPath)
	require.NoError(t, err, "ReadGoMod failed")

	found := false
	for _, replace := range modFile.Replace {
		if replace.Old.Path == "github.com/ava-labs/avalanchego" {
			found = true
			require.Equal(t, "./avalanchego", replace.New.Path, "replacement path")
		}
	}

	require.True(t, found, "replace directive not found in go.mod")
}

func TestRemoveReplaceDirective(t *testing.T) {
	// Create a temporary go.mod file with a replace directive
	tmpDir := t.TempDir()
	goModPath := filepath.Join(tmpDir, "go.mod")

	goModContent := `module github.com/test/repo

go 1.21

require (
	github.com/ava-labs/avalanchego v1.11.11
)

replace github.com/ava-labs/avalanchego => ./avalanchego
`
	err := os.WriteFile(goModPath, []byte(goModContent), 0o600)
	require.NoError(t, err, "failed to write test go.mod")

	// Remove the replace directive
	log := logging.NoLog{}
	err = RemoveReplaceDirective(log, goModPath, "github.com/ava-labs/avalanchego")
	require.NoError(t, err, "RemoveReplaceDirective failed")

	// Read back and verify it's gone
	modFile, err := ReadGoMod(log, goModPath)
	require.NoError(t, err, "ReadGoMod failed")

	for _, replace := range modFile.Replace {
		require.NotEqual(t, "github.com/ava-labs/avalanchego", replace.Old.Path, "replace directive should have been removed")
	}
}

func TestGetDependencyVersion(t *testing.T) {
	// Create a temporary go.mod file
	tmpDir := t.TempDir()
	goModPath := filepath.Join(tmpDir, "go.mod")

	goModContent := `module github.com/test/repo

go 1.21

require (
	github.com/ava-labs/avalanchego v1.11.11
	github.com/ava-labs/coreth v0.13.8
)
`
	err := os.WriteFile(goModPath, []byte(goModContent), 0o600)
	require.NoError(t, err, "failed to write test go.mod")

	// Test getting dependency version
	log := logging.NoLog{}
	version, err := GetDependencyVersion(log, goModPath, "github.com/ava-labs/avalanchego")
	require.NoError(t, err, "GetDependencyVersion failed")

	require.Equal(t, "v1.11.11", version)

	// Test getting version for non-existent dependency
	_, err = GetDependencyVersion(log, goModPath, "github.com/nonexistent/repo")
	if err == nil {
		require.Fail(t, "expected error for non-existent dependency")
	}
}

func TestAddReplaceDirective_PreservesComments(t *testing.T) {
	// Create a temporary go.mod file with comments
	tmpDir := t.TempDir()
	goModPath := filepath.Join(tmpDir, "go.mod")

	goModContent := `// This is a test module
module github.com/test/repo

go 1.21

// Production dependencies
require (
	github.com/ava-labs/avalanchego v1.11.11 // Main dependency
	github.com/ava-labs/coreth v0.13.8
)

// Development tools
require github.com/stretchr/testify v1.8.0
`
	err := os.WriteFile(goModPath, []byte(goModContent), 0o600)
	require.NoError(t, err, "failed to write test go.mod")

	// Add a replace directive
	log := logging.NoLog{}
	err = AddReplaceDirective(log, goModPath, "github.com/ava-labs/avalanchego", "./avalanchego")
	require.NoError(t, err, "AddReplaceDirective failed")

	// Read back and verify comments are preserved
	content, err := os.ReadFile(goModPath)
	require.NoError(t, err, "failed to read go.mod")

	contentStr := string(content)

	// Check that comments are preserved
	require.Contains(t, contentStr, "// This is a test module", "module comment was not preserved")
	require.Contains(t, contentStr, "// Production dependencies", "require block comment was not preserved")
	require.Contains(t, contentStr, "// Main dependency", "inline comment was not preserved")
	require.Contains(t, contentStr, "// Development tools", "development tools comment was not preserved")

	// Verify the replace directive was added
	require.Contains(t, contentStr, "replace github.com/ava-labs/avalanchego => ./avalanchego", "replace directive was not added correctly")
}

func TestAddReplaceDirective_UpdatesExisting(t *testing.T) {
	// Create a temporary go.mod file with an existing replace
	tmpDir := t.TempDir()
	goModPath := filepath.Join(tmpDir, "go.mod")

	goModContent := `module github.com/test/repo

go 1.21

require github.com/ava-labs/avalanchego v1.11.11

replace github.com/ava-labs/avalanchego => ./old-path
`
	err := os.WriteFile(goModPath, []byte(goModContent), 0o600)
	require.NoError(t, err, "failed to write test go.mod")

	// Add/update the replace directive
	log := logging.NoLog{}
	err = AddReplaceDirective(log, goModPath, "github.com/ava-labs/avalanchego", "./new-path")
	require.NoError(t, err, "AddReplaceDirective failed")

	// Read back and verify
	modFile, err := ReadGoMod(log, goModPath)
	require.NoError(t, err, "ReadGoMod failed")

	// Should have exactly one replace directive
	require.Len(t, modFile.Replace, 1, "expected 1 replace directive")

	// Check that it points to the new path
	require.Equal(t, "./new-path", modFile.Replace[0].New.Path, "replace should point to new path")
}

func TestConvertVersionToGitRef(t *testing.T) {
	tests := []struct {
		name        string
		version     string
		expectedRef string
		expectError bool
	}{
		{
			name:        "pseudo-version with full timestamp",
			version:     "v1.13.6-0.20251007213349-63cc1a166a56",
			expectedRef: "63cc1a166a56",
			expectError: false,
		},
		{
			name:        "another pseudo-version",
			version:     "v0.15.4-rc.3.0.20251002221438-a857a64c28ea",
			expectedRef: "a857a64c28ea",
			expectError: false,
		},
		{
			name:        "semantic version tag",
			version:     "v0.13.8",
			expectedRef: "v0.13.8",
			expectError: false,
		},
		{
			name:        "semantic version with rc",
			version:     "v1.11.11-rc.1",
			expectedRef: "v1.11.11-rc.1",
			expectError: false,
		},
		{
			name:        "branch name",
			version:     "main",
			expectedRef: "main",
			expectError: false,
		},
		{
			name:        "commit hash directly",
			version:     "63cc1a166a56",
			expectedRef: "63cc1a166a56",
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			log := logging.NoLog{}
			gitRef, err := ConvertVersionToGitRef(log, tt.version)

			if tt.expectError {
				if err == nil {
					require.Fail(t, "expected error but got nil")
				}
				return
			}

			require.NoError(t, err, "unexpected error")
			require.Equal(t, tt.expectedRef, gitRef, "git ref mismatch")
		})
	}
}

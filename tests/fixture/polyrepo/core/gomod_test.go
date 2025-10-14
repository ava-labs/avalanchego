// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package core

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
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
	err := os.WriteFile(goModPath, []byte(goModContent), 0644)
	if err != nil {
		t.Fatalf("failed to write test go.mod: %v", err)
	}

	// Test reading the go.mod
	modFile, err := ReadGoMod(goModPath)
	if err != nil {
		t.Fatalf("ReadGoMod failed: %v", err)
	}

	if modFile.Module == nil || modFile.Module.Mod.Path != "github.com/test/repo" {
		t.Errorf("expected module 'github.com/test/repo', got '%v'", modFile.Module)
	}

	// Check that we can find dependencies
	foundAvalanchego := false
	foundCoreth := false
	for _, req := range modFile.Require {
		if req.Mod.Path == "github.com/ava-labs/avalanchego" {
			foundAvalanchego = true
			if req.Mod.Version != "v1.11.11" {
				t.Errorf("expected avalanchego version 'v1.11.11', got '%s'", req.Mod.Version)
			}
		}
		if req.Mod.Path == "github.com/ava-labs/coreth" {
			foundCoreth = true
			if req.Mod.Version != "v0.13.8" {
				t.Errorf("expected coreth version 'v0.13.8', got '%s'", req.Mod.Version)
			}
		}
	}

	if !foundAvalanchego {
		t.Error("did not find avalanchego requirement")
	}
	if !foundCoreth {
		t.Error("did not find coreth requirement")
	}
}

func TestGetModulePath(t *testing.T) {
	// Create a temporary go.mod file
	tmpDir := t.TempDir()
	goModPath := filepath.Join(tmpDir, "go.mod")

	goModContent := `module github.com/test/myrepo

go 1.21
`
	err := os.WriteFile(goModPath, []byte(goModContent), 0644)
	if err != nil {
		t.Fatalf("failed to write test go.mod: %v", err)
	}

	// Test getting the module path
	modulePath, err := GetModulePath(goModPath)
	if err != nil {
		t.Fatalf("GetModulePath failed: %v", err)
	}

	if modulePath != "github.com/test/myrepo" {
		t.Errorf("expected module path 'github.com/test/myrepo', got '%s'", modulePath)
	}
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
	err := os.WriteFile(goModPath, []byte(goModContent), 0644)
	if err != nil {
		t.Fatalf("failed to write test go.mod: %v", err)
	}

	// Add a replace directive
	err = AddReplaceDirective(goModPath, "github.com/ava-labs/avalanchego", "./avalanchego")
	if err != nil {
		t.Fatalf("AddReplaceDirective failed: %v", err)
	}

	// Read back and verify
	modFile, err := ReadGoMod(goModPath)
	if err != nil {
		t.Fatalf("ReadGoMod failed: %v", err)
	}

	found := false
	for _, replace := range modFile.Replace {
		if replace.Old.Path == "github.com/ava-labs/avalanchego" {
			found = true
			if replace.New.Path != "./avalanchego" {
				t.Errorf("expected replacement path './avalanchego', got '%s'", replace.New.Path)
			}
		}
	}

	if !found {
		t.Error("replace directive not found in go.mod")
	}
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
	err := os.WriteFile(goModPath, []byte(goModContent), 0644)
	if err != nil {
		t.Fatalf("failed to write test go.mod: %v", err)
	}

	// Remove the replace directive
	err = RemoveReplaceDirective(goModPath, "github.com/ava-labs/avalanchego")
	if err != nil {
		t.Fatalf("RemoveReplaceDirective failed: %v", err)
	}

	// Read back and verify it's gone
	modFile, err := ReadGoMod(goModPath)
	if err != nil {
		t.Fatalf("ReadGoMod failed: %v", err)
	}

	for _, replace := range modFile.Replace {
		if replace.Old.Path == "github.com/ava-labs/avalanchego" {
			t.Error("replace directive should have been removed")
		}
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
	err := os.WriteFile(goModPath, []byte(goModContent), 0644)
	if err != nil {
		t.Fatalf("failed to write test go.mod: %v", err)
	}

	// Test getting dependency version
	version, err := GetDependencyVersion(goModPath, "github.com/ava-labs/avalanchego")
	if err != nil {
		t.Fatalf("GetDependencyVersion failed: %v", err)
	}

	if version != "v1.11.11" {
		t.Errorf("expected version 'v1.11.11', got '%s'", version)
	}

	// Test getting version for non-existent dependency
	_, err = GetDependencyVersion(goModPath, "github.com/nonexistent/repo")
	if err == nil {
		t.Error("expected error for non-existent dependency")
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
	err := os.WriteFile(goModPath, []byte(goModContent), 0644)
	if err != nil {
		t.Fatalf("failed to write test go.mod: %v", err)
	}

	// Add a replace directive
	err = AddReplaceDirective(goModPath, "github.com/ava-labs/avalanchego", "./avalanchego")
	if err != nil {
		t.Fatalf("AddReplaceDirective failed: %v", err)
	}

	// Read back and verify comments are preserved
	content, err := os.ReadFile(goModPath)
	if err != nil {
		t.Fatalf("failed to read go.mod: %v", err)
	}

	contentStr := string(content)

	// Check that comments are preserved
	if !strings.Contains(contentStr, "// This is a test module") {
		t.Error("module comment was not preserved")
	}
	if !strings.Contains(contentStr, "// Production dependencies") {
		t.Error("require block comment was not preserved")
	}
	if !strings.Contains(contentStr, "// Main dependency") {
		t.Error("inline comment was not preserved")
	}
	if !strings.Contains(contentStr, "// Development tools") {
		t.Error("development tools comment was not preserved")
	}

	// Verify the replace directive was added
	if !strings.Contains(contentStr, "replace github.com/ava-labs/avalanchego => ./avalanchego") {
		t.Error("replace directive was not added correctly")
	}
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
	err := os.WriteFile(goModPath, []byte(goModContent), 0644)
	if err != nil {
		t.Fatalf("failed to write test go.mod: %v", err)
	}

	// Add/update the replace directive
	err = AddReplaceDirective(goModPath, "github.com/ava-labs/avalanchego", "./new-path")
	if err != nil {
		t.Fatalf("AddReplaceDirective failed: %v", err)
	}

	// Read back and verify
	modFile, err := ReadGoMod(goModPath)
	if err != nil {
		t.Fatalf("ReadGoMod failed: %v", err)
	}

	// Should have exactly one replace directive
	if len(modFile.Replace) != 1 {
		t.Fatalf("expected 1 replace directive, got %d", len(modFile.Replace))
	}

	// Check that it points to the new path
	if modFile.Replace[0].New.Path != "./new-path" {
		t.Errorf("expected replace to point to './new-path', got '%s'", modFile.Replace[0].New.Path)
	}
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
			gitRef, err := ConvertVersionToGitRef(tt.version)

			if tt.expectError {
				if err == nil {
					t.Error("expected error but got nil")
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if gitRef != tt.expectedRef {
				t.Errorf("expected ref '%s', got '%s'", tt.expectedRef, gitRef)
			}
		})
	}
}

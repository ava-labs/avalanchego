// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package core

import (
	"os"
	"path/filepath"
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

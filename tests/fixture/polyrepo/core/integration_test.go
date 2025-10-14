// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

//go:build integration
// +build integration

package core

import (
	"os"
	"path/filepath"
	"testing"
)

// TestCloneRepo_ActualClone tests cloning a real repository
func TestCloneRepo_ActualClone(t *testing.T) {
	tmpDir := t.TempDir()
	clonePath := filepath.Join(tmpDir, "firewood")

	// Get firewood config
	config, err := GetRepoConfig("firewood")
	if err != nil {
		t.Fatalf("failed to get config: %v", err)
	}

	// Clone with default branch and shallow depth
	err = CloneRepo(config.GitRepo, clonePath, config.DefaultBranch, 1)
	if err != nil {
		t.Fatalf("failed to clone: %v", err)
	}

	// Verify the repo was cloned
	if _, err := os.Stat(filepath.Join(clonePath, ".git")); os.IsNotExist(err) {
		t.Error("expected .git directory to exist")
	}

	// Verify we can get the current ref
	currentRef, err := GetCurrentRef(clonePath)
	if err != nil {
		t.Fatalf("failed to get current ref: %v", err)
	}

	if currentRef != config.DefaultBranch {
		t.Errorf("expected branch %s, got %s", config.DefaultBranch, currentRef)
	}
}

// TestCloneOrUpdateRepo_WithSHA tests cloning with a specific commit SHA
func TestCloneOrUpdateRepo_WithSHA(t *testing.T) {
	tmpDir := t.TempDir()
	clonePath := filepath.Join(tmpDir, "firewood")

	// Get firewood config
	config, err := GetRepoConfig("firewood")
	if err != nil {
		t.Fatalf("failed to get config: %v", err)
	}

	// Known good commit SHA from firewood repo
	commitSHA := "57a74c3a7fd7dcdda24f49a237bfa9fa69f26a85"

	// Clone with SHA and shallow depth (should fallback to full clone)
	err = CloneOrUpdateRepo(config.GitRepo, clonePath, commitSHA, 1, false)
	if err != nil {
		t.Fatalf("failed to clone with SHA: %v", err)
	}

	// Verify the repo was cloned
	if _, err := os.Stat(filepath.Join(clonePath, ".git")); os.IsNotExist(err) {
		t.Error("expected .git directory to exist")
	}

	// Verify we're at the correct commit
	currentRef, err := GetCurrentRef(clonePath)
	if err != nil {
		t.Fatalf("failed to get current ref: %v", err)
	}

	if currentRef != commitSHA {
		t.Errorf("expected commit %s, got %s", commitSHA, currentRef)
	}
}

// TestIsRepoDirty_Clean tests dirty detection on a clean repo
func TestIsRepoDirty_Clean(t *testing.T) {
	tmpDir := t.TempDir()
	clonePath := filepath.Join(tmpDir, "firewood")

	config, err := GetRepoConfig("firewood")
	if err != nil {
		t.Fatalf("failed to get config: %v", err)
	}

	// Clone repo
	err = CloneRepo(config.GitRepo, clonePath, config.DefaultBranch, 1)
	if err != nil {
		t.Fatalf("failed to clone: %v", err)
	}

	// Should not be dirty
	isDirty, err := IsRepoDirty(clonePath)
	if err != nil {
		t.Fatalf("failed to check dirty status: %v", err)
	}

	if isDirty {
		t.Error("expected clean repo, got dirty")
	}
}

// TestIsRepoDirty_WithChanges tests dirty detection with uncommitted changes
func TestIsRepoDirty_WithChanges(t *testing.T) {
	tmpDir := t.TempDir()
	clonePath := filepath.Join(tmpDir, "firewood")

	config, err := GetRepoConfig("firewood")
	if err != nil {
		t.Fatalf("failed to get config: %v", err)
	}

	// Clone repo
	err = CloneRepo(config.GitRepo, clonePath, config.DefaultBranch, 1)
	if err != nil {
		t.Fatalf("failed to clone: %v", err)
	}

	// Make a change to the repo
	testFile := filepath.Join(clonePath, "test_file.txt")
	err = os.WriteFile(testFile, []byte("test content"), 0644)
	if err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	// Should be dirty now
	isDirty, err := IsRepoDirty(clonePath)
	if err != nil {
		t.Fatalf("failed to check dirty status: %v", err)
	}

	if !isDirty {
		t.Error("expected dirty repo, got clean")
	}
}

// TestCloneOrUpdateRepo_ExistingRepoWithoutForce tests that cloning fails when repo exists without force
func TestCloneOrUpdateRepo_ExistingRepoWithoutForce(t *testing.T) {
	tmpDir := t.TempDir()
	clonePath := filepath.Join(tmpDir, "firewood")

	config, err := GetRepoConfig("firewood")
	if err != nil {
		t.Fatalf("failed to get config: %v", err)
	}

	// Clone repo first time
	err = CloneOrUpdateRepo(config.GitRepo, clonePath, config.DefaultBranch, 1, false)
	if err != nil {
		t.Fatalf("failed to clone: %v", err)
	}

	// Try to clone again without force - should fail
	err = CloneOrUpdateRepo(config.GitRepo, clonePath, config.DefaultBranch, 1, false)
	if err == nil {
		t.Fatal("expected error when cloning to existing path without force")
	}

	if !IsErrRepoAlreadyExists(err) {
		t.Errorf("expected ErrRepoAlreadyExists, got: %v", err)
	}
}

// TestCloneOrUpdateRepo_ExistingRepoWithForce tests that cloning succeeds when repo exists with force
func TestCloneOrUpdateRepo_ExistingRepoWithForce(t *testing.T) {
	tmpDir := t.TempDir()
	clonePath := filepath.Join(tmpDir, "firewood")

	config, err := GetRepoConfig("firewood")
	if err != nil {
		t.Fatalf("failed to get config: %v", err)
	}

	// Clone repo first time
	err = CloneOrUpdateRepo(config.GitRepo, clonePath, config.DefaultBranch, 1, false)
	if err != nil {
		t.Fatalf("failed to clone: %v", err)
	}

	// Try to clone again with force - should succeed
	err = CloneOrUpdateRepo(config.GitRepo, clonePath, config.DefaultBranch, 1, true)
	if err != nil {
		t.Fatalf("expected success when cloning to existing path with force, got: %v", err)
	}
}

// TestGetRepoStatus_NotCloned tests status for a repo that hasn't been cloned
func TestGetRepoStatus_NotCloned(t *testing.T) {
	tmpDir := t.TempDir()

	status, err := GetRepoStatus("firewood", tmpDir, "")
	if err != nil {
		t.Fatalf("failed to get status: %v", err)
	}

	if status.Exists {
		t.Error("expected repo to not exist")
	}

	if status.Name != "firewood" {
		t.Errorf("expected name 'firewood', got %s", status.Name)
	}
}

// TestGetRepoStatus_Cloned tests status for a cloned repo
func TestGetRepoStatus_Cloned(t *testing.T) {
	tmpDir := t.TempDir()
	clonePath := filepath.Join(tmpDir, "firewood")

	config, err := GetRepoConfig("firewood")
	if err != nil {
		t.Fatalf("failed to get config: %v", err)
	}

	// Clone repo
	err = CloneRepo(config.GitRepo, clonePath, config.DefaultBranch, 1)
	if err != nil {
		t.Fatalf("failed to clone: %v", err)
	}

	status, err := GetRepoStatus("firewood", tmpDir, "")
	if err != nil {
		t.Fatalf("failed to get status: %v", err)
	}

	if !status.Exists {
		t.Error("expected repo to exist")
	}

	if status.CurrentRef != config.DefaultBranch {
		t.Errorf("expected ref %s, got %s", config.DefaultBranch, status.CurrentRef)
	}

	if status.IsDirty {
		t.Error("expected clean repo")
	}
}

// TestGetRepoStatus_WithReplaceDirective tests status shows replace directive
func TestGetRepoStatus_WithReplaceDirective(t *testing.T) {
	tmpDir := t.TempDir()
	clonePath := filepath.Join(tmpDir, "firewood")

	config, err := GetRepoConfig("firewood")
	if err != nil {
		t.Fatalf("failed to get config: %v", err)
	}

	// Clone repo
	err = CloneRepo(config.GitRepo, clonePath, config.DefaultBranch, 1)
	if err != nil {
		t.Fatalf("failed to clone: %v", err)
	}

	// Create a go.mod file
	goModPath := filepath.Join(tmpDir, "go.mod")
	goModContent := `module example.com/test

go 1.21

require github.com/ava-labs/firewood/ffi v0.0.0
`
	err = os.WriteFile(goModPath, []byte(goModContent), 0644)
	if err != nil {
		t.Fatalf("failed to write go.mod: %v", err)
	}

	// Add replace directive
	replacePath := "./firewood/ffi/result/ffi"
	err = AddReplaceDirective(goModPath, config.GoModule, replacePath)
	if err != nil {
		t.Fatalf("failed to add replace directive: %v", err)
	}

	// Get status
	status, err := GetRepoStatus("firewood", tmpDir, goModPath)
	if err != nil {
		t.Fatalf("failed to get status: %v", err)
	}

	if !status.HasReplace {
		t.Error("expected replace directive to be detected")
	}

	if status.ReplacePath != replacePath {
		t.Errorf("expected replace path %s, got %s", replacePath, status.ReplacePath)
	}
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
			if output == "" {
				t.Error("expected non-empty output")
			}
			if tt.contains != "" && !contains(output, tt.contains) {
				t.Errorf("expected output to contain %q, got: %s", tt.contains, output)
			}
		})
	}
}

// Helper function since strings.Contains isn't imported
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 ||
		(len(s) > 0 && (s[0:len(substr)] == substr || contains(s[1:], substr))))
}

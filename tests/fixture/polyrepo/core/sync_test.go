// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package core

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDetectCurrentRepo(t *testing.T) {
	tests := []struct {
		name           string
		goModContent   string
		goModSubPath   string // if set, create go.mod in this subdir
		expectedRepo   string
		expectedError  bool
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
					err := os.MkdirAll(subDir, 0755)
					if err != nil {
						t.Fatalf("failed to create subdir: %v", err)
					}
					goModPath = filepath.Join(subDir, "go.mod")
				}

				err := os.WriteFile(goModPath, []byte(tt.goModContent), 0644)
				if err != nil {
					t.Fatalf("failed to write go.mod: %v", err)
				}
			}

			repo, err := DetectCurrentRepo(tmpDir)

			if tt.expectedError {
				if err == nil {
					t.Error("expected error but got nil")
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if repo != tt.expectedRepo {
				t.Errorf("expected repo '%s', got '%s'", tt.expectedRepo, repo)
			}
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

			if len(repos) != len(tt.expectedRepos) {
				t.Fatalf("expected %d repos, got %d", len(tt.expectedRepos), len(repos))
			}

			for i, expectedRepo := range tt.expectedRepos {
				if repos[i] != expectedRepo {
					t.Errorf("at index %d: expected '%s', got '%s'", i, expectedRepo, repos[i])
				}
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
			name:        "avalanchego depends on firewood",
			currentRepo: "avalanchego",
			targetRepo:  "firewood",
			goModContent: `module github.com/ava-labs/avalanchego

go 1.21

require github.com/ava-labs/firewood/ffi v0.0.0-20240101120000-abc123def456
`,
			expectedRef: "v0.0.0-20240101120000-abc123def456",
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
			name:        "firewood has no dependency on avalanchego - uses default branch",
			currentRepo: "firewood",
			targetRepo:  "avalanchego",
			goModContent: `module github.com/ava-labs/firewood/ffi

go 1.21
`,
			expectedRef: "master",
			expectError: false,
		},
		{
			name:        "firewood has no dependency on coreth - uses default branch",
			currentRepo: "firewood",
			targetRepo:  "coreth",
			goModContent: `module github.com/ava-labs/firewood/ffi

go 1.21
`,
			expectedRef: "master",
			expectError: false,
		},
		{
			name:        "no go.mod path - uses default branch",
			currentRepo: "avalanchego",
			targetRepo:  "coreth",
			goModContent: "",
			expectedRef: "master",
			expectError: false,
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
				err := os.WriteFile(goModPath, []byte(tt.goModContent), 0644)
				if err != nil {
					t.Fatalf("failed to write go.mod: %v", err)
				}
			}

			ref, err := GetDefaultRefForRepo(tt.currentRepo, tt.targetRepo, goModPath)

			if tt.expectError {
				if err == nil {
					t.Error("expected error but got nil")
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if ref != tt.expectedRef {
				t.Errorf("expected ref '%s', got '%s'", tt.expectedRef, ref)
			}
		})
	}
}

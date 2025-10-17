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

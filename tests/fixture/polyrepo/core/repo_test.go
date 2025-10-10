// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package core

import (
	"testing"
)

func TestParseRepoAndVersion(t *testing.T) {
	tests := []struct {
		name          string
		input         string
		expectedRepo  string
		expectedVer   string
		expectedError bool
	}{
		{
			name:          "repo with version",
			input:         "avalanchego@v1.11.11",
			expectedRepo:  "avalanchego",
			expectedVer:   "v1.11.11",
			expectedError: false,
		},
		{
			name:          "repo with commit SHA",
			input:         "coreth@abc123def",
			expectedRepo:  "coreth",
			expectedVer:   "abc123def",
			expectedError: false,
		},
		{
			name:          "repo with branch",
			input:         "firewood@main",
			expectedRepo:  "firewood",
			expectedVer:   "main",
			expectedError: false,
		},
		{
			name:          "repo without version",
			input:         "avalanchego",
			expectedRepo:  "avalanchego",
			expectedVer:   "",
			expectedError: false,
		},
		{
			name:          "invalid format - multiple @",
			input:         "repo@version@extra",
			expectedRepo:  "",
			expectedVer:   "",
			expectedError: true,
		},
		{
			name:          "empty string",
			input:         "",
			expectedRepo:  "",
			expectedVer:   "",
			expectedError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo, version, err := ParseRepoAndVersion(tt.input)

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

			if version != tt.expectedVer {
				t.Errorf("expected version '%s', got '%s'", tt.expectedVer, version)
			}
		})
	}
}

func TestGetRepoClonePath(t *testing.T) {
	tests := []struct {
		name         string
		repoName     string
		baseDir      string
		expectedPath string
	}{
		{
			name:         "avalanchego in current dir",
			repoName:     "avalanchego",
			baseDir:      ".",
			expectedPath: "avalanchego",
		},
		{
			name:         "coreth in custom dir",
			repoName:     "coreth",
			baseDir:      "/tmp/test",
			expectedPath: "/tmp/test/coreth",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := GetRepoClonePath(tt.repoName, tt.baseDir)
			if path != tt.expectedPath {
				t.Errorf("expected path '%s', got '%s'", tt.expectedPath, path)
			}
		})
	}
}

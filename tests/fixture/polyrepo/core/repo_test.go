// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package core

import (
	"testing"

	"github.com/stretchr/testify/require"
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
					require.Fail(t, "expected error but got nil")
				}
				return
			}

			require.NoError(t, err)
			require.Equal(t, tt.expectedRepo, repo)
			require.Equal(t, tt.expectedVer, version)
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
			require.Equal(t, tt.expectedPath, path)
		})
	}
}

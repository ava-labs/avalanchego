// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package core

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGetNixBuildPath(t *testing.T) {
	tests := []struct {
		name         string
		repoPath     string
		nixSubPath   string
		expectedPath string
	}{
		{
			name:         "firewood ffi path",
			repoPath:     "/tmp/firewood",
			nixSubPath:   "ffi",
			expectedPath: "/tmp/firewood/ffi",
		},
		{
			name:         "root path",
			repoPath:     "./myrepo",
			nixSubPath:   ".",
			expectedPath: "./myrepo",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := GetNixBuildPath(tt.repoPath, tt.nixSubPath)
			require.Equal(t, tt.expectedPath, path)
		})
	}
}

func TestGetNixResultPath(t *testing.T) {
	tests := []struct {
		name          string
		nixBuildPath  string
		resultSubPath string
		expectedPath  string
	}{
		{
			name:          "firewood ffi result",
			nixBuildPath:  "/tmp/firewood/ffi",
			resultSubPath: "ffi",
			expectedPath:  "/tmp/firewood/ffi/result/ffi",
		},
		{
			name:          "simple result",
			nixBuildPath:  "./build",
			resultSubPath: "output",
			expectedPath:  "build/result/output",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := GetNixResultPath(tt.nixBuildPath, tt.resultSubPath)
			require.Equal(t, tt.expectedPath, path)
		})
	}
}

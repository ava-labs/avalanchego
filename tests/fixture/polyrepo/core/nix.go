// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package core

import (
	"fmt"
	"os/exec"
	"path/filepath"
)

// GetNixBuildPath returns the path where nix build should be run
func GetNixBuildPath(repoPath, nixSubPath string) string {
	if nixSubPath == "." {
		return repoPath
	}
	return filepath.Join(repoPath, nixSubPath)
}

// GetNixResultPath returns the path to the nix build result
func GetNixResultPath(nixBuildPath, resultSubPath string) string {
	return filepath.Join(nixBuildPath, "result", resultSubPath)
}

// RunNixBuild runs nix build in the specified directory
func RunNixBuild(buildPath string) error {
	cmd := exec.Command("nix", "build")
	cmd.Dir = buildPath

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("nix build failed: %w\nOutput: %s", err, string(output))
	}

	return nil
}

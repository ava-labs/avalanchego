// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package core

import (
	"os/exec"
	"path/filepath"

	"github.com/ava-labs/avalanchego/tests/fixture/stacktrace"
	"github.com/ava-labs/avalanchego/utils/logging"
	"go.uber.org/zap"
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
func RunNixBuild(log logging.Logger, buildPath string) error {
	log.Debug("running nix build",
		zap.String("buildPath", buildPath),
	)

	cmd := exec.Command("nix", "build")
	cmd.Dir = buildPath

	log.Debug("executing command",
		zap.String("cmd", "nix"),
		zap.String("args", "build"),
		zap.String("dir", buildPath),
	)

	output, err := cmd.CombinedOutput()
	if err != nil {
		log.Error("nix build failed",
			zap.String("buildPath", buildPath),
			zap.String("output", string(output)),
			zap.Error(err),
		)
		return stacktrace.Errorf("nix build failed: %w\nOutput: %s", err, string(output))
	}

	log.Debug("nix build completed successfully",
		zap.Int("outputBytes", len(output)),
	)

	return nil
}

// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package core

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/tests/fixture/stacktrace"
	"github.com/ava-labs/avalanchego/utils/logging"
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

// FileExistsAtRevision checks if a file exists at a specific git revision
func FileExistsAtRevision(log logging.Logger, repoPath, filePath, revision string) (bool, error) {
	log.Debug("checking if file exists at revision",
		zap.String("repoPath", repoPath),
		zap.String("filePath", filePath),
		zap.String("revision", revision),
	)

	// Use git cat-file to check if the file exists at the given revision
	//#nosec G204 -- revision and filePath are controlled by the tool, not user input
	cmd := exec.Command("git", "cat-file", "-e", revision+":"+filePath)
	cmd.Dir = repoPath

	err := cmd.Run()
	if err != nil {
		// Exit code 128 or 1 means file doesn't exist
		log.Debug("file does not exist at revision",
			zap.String("filePath", filePath),
			zap.String("revision", revision),
			zap.Error(err),
		)
		return false, nil
	}

	log.Debug("file exists at revision",
		zap.String("filePath", filePath),
		zap.String("revision", revision),
	)
	return true, nil
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

// RunCargoBuildInNixShell runs cargo build from within a nix develop shell
// This is used as a fallback when flake.nix is missing at the current revision
func RunCargoBuildInNixShell(log logging.Logger, repoPath, nixSubPath, fallbackRevision string) error {
	buildPath := GetNixBuildPath(repoPath, nixSubPath)

	log.Info("running cargo build in nix shell (fallback mode)",
		zap.String("buildPath", buildPath),
		zap.String("fallbackRevision", fallbackRevision),
	)

	// Build nix develop command using GitHub shorthand syntax
	// Hardcoded to ava-labs/firewood because this function is firewood-specific
	nixFlakeRef := fmt.Sprintf("github:ava-labs/firewood?dir=%s&ref=%s", nixSubPath, fallbackRevision)

	log.Debug("using nix flake reference",
		zap.String("flakeRef", nixFlakeRef),
	)

	// Run cargo build from nix develop shell
	cmd := exec.Command("nix", "develop", nixFlakeRef, "--command", "cargo", "build", "--release")
	cmd.Dir = buildPath

	log.Debug("executing command",
		zap.String("cmd", "nix develop"),
		zap.String("flakeRef", nixFlakeRef),
		zap.String("dir", buildPath),
	)

	output, err := cmd.CombinedOutput()
	if err != nil {
		log.Error("cargo build in nix shell failed",
			zap.String("buildPath", buildPath),
			zap.String("output", string(output)),
			zap.Error(err),
		)
		return stacktrace.Errorf("cargo build in nix shell failed: %w\nOutput: %s", err, string(output))
	}

	log.Info("cargo build in nix shell completed successfully",
		zap.Int("outputBytes", len(output)),
	)

	// Create symlink to mimic nix build output structure
	// The built artifacts should be in target/release/
	resultPath := filepath.Join(buildPath, "result")
	targetPath := filepath.Join(buildPath, "target", "release")

	// Remove existing result symlink if it exists
	os.Remove(resultPath)

	// Create symlink result -> target/release
	err = os.Symlink(targetPath, resultPath)
	if err != nil {
		log.Warn("failed to create result symlink",
			zap.Error(err),
		)
	}

	return nil
}

// BuildFirewood builds firewood using either nix build or cargo build in nix shell
// It checks if ffi/flake.nix exists at the current revision, and if not, falls back
// to using flake.nix from a known good revision from GitHub
func BuildFirewood(log logging.Logger, repoPath, ref string) error {
	const (
		nixSubPath       = "ffi"
		flakeRelPath     = "ffi/flake.nix"
		fallbackRevision = "81c105e62fe7003231c161b2c414bc8827f29e78"
	)

	log.Info("building firewood",
		zap.String("repoPath", repoPath),
		zap.String("ref", ref),
	)

	// Check if flake.nix exists at the current revision
	exists, err := FileExistsAtRevision(log, repoPath, flakeRelPath, "HEAD")
	if err != nil {
		return stacktrace.Errorf("failed to check if flake.nix exists: %w", err)
	}

	buildPath := GetNixBuildPath(repoPath, nixSubPath)

	if exists {
		log.Info("flake.nix exists at current revision, using nix build")
		return RunNixBuild(log, buildPath)
	}

	log.Info("flake.nix not found at current revision, using cargo build with nix shell fallback",
		zap.String("fallbackRevision", fallbackRevision),
	)

	return RunCargoBuildInNixShell(log, repoPath, nixSubPath, fallbackRevision)
}

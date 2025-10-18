// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package core

import (
	"os"
	"path/filepath"

	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/tests/fixture/stacktrace"
	"github.com/ava-labs/avalanchego/utils/logging"
)

// UpdateAvalanchego updates the avalanchego dependency version in go.mod.
// If version is empty, uses the current version from go.mod.
// Returns error if go.mod doesn't exist in baseDir or operation fails.
func UpdateAvalanchego(log logging.Logger, baseDir string, version string) error {
	// Validate go.mod exists in baseDir
	goModPath := filepath.Join(baseDir, "go.mod")
	if _, err := os.Stat(goModPath); err != nil {
		if os.IsNotExist(err) {
			return stacktrace.Errorf("go.mod not found in current directory")
		}
		return stacktrace.Errorf("failed to stat go.mod: %w", err)
	}

	log.Debug("updating avalanchego dependency",
		zap.String("baseDir", baseDir),
		zap.String("goModPath", goModPath),
		zap.String("version", version),
	)

	// Check that we're not in avalanchego
	modulePath, err := GetModulePath(log, goModPath)
	if err != nil {
		return stacktrace.Errorf("failed to get module path: %w", err)
	}

	log.Debug("checking if running from avalanchego repository",
		zap.String("modulePath", modulePath),
	)

	avalanchegoConfig, _ := GetRepoConfig("avalanchego")
	if modulePath == avalanchegoConfig.GoModule {
		log.Debug("detected avalanchego repository, cannot update from itself")
		return stacktrace.Errorf("update-avalanchego cannot be run from avalanchego repository")
	}

	// If no version specified, get the current version from go.mod
	if version == "" {
		log.Debug("no version specified, getting current version from go.mod")
		version, err = GetDependencyVersion(log, goModPath, avalanchegoConfig.GoModule)
		if err != nil {
			return stacktrace.Errorf("failed to get current avalanchego version: %w", err)
		}
		log.Debug("using current version from go.mod",
			zap.String("version", version),
		)
	}

	// TODO: Update go.mod dependency (requires additional modfile functionality)
	// TODO: Update GitHub Actions references (deferred per plan)

	log.Debug("update-avalanchego not fully implemented yet",
		zap.String("version", version),
	)
	return stacktrace.Errorf("update-avalanchego not fully implemented yet (version=%s)", version)
}

// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package core

import (
	"github.com/ava-labs/avalanchego/tests/fixture/stacktrace"
	"github.com/ava-labs/avalanchego/utils/logging"
	"go.uber.org/zap"
)

// UpdateAvalanchego updates the avalanchego dependency in the current repo's go.mod
// This should NOT be run from avalanchego itself
func UpdateAvalanchego(log logging.Logger, goModPath, version string) error {
	log.Debug("updating avalanchego dependency",
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

// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package core

import "fmt"

// UpdateAvalanchego updates the avalanchego dependency in the current repo's go.mod
// This should NOT be run from avalanchego itself
func UpdateAvalanchego(goModPath, version string) error {
	// Check that we're not in avalanchego
	modulePath, err := GetModulePath(goModPath)
	if err != nil {
		return fmt.Errorf("failed to get module path: %w", err)
	}

	avalanchegoConfig, _ := GetRepoConfig("avalanchego")
	if modulePath == avalanchegoConfig.GoModule {
		return fmt.Errorf("update-avalanchego cannot be run from avalanchego repository")
	}

	// If no version specified, get the current version from go.mod
	if version == "" {
		version, err = GetDependencyVersion(goModPath, avalanchegoConfig.GoModule)
		if err != nil {
			return fmt.Errorf("failed to get current avalanchego version: %w", err)
		}
	}

	// TODO: Update go.mod dependency (requires additional modfile functionality)
	// TODO: Update GitHub Actions references (deferred per plan)

	return fmt.Errorf("update-avalanchego not fully implemented yet (version=%s)", version)
}

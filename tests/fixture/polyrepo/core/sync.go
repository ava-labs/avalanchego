// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package core

import (
	"os"
	"path/filepath"

	"github.com/ava-labs/avalanchego/tests/fixture/stacktrace"
)

// DetectCurrentRepo detects which repository we're currently in based on go.mod
func DetectCurrentRepo(dir string) (string, error) {
	// Check for regular go.mod in current directory
	goModPath := filepath.Join(dir, "go.mod")
	if _, err := os.Stat(goModPath); err == nil {
		modulePath, err := GetModulePath(goModPath)
		if err != nil {
			return "", stacktrace.Errorf("failed to get module path: %w", err)
		}

		// Check against known repos
		for _, config := range GetAllRepoConfigs() {
			if config.GoModule == modulePath {
				return config.Name, nil
			}
		}
	}

	// Check for firewood's special case (ffi/go.mod)
	firewoodConfig, _ := GetRepoConfig("firewood")
	if firewoodConfig.GoModPath != "" && firewoodConfig.GoModPath != "go.mod" {
		subGoModPath := filepath.Join(dir, firewoodConfig.GoModPath)
		if _, err := os.Stat(subGoModPath); err == nil {
			modulePath, err := GetModulePath(subGoModPath)
			if err != nil {
				return "", stacktrace.Errorf("failed to get module path: %w", err)
			}

			if modulePath == firewoodConfig.GoModule {
				return "firewood", nil
			}
		}
	}

	// Not in a known repo
	return "", nil
}

// GetReposToSync returns the list of repos to sync based on the current repo
func GetReposToSync(currentRepo string) []string {
	allRepos := []string{"avalanchego", "coreth", "firewood"}

	// If we're not in a known repo, sync all
	if currentRepo == "" {
		return allRepos
	}

	// Otherwise, sync all repos except the current one
	result := make([]string, 0, len(allRepos)-1)
	for _, repo := range allRepos {
		if repo != currentRepo {
			result = append(result, repo)
		}
	}

	return result
}

// GetDefaultRefForRepo determines the default ref to use for a target repo
// when syncing from a current repo. If the current repo has a go.mod with a
// dependency on the target repo, returns that version converted to a git ref.
// For pseudo-versions, this extracts the commit hash. Otherwise, returns
// the target repo's default branch.
func GetDefaultRefForRepo(currentRepo, targetRepo, goModPath string) (string, error) {
	// Get the target repo's configuration
	targetConfig, err := GetRepoConfig(targetRepo)
	if err != nil {
		return "", err
	}

	// If there's no go.mod or no current repo, use default branch
	if goModPath == "" || currentRepo == "" {
		return targetConfig.DefaultBranch, nil
	}

	// Try to get the dependency version from go.mod
	version, err := GetDependencyVersion(goModPath, targetConfig.GoModule)
	if err != nil {
		// If dependency not found, use default branch
		return targetConfig.DefaultBranch, nil
	}

	// Convert the version to a git ref (handles pseudo-versions)
	gitRef, err := ConvertVersionToGitRef(version)
	if err != nil {
		return "", stacktrace.Errorf("failed to convert version %s to git ref: %w", version, err)
	}

	return gitRef, nil
}

// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package core

import (
	"os"
	"path/filepath"

	"github.com/ava-labs/avalanchego/tests/fixture/stacktrace"
	"github.com/ava-labs/avalanchego/utils/logging"
	"go.uber.org/zap"
)

// DetectCurrentRepo detects which repository we're currently in based on go.mod
func DetectCurrentRepo(log logging.Logger, dir string) (string, error) {
	log.Debug("detecting current repository",
		zap.String("dir", dir),
	)

	// Check for regular go.mod in current directory
	goModPath := filepath.Join(dir, "go.mod")
	if _, err := os.Stat(goModPath); err == nil {
		log.Debug("found go.mod in current directory")

		modulePath, err := GetModulePath(log, goModPath)
		if err != nil {
			return "", stacktrace.Errorf("failed to get module path: %w", err)
		}

		log.Debug("checking module path against known repos",
			zap.String("modulePath", modulePath),
		)

		// Check against known repos
		for _, config := range GetAllRepoConfigs() {
			if config.GoModule == modulePath {
				log.Debug("matched known repository",
					zap.String("repoName", config.Name),
					zap.String("module", modulePath),
				)
				return config.Name, nil
			}
		}

		log.Debug("module path does not match any known repo")
	} else {
		log.Debug("no go.mod in current directory")
	}

	// Check for firewood's special case (ffi/go.mod)
	firewoodConfig, _ := GetRepoConfig("firewood")
	if firewoodConfig.GoModPath != "" && firewoodConfig.GoModPath != "go.mod" {
		subGoModPath := filepath.Join(dir, firewoodConfig.GoModPath)
		log.Debug("checking for firewood special case",
			zap.String("path", subGoModPath),
		)

		if _, err := os.Stat(subGoModPath); err == nil {
			modulePath, err := GetModulePath(log, subGoModPath)
			if err != nil {
				return "", stacktrace.Errorf("failed to get module path: %w", err)
			}

			if modulePath == firewoodConfig.GoModule {
				log.Debug("detected firewood repository")
				return "firewood", nil
			}
		}
	}

	// Not in a known repo
	log.Debug("not in a known repository")
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
func GetDefaultRefForRepo(log logging.Logger, currentRepo, targetRepo, goModPath string) (string, error) {
	log.Info("determining default reference for repository",
		zap.String("currentRepo", currentRepo),
		zap.String("targetRepo", targetRepo),
		zap.String("goModPath", goModPath),
	)

	// Get the target repo's configuration
	targetConfig, err := GetRepoConfig(targetRepo)
	if err != nil {
		return "", err
	}

	log.Debug("target repository configuration",
		zap.String("goModule", targetConfig.GoModule),
		zap.String("defaultBranch", targetConfig.DefaultBranch),
	)

	// If there's no go.mod or no current repo, use default branch
	if goModPath == "" || currentRepo == "" {
		log.Info("using default branch (no go.mod or current repo)",
			zap.String("targetRepo", targetRepo),
			zap.String("defaultBranch", targetConfig.DefaultBranch),
		)
		return targetConfig.DefaultBranch, nil
	}

	// Try to get the dependency version from go.mod
	version, err := GetDependencyVersion(log, goModPath, targetConfig.GoModule)
	if err != nil {
		// If dependency not found, use default branch
		log.Info("using default branch (dependency not found in go.mod)",
			zap.String("targetRepo", targetRepo),
			zap.String("defaultBranch", targetConfig.DefaultBranch),
		)
		return targetConfig.DefaultBranch, nil
	}

	log.Info("found dependency version in go.mod",
		zap.String("targetRepo", targetRepo),
		zap.String("module", targetConfig.GoModule),
		zap.String("version", version),
	)

	// Convert the version to a git ref (handles pseudo-versions)
	gitRef, err := ConvertVersionToGitRef(log, version)
	if err != nil {
		return "", stacktrace.Errorf("failed to convert version %s to git ref: %w", version, err)
	}

	log.Info("converted version to git reference",
		zap.String("version", version),
		zap.String("gitRef", gitRef),
	)

	return gitRef, nil
}

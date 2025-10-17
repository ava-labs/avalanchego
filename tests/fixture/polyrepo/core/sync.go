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
		return targetConfig.DefaultBranch, nil //nolint:nilerr // Error is intentionally ignored - missing dependency is expected
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

// UpdateAllReplaceDirectives updates go.mod files for all repos (primary and synced)
// to add replace directives for any locally cloned dependencies.
// This ensures the full dependency graph is properly configured with replace directives.
func UpdateAllReplaceDirectives(log logging.Logger, baseDir string, syncedRepos []string) error {
	log.Info("updating replace directives for all repos",
		zap.String("baseDir", baseDir),
		zap.Strings("syncedRepos", syncedRepos),
	)

	// Build a set of all repos we need to check (primary + synced)
	allRepos := make(map[string]bool)

	// Detect the primary repo
	primaryRepo, err := DetectCurrentRepo(log, baseDir)
	if err != nil {
		return stacktrace.Errorf("failed to detect current repo: %w", err)
	}

	if primaryRepo != "" {
		allRepos[primaryRepo] = true
		log.Debug("detected primary repository",
			zap.String("repo", primaryRepo),
		)
	}

	// Add all synced repos
	for _, repo := range syncedRepos {
		allRepos[repo] = true
	}

	log.Info("identified all repos to update",
		zap.Int("count", len(allRepos)),
	)

	// For each repo, check if it exists and has a go.mod
	for repoName := range allRepos {
		config, err := GetRepoConfig(repoName)
		if err != nil {
			log.Warn("failed to get config for repo, skipping",
				zap.String("repo", repoName),
				zap.Error(err),
			)
			continue
		}

		// Determine the go.mod path for this repo
		var goModPath string
		if repoName == primaryRepo {
			// Primary repo - use go.mod in base directory
			goModPath = filepath.Join(baseDir, config.GoModPath)
		} else {
			// Synced repo - use go.mod in the cloned directory
			clonePath := GetRepoClonePath(repoName, baseDir)
			goModPath = filepath.Join(clonePath, config.GoModPath)
		}

		// Check if go.mod exists
		if _, err := os.Stat(goModPath); err != nil {
			if os.IsNotExist(err) {
				log.Debug("go.mod does not exist for repo, skipping",
					zap.String("repo", repoName),
					zap.String("goModPath", goModPath),
				)
				continue
			}
			return stacktrace.Errorf("failed to stat go.mod for %s: %w", repoName, err)
		}

		log.Debug("checking dependencies for repo",
			zap.String("repo", repoName),
			zap.String("goModPath", goModPath),
		)

		// For each other repo, check if this repo depends on it
		for otherRepoName := range allRepos {
			if otherRepoName == repoName {
				continue // Skip self
			}

			otherConfig, err := GetRepoConfig(otherRepoName)
			if err != nil {
				log.Warn("failed to get config for other repo, skipping",
					zap.String("otherRepo", otherRepoName),
					zap.Error(err),
				)
				continue
			}

			// Check if this repo's go.mod depends on the other repo
			_, err = GetDependencyVersion(log, goModPath, otherConfig.GoModule)
			if err != nil {
				// Dependency not found, skip
				log.Debug("repo does not depend on other repo",
					zap.String("repo", repoName),
					zap.String("otherRepo", otherRepoName),
					zap.String("module", otherConfig.GoModule),
				)
				continue
			}

			log.Info("found dependency, adding replace directive",
				zap.String("repo", repoName),
				zap.String("dependency", otherRepoName),
				zap.String("module", otherConfig.GoModule),
			)

			// Calculate the replace path (always relative)
			var replacePath string
			if otherRepoName == primaryRepo {
				// Dependency is the primary repo
				// Path is relative from the synced repo to the base dir
				if repoName == primaryRepo {
					// This shouldn't happen (self-dependency), but handle it
					log.Warn("unexpected self-dependency on primary repo",
						zap.String("repo", repoName),
					)
					continue
				}
				// From synced repo back to base dir
				replacePath = ".."
				if otherConfig.ModuleReplacementPath != "." {
					// Strip leading ./ from ModuleReplacementPath if present
					modPath := otherConfig.ModuleReplacementPath
					if len(modPath) >= 2 && modPath[0:2] == "./" {
						modPath = modPath[2:]
					}
					replacePath = filepath.Join(replacePath, modPath)
				}
			} else {
				// Dependency is another synced repo
				if repoName == primaryRepo {
					// From primary repo to synced repo
					// filepath.Join normalizes away ./ so we build it manually
					replacePath = otherRepoName
					if otherConfig.ModuleReplacementPath != "." {
						// Strip leading ./ from ModuleReplacementPath if present
						modPath := otherConfig.ModuleReplacementPath
						if len(modPath) >= 2 && modPath[0:2] == "./" {
							modPath = modPath[2:]
						}
						replacePath = filepath.Join(replacePath, modPath)
					}
					// Prepend ./ after all joins to ensure valid go.mod syntax
					replacePath = "./" + replacePath
				} else {
					// From synced repo to another synced repo (sibling)
					replacePath = otherRepoName
					if otherConfig.ModuleReplacementPath != "." {
						// Strip leading ./ from ModuleReplacementPath if present
						modPath := otherConfig.ModuleReplacementPath
						if len(modPath) >= 2 && modPath[0:2] == "./" {
							modPath = modPath[2:]
						}
						replacePath = filepath.Join(replacePath, modPath)
					}
					// Prepend ../ for sibling repos
					replacePath = "../" + replacePath
				}
			}

			log.Info("adding replace directive",
				zap.String("repo", repoName),
				zap.String("goModPath", goModPath),
				zap.String("module", otherConfig.GoModule),
				zap.String("replacePath", replacePath),
			)

			err = AddReplaceDirective(log, goModPath, otherConfig.GoModule, replacePath)
			if err != nil {
				return stacktrace.Errorf("failed to add replace directive for %s in %s: %w",
					otherRepoName, repoName, err)
			}
		}
	}

	log.Info("successfully updated all replace directives")
	return nil
}

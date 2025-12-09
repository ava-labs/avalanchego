// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package core

import (
	"os"
	"os/exec"
	"path/filepath"

	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/tests/fixture/stacktrace"
	"github.com/ava-labs/avalanchego/utils/logging"
)

const (
	repoAvalanchego = "avalanchego"
	repoFirewood    = "firewood"
)

// GetFirewoodReplacementPath returns the correct replacement path for firewood
// based on which build method was used (nix build or cargo build).
// For non-firewood repos, returns the config default.
// isPrimary indicates if this repo is the primary repo (baseDir IS the repo) vs synced (baseDir contains repo/).
func GetFirewoodReplacementPath(log logging.Logger, baseDir, repoName string, isPrimary bool) string {
	// For non-firewood repos, return the config default
	if repoName != repoFirewood {
		config, err := GetRepoConfig(repoName)
		if err != nil {
			// Shouldn't happen for known repos, but handle gracefully
			return "."
		}
		return config.ModuleReplacementPath
	}

	// Check if nix build output exists
	var nixPath string
	if isPrimary {
		// Primary repo: baseDir IS the firewood directory
		nixPath = filepath.Join(baseDir, "ffi", "result", "ffi", "go.mod")
	} else {
		// Synced repo: baseDir contains firewood/ subdirectory
		nixPath = filepath.Join(baseDir, repoName, "ffi", "result", "ffi", "go.mod")
	}

	if _, err := os.Stat(nixPath); err == nil {
		log.Info("detected nix build output for firewood",
			zap.String("path", "./ffi/result/ffi"),
			zap.Bool("isPrimary", isPrimary),
		)
		return "./ffi/result/ffi" // Nix build
	}

	log.Info("using cargo build path for firewood (nix output not found)",
		zap.String("path", "./ffi"),
		zap.Bool("isPrimary", isPrimary),
	)
	// Otherwise, cargo build (or not yet built)
	return "./ffi"
}

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
			if config.GoModule == modulePath || config.InternalGoModule == modulePath {
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

			if modulePath == firewoodConfig.GoModule || modulePath == firewoodConfig.InternalGoModule {
				log.Debug("detected firewood repository")
				return "firewood", nil
			}
		}
	}

	// Not in a known repo
	log.Debug("not in a known repository")
	return "", nil
}

// GetDirectDependencies determines which repos to sync when no explicit repos
// are specified.
// - From avalanchego: syncs firewood
// - From firewood: syncs avalanchego
// - Standalone mode (no primary repo): syncs both avalanchego and firewood
func GetDirectDependencies(log logging.Logger, currentRepo string) ([]string, error) {
	log.Debug("determining repositories to sync",
		zap.String("currentRepo", currentRepo),
	)

	// Standalone mode: sync both avalanchego and firewood
	if currentRepo == "" {
		log.Info("determined repositories to sync: avalanchego and firewood (standalone mode)")
		return []string{repoAvalanchego, repoFirewood}, nil
	}

	// From avalanchego: sync firewood
	if currentRepo == repoAvalanchego {
		log.Info("determined repositories to sync: only firewood (default for avalanchego)")
		return []string{repoFirewood}, nil
	}

	// From firewood: sync avalanchego
	log.Info("determined repositories to sync: only avalanchego (primary dependency)")
	return []string{repoAvalanchego}, nil
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
	version, depErr := GetDependencyVersion(log, goModPath, targetConfig.GoModule)
	if depErr != nil {
		// If dependency not found, use default branch
		// This is a valid scenario - you can sync a repo that isn't in your dependencies
		log.Info("dependency not found in go.mod, using default branch",
			zap.String("targetRepo", targetRepo),
			zap.String("currentRepo", currentRepo),
			zap.String("defaultBranch", targetConfig.DefaultBranch),
		)
		return targetConfig.DefaultBranch, nil //nolint:nilerr // Dependency not found is an expected case, not an error
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
				modPath := GetFirewoodReplacementPath(log, baseDir, otherRepoName, otherRepoName == primaryRepo)
				if modPath != "." {
					// Strip leading ./ from ModuleReplacementPath if present
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
					modPath := GetFirewoodReplacementPath(log, baseDir, otherRepoName, otherRepoName == primaryRepo)
					if modPath != "." {
						// Strip leading ./ from ModuleReplacementPath if present
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
					modPath := GetFirewoodReplacementPath(log, baseDir, otherRepoName, otherRepoName == primaryRepo)
					if modPath != "." {
						// Strip leading ./ from ModuleReplacementPath if present
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

	// Special handling: update graft/coreth/go.mod with firewood replace directive
	// when primary repo is avalanchego and firewood is synced
	if primaryRepo == repoAvalanchego && allRepos[repoFirewood] {
		graftedCorethGoMod := filepath.Join(baseDir, "graft", "coreth", "go.mod")

		// Check if grafted coreth go.mod exists
		if _, err := os.Stat(graftedCorethGoMod); err == nil {
			firewoodConfig, err := GetRepoConfig(repoFirewood)
			if err != nil {
				return stacktrace.Errorf("failed to get firewood config: %w", err)
			}

			// Check if grafted coreth depends on firewood
			_, err = GetDependencyVersion(log, graftedCorethGoMod, firewoodConfig.GoModule)
			if err == nil {
				// Calculate replace path from graft/coreth to firewood
				// graft/coreth/go.mod needs to point to ../../firewood/ffi/...
				firewoodPath := GetFirewoodReplacementPath(log, baseDir, repoFirewood, false)
				// Strip ./ prefix if present
				if len(firewoodPath) >= 2 && firewoodPath[:2] == "./" {
					firewoodPath = firewoodPath[2:]
				}
				// Path is ../../firewood/<firewoodPath>
				replacePath := "../../firewood/" + firewoodPath

				log.Info("adding firewood replace directive to grafted coreth",
					zap.String("goModPath", graftedCorethGoMod),
					zap.String("module", firewoodConfig.GoModule),
					zap.String("replacePath", replacePath),
				)

				err = AddReplaceDirective(log, graftedCorethGoMod, firewoodConfig.GoModule, replacePath)
				if err != nil {
					return stacktrace.Errorf("failed to add firewood replace directive to grafted coreth: %w", err)
				}
			} else {
				log.Debug("grafted coreth does not depend on firewood, skipping")
			}
		} else {
			log.Debug("grafted coreth go.mod does not exist, skipping")
		}
	}

	// Run go mod tidy for all repos to clean up indirect dependencies
	for repoName := range allRepos {
		config, err := GetRepoConfig(repoName)
		if err != nil {
			log.Warn("failed to get config for repo, skipping go mod tidy",
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

		// Check if go.mod exists before running tidy
		if _, err := os.Stat(goModPath); err != nil {
			if os.IsNotExist(err) {
				log.Debug("go.mod does not exist for repo, skipping go mod tidy",
					zap.String("repo", repoName),
					zap.String("goModPath", goModPath),
				)
				continue
			}
			return stacktrace.Errorf("failed to stat go.mod for %s: %w", repoName, err)
		}

		log.Info("running go mod tidy",
			zap.String("repo", repoName),
			zap.String("goModPath", goModPath),
		)

		// Run go mod tidy in the directory containing go.mod
		goModDir := filepath.Dir(goModPath)
		tidyCmd := exec.Command("go", "mod", "tidy")
		tidyCmd.Dir = goModDir
		if output, err := tidyCmd.CombinedOutput(); err != nil {
			return stacktrace.Errorf("failed to run go mod tidy for %s: %w\nOutput: %s",
				repoName, err, string(output))
		}

		log.Info("successfully ran go mod tidy",
			zap.String("repo", repoName),
		)
	}

	// Run go mod tidy for grafted coreth if we updated it
	if primaryRepo == repoAvalanchego && allRepos[repoFirewood] {
		graftedCorethGoMod := filepath.Join(baseDir, "graft", "coreth", "go.mod")
		if _, err := os.Stat(graftedCorethGoMod); err == nil {
			log.Info("running go mod tidy for grafted coreth",
				zap.String("goModPath", graftedCorethGoMod),
			)
			tidyCmd := exec.Command("go", "mod", "tidy")
			tidyCmd.Dir = filepath.Join(baseDir, "graft", "coreth")
			if output, err := tidyCmd.CombinedOutput(); err != nil {
				return stacktrace.Errorf("failed to run go mod tidy for grafted coreth: %w\nOutput: %s", err, string(output))
			}
			log.Info("successfully ran go mod tidy for grafted coreth")
		}
	}

	return nil
}

// DiscoverFirewoodVersion handles version discovery for firewood following the dependency chain.
// Priority: explicit ref > grafted coreth's go.mod (direct) > avalanchego's go.mod (indirect) > default branch
func DiscoverFirewoodVersion(
	log logging.Logger,
	baseDir string,
	explicitRef string,
) (string, error) {
	log.Info("discovering firewood version",
		zap.String("baseDir", baseDir),
		zap.String("explicitRef", explicitRef),
	)

	// If explicit ref provided, use it
	if explicitRef != "" {
		log.Info("using explicit firewood version",
			zap.String("ref", explicitRef),
		)
		return explicitRef, nil
	}

	firewoodConfig, err := GetRepoConfig("firewood")
	if err != nil {
		return "", stacktrace.Errorf("failed to get firewood config: %w", err)
	}

	// Check if avalanchego is already cloned
	avalanchegoPath := filepath.Join(baseDir, "avalanchego")
	if _, err := os.Stat(filepath.Join(avalanchegoPath, ".git")); err == nil {
		log.Debug("avalanchego is already cloned locally")

		// Priority 1: Discover from grafted coreth (direct dependency)
		graftedCorethGoMod := filepath.Join(avalanchegoPath, "graft", "coreth", "go.mod")
		if _, err := os.Stat(graftedCorethGoMod); err == nil {
			log.Info("discovering firewood version from grafted coreth (direct dependency)")
			version, err := GetDependencyVersion(log, graftedCorethGoMod, firewoodConfig.GoModule)
			if err == nil {
				gitRef, err := ConvertVersionToGitRef(log, version)
				if err != nil {
					return "", stacktrace.Errorf("failed to convert firewood version to git ref: %w", err)
				}
				log.Info("discovered firewood version from grafted coreth",
					zap.String("version", version),
					zap.String("gitRef", gitRef),
				)
				return gitRef, nil
			}
			log.Debug("grafted coreth does not depend on firewood, trying avalanchego")
		}

		// Priority 2: Discover from avalanchego (indirect dependency)
		log.Info("discovering firewood version from avalanchego (indirect dependency)")
		avalanchegoGoMod := filepath.Join(avalanchegoPath, "go.mod")

		version, err := GetDependencyVersion(log, avalanchegoGoMod, firewoodConfig.GoModule)
		if err != nil {
			return "", stacktrace.Errorf("failed to get firewood version from avalanchego go.mod: %w", err)
		}

		gitRef, err := ConvertVersionToGitRef(log, version)
		if err != nil {
			return "", stacktrace.Errorf("failed to convert firewood version to git ref: %w", err)
		}

		log.Info("discovered firewood version from avalanchego",
			zap.String("version", version),
			zap.String("gitRef", gitRef),
		)
		return gitRef, nil
	}

	// Priority 3: Use default branch (firewood alone, no primary repo)
	log.Info("using default branch for firewood (standalone mode)",
		zap.String("branch", firewoodConfig.DefaultBranch),
	)
	return firewoodConfig.DefaultBranch, nil
}

// Sync orchestrates the complete repository synchronization workflow.
// It handles both Primary Repo Mode (with go.mod) and Standalone Mode (without go.mod).
//
// Primary Repo Mode (go.mod exists in baseDir):
//   - Detects current repo from go.mod
//   - Discovers versions from go.mod dependencies using GetDefaultRefForRepo()
//   - Updates replace directives after syncing
//   - If no repoArgs provided, auto-detects repos to sync based on current repo
//
// Standalone Mode (no go.mod in baseDir):
//   - No primary repo detected
//   - Uses DiscoverFirewoodVersion for firewood version discovery
//   - No replace directives updated
//   - If no repoArgs provided, syncs both avalanchego and firewood by default
//
// Parameters:
//   - log: Logger instance
//   - baseDir: Directory to operate in (already validated by caller)
//   - repoArgs: CLI arguments (e.g., ["avalanchego", "firewood@main"])
//   - depth: Clone depth (0=full, 1=shallow, >1=partial)
//   - force: Force sync even if dirty or already exists
//
// Returns error if sync fails (fail fast - stop on first error).
func Sync(
	log logging.Logger,
	baseDir string,
	repoArgs []string,
	depth int,
	force bool,
) error {
	log.Info("starting sync",
		zap.String("baseDir", baseDir),
		zap.Int("repoArgsCount", len(repoArgs)),
		zap.Int("depth", depth),
		zap.Bool("force", force),
	)

	// Step 1: Detect operating mode by checking if we're in a known repo
	currentRepo, err := DetectCurrentRepo(log, baseDir)
	if err != nil {
		return stacktrace.Errorf("failed to detect current repo: %w", err)
	}

	hasPrimaryRepo := currentRepo != ""
	var goModPath string
	if hasPrimaryRepo {
		log.Info("detected primary repo mode (go.mod exists)")
		log.Info("detected current repository",
			zap.String("currentRepo", currentRepo),
		)

		// Get the go.mod path for the current repo (handles special cases like firewood's ffi/go.mod)
		config, err := GetRepoConfig(currentRepo)
		if err != nil {
			return stacktrace.Errorf("failed to get config for repo %s: %w", currentRepo, err)
		}
		goModPath = filepath.Join(baseDir, config.GoModPath)
	} else {
		log.Info("detected standalone mode (no go.mod)")
	}

	// Step 2: Determine repos to sync
	type repoWithRef struct {
		name        string
		ref         string
		wasExplicit bool
	}
	var reposToSync []repoWithRef

	if len(repoArgs) > 0 {
		log.Info("parsing repository arguments from command line",
			zap.Int("count", len(repoArgs)),
		)

		// Parse each repo[@ref] argument
		for _, arg := range repoArgs {
			repoName, ref, err := ParseRepoAndVersion(arg)
			if err != nil {
				return stacktrace.Errorf("invalid repo format %q: %w", arg, err)
			}

			// Step 4: Validation - cannot sync a repo into itself (Primary mode only)
			if hasPrimaryRepo && currentRepo != "" && repoName == currentRepo {
				return stacktrace.Errorf("cannot sync %s into itself (you are currently in %s)", repoName, currentRepo)
			}

			wasExplicit := ref != ""
			reposToSync = append(reposToSync, repoWithRef{
				name:        repoName,
				ref:         ref,
				wasExplicit: wasExplicit,
			})
		}
	} else {
		// No repos specified - auto-detect based on current context
		log.Info("no repositories specified, auto-detecting direct dependencies")
		repos, err := GetDirectDependencies(log, currentRepo)
		if err != nil {
			return stacktrace.Errorf("failed to determine direct dependencies: %w", err)
		}

		log.Info("determined repositories to sync",
			zap.Int("count", len(repos)),
			zap.Strings("repos", repos),
		)

		for _, repoName := range repos {
			reposToSync = append(reposToSync, repoWithRef{
				name:        repoName,
				ref:         "",
				wasExplicit: false,
			})
		}
	}

	// Step 3: Determine ref for each repo
	// We need to handle version discovery differently for Primary vs Standalone mode
	if hasPrimaryRepo {
		// Primary Mode: Use GetDefaultRefForRepo for discovery
		for i := range reposToSync {
			if reposToSync[i].ref == "" {
				log.Info("discovering ref for repo from go.mod",
					zap.String("repo", reposToSync[i].name),
				)
				ref, err := GetDefaultRefForRepo(log, currentRepo, reposToSync[i].name, goModPath)
				if err != nil {
					return stacktrace.Errorf("failed to get default ref for %s: %w", reposToSync[i].name, err)
				}
				reposToSync[i].ref = ref
			}
		}
	} else {
		// Standalone Mode: Use version discovery functions
		log.Info("standalone mode: using version discovery")

		// For avalanchego, use default branch (master)
		avalanchegoConfig, err := GetRepoConfig(repoAvalanchego)
		if err != nil {
			return stacktrace.Errorf("failed to get avalanchego config: %w", err)
		}

		for i := range reposToSync {
			if reposToSync[i].ref == "" {
				if reposToSync[i].name == repoAvalanchego {
					reposToSync[i].ref = avalanchegoConfig.DefaultBranch
					log.Info("using default branch for avalanchego",
						zap.String("ref", avalanchegoConfig.DefaultBranch),
					)
				}
			}
		}

		// Discover firewood version (after avalanchego is synced, it can be discovered from grafted coreth)
		for i := range reposToSync {
			if reposToSync[i].name == repoFirewood && reposToSync[i].ref == "" {
				explicitRef := ""
				if reposToSync[i].wasExplicit {
					explicitRef = reposToSync[i].ref
				}
				ref, err := DiscoverFirewoodVersion(log, baseDir, explicitRef)
				if err != nil {
					return stacktrace.Errorf("failed to discover firewood version: %w", err)
				}
				reposToSync[i].ref = ref
				log.Info("using discovered firewood version",
					zap.String("ref", ref),
				)
			}
		}
	}

	// Step 5: Execute sync loop
	syncedRepoNames := make([]string, 0, len(reposToSync))
	for _, repo := range reposToSync {
		log.Info("syncing repository",
			zap.String("repo", repo.name),
			zap.String("ref", repo.ref),
			zap.Bool("explicit", repo.wasExplicit),
		)

		config, err := GetRepoConfig(repo.name)
		if err != nil {
			return err
		}

		log.Info("repository configuration",
			zap.String("repo", repo.name),
			zap.String("gitRepo", config.GitRepo),
			zap.String("defaultBranch", config.DefaultBranch),
			zap.String("goModule", config.GoModule),
			zap.Bool("requiresNixBuild", config.RequiresNixBuild),
		)

		clonePath := GetRepoClonePath(repo.name, baseDir)

		// Use specified ref or default branch
		refToUse := repo.ref
		if refToUse == "" {
			refToUse = config.DefaultBranch
		}

		log.Info("cloning or updating repository",
			zap.String("repo", repo.name),
			zap.String("ref", refToUse),
			zap.Int("depth", depth),
			zap.String("url", config.GitRepo),
			zap.String("path", clonePath),
		)

		// Clone or update the repo
		err = CloneOrUpdateRepo(log, config.GitRepo, clonePath, refToUse, depth, force)
		if err != nil {
			return stacktrace.Errorf("failed to sync %s: %w", repo.name, err)
		}

		// Check if dirty (refuse unless forced)
		if !force {
			isDirty, err := IsRepoDirty(log, clonePath)
			if err == nil && isDirty {
				return ErrDirtyWorkingDir(clonePath)
			}
		}

		// Run nix build if required
		if config.RequiresNixBuild {
			log.Info("building repository",
				zap.String("repo", repo.name),
				zap.String("path", clonePath),
			)

			// Use specialized build function for firewood
			if repo.name == repoFirewood {
				err = BuildFirewood(log, clonePath, refToUse)
				if err != nil {
					return stacktrace.Errorf("failed to build firewood: %w", err)
				}
			} else {
				// Default nix build for other repos
				nixBuildPath := GetNixBuildPath(clonePath, config.NixBuildPath)
				err = RunNixBuild(log, nixBuildPath)
				if err != nil {
					return stacktrace.Errorf("failed to run nix build for %s: %w", repo.name, err)
				}
			}
		}

		log.Info("successfully synced repository",
			zap.String("repo", repo.name),
			zap.String("path", clonePath),
			zap.String("ref", refToUse),
		)

		syncedRepoNames = append(syncedRepoNames, repo.name)
	}

	// Build firewood if it's the primary repo (not synced)
	// This ensures firewood is built before replace directives are updated
	if currentRepo == repoFirewood {
		log.Info("building primary repository (firewood)",
			zap.String("path", baseDir),
		)
		err = BuildFirewood(log, baseDir, "HEAD")
		if err != nil {
			return stacktrace.Errorf("failed to build primary firewood: %w", err)
		}
	}

	// Step 6: Update replace directives (Primary mode only)
	if hasPrimaryRepo {
		log.Info("updating replace directives for all repos")
		err = UpdateAllReplaceDirectives(log, baseDir, syncedRepoNames)
		if err != nil {
			return stacktrace.Errorf("failed to update replace directives: %w", err)
		}
	} else {
		log.Info("skipping replace directives (standalone mode)")
	}

	log.Info("sync completed successfully",
		zap.Int("syncedCount", len(syncedRepoNames)),
		zap.Strings("syncedRepos", syncedRepoNames),
	)

	return nil
}

// Copyright (C) 2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package dep

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/storage/memory"
	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/tools/depctl/stacktrace"
	"github.com/ava-labs/avalanchego/utils/logging"
	"golang.org/x/mod/modfile"
)

// CloneOptions contains options for the Clone operation
type CloneOptions struct {
	Target         RepoTarget
	Path           string
	Version        string
	Shallow        bool
	UpdateExisting bool
	Logger         logging.Logger
}

// CloneResult contains information about a completed clone operation
type CloneResult struct {
	VersionInfo VersionInfo
	Path        string
}

// Clone clones or updates a repository and sets up go mod replace directives
func Clone(opts CloneOptions) (CloneResult, error) {
	// Ensure logger is set
	if opts.Logger == nil {
		opts.Logger = logging.NoLog{}
	}

	// Default path to repo name if not specified
	if opts.Path == "" {
		opts.Path = string(opts.Target)
		opts.Logger.Debug("Using default path",
			zap.String("path", opts.Path),
		)
	}

	// If no version provided, get it from go.mod
	version := opts.Version
	var versionInfo VersionInfo
	if version == "" {
		opts.Logger.Debug("Getting version from go.mod",
			zap.String("target", string(opts.Target)),
		)
		var err error
		versionInfo, err = GetVersion(opts.Target)
		if err != nil {
			return CloneResult{}, stacktrace.Errorf("failed to get version from go.mod: %w", err)
		}
		version = versionInfo.Version
		opts.Logger.Debug("Got version from go.mod",
			zap.String("version", version),
			zap.Bool("isDefault", versionInfo.IsDefault),
		)
	} else {
		// Version was explicitly provided
		versionInfo = VersionInfo{Version: version, IsDefault: false}
		opts.Logger.Debug("Using explicitly provided version",
			zap.String("version", version),
		)
	}

	// Resolve target to get clone URL
	_, cloneURL, err := opts.Target.Resolve()
	if err != nil {
		return CloneResult{}, stacktrace.Wrap(err)
	}
	opts.Logger.Debug("Resolved clone URL",
		zap.String("url", cloneURL),
	)

	// Convert path to absolute for consistency
	absPath, err := filepath.Abs(opts.Path)
	if err != nil {
		return CloneResult{}, stacktrace.Errorf("failed to resolve absolute path: %w", err)
	}
	opts.Logger.Debug("Resolved absolute path",
		zap.String("relativePath", opts.Path),
		zap.String("absolutePath", absPath),
	)

	// Clone or update the repository
	if err := cloneOrUpdate(absPath, cloneURL, version, opts.Shallow, opts.UpdateExisting, opts.Logger); err != nil {
		return CloneResult{}, stacktrace.Wrap(err)
	}

	// Set up go mod replace directives
	if err := setupModReplace(opts.Target, absPath, opts.Logger); err != nil {
		return CloneResult{}, stacktrace.Wrap(err)
	}

	return CloneResult{
		VersionInfo: versionInfo,
		Path:        opts.Path,
	}, nil
}

// cloneOrUpdate clones a repository if it doesn't exist, or updates it if it does
func cloneOrUpdate(path, cloneURL, version string, shallow, updateExisting bool, log logging.Logger) error {
	var repo *git.Repository
	var err error

	// Check if directory exists and is already a git repository
	if _, statErr := os.Stat(path); statErr == nil {
		// Directory exists - try to open as git repo
		repo, err = git.PlainOpen(path)
		if err != nil {
			return stacktrace.Errorf("directory exists but is not a git repository: %w", err)
		}

		// Check if we're already at the correct version
		if !updateExisting {
			head, err := repo.Head()
			if err == nil {
				// Try to resolve the expected version
				expectedHash, err := repo.ResolveRevision(plumbing.Revision(version))
				if err == nil && head.Hash() == *expectedHash {
					log.Info("Already at requested version",
						zap.String("version", version),
						zap.String("hash", head.Hash().String()[:8]),
					)
					return nil
				}
			}
			log.Info("Clone directory already exists, skipping (use --update-existing to update)",
				zap.String("path", path),
			)
			return nil
		}

		log.Debug("Directory already exists, will update",
			zap.String("path", path),
		)

		// Fetch updates
		log.Debug("Fetching updates from remote")
		err = repo.Fetch(&git.FetchOptions{
			RemoteName: "origin",
			Tags:       git.AllTags,
		})
		if err != nil && err != git.NoErrAlreadyUpToDate {
			log.Debug("Fetch completed with status", zap.Error(err))
		}
	} else if os.IsNotExist(statErr) {
		// Directory doesn't exist - need to clone
		log.Debug("Directory does not exist, cloning repository",
			zap.String("path", path),
		)

		// List remote refs to determine optimal clone strategy
		log.Debug("Listing remote references",
			zap.String("url", cloneURL),
			zap.String("version", version),
		)

		// Create a temporary remote to list refs
		rem := git.NewRemote(memory.NewStorage(), &config.RemoteConfig{
			Name: "origin",
			URLs: []string{cloneURL},
		})

		remoteRefs, err := rem.List(&git.ListOptions{})
		if err != nil {
			return stacktrace.Errorf("failed to list remote refs: %w", err)
		}

		// Determine what type of ref the version is
		refToClone, refType := analyzeVersion(version, remoteRefs, log)

		// Set up clone options
		cloneOpts := &git.CloneOptions{
			URL:      cloneURL,
			Progress: os.Stdout,
			Tags:     git.NoTags, // Fetch tags separately if needed
		}

		if shallow {
			cloneOpts.Depth = 1

			switch refType {
			case refTypeBranch:
				// Shallow clone specific branch
				cloneOpts.SingleBranch = true
				cloneOpts.ReferenceName = plumbing.NewBranchReferenceName(refToClone)
				log.Debug("Shallow cloning branch",
					zap.String("branch", refToClone),
				)

			case refTypeTag:
				// Shallow clone specific tag
				cloneOpts.SingleBranch = true
				cloneOpts.ReferenceName = plumbing.NewTagReferenceName(refToClone)
				log.Debug("Shallow cloning tag",
					zap.String("tag", refToClone),
				)

			case refTypeSHA:
				// For SHAs: clone default branch shallow, then fetch to find SHA
				cloneOpts.SingleBranch = true
				log.Debug("Shallow cloning default branch for SHA checkout",
					zap.String("sha", version),
				)

			case refTypeUnknown:
				// Version not found in remote refs - try full clone
				cloneOpts.Depth = 0
				cloneOpts.Tags = git.AllTags
				log.Debug("Version not found in remote refs, attempting full clone",
					zap.String("version", version),
				)
			}
		} else {
			// Full clone requested
			cloneOpts.Tags = git.AllTags
		}

		log.Debug("Cloning repository",
			zap.String("url", cloneURL),
			zap.String("path", path),
			zap.Bool("shallow", shallow),
			zap.String("refType", string(refType)),
		)

		repo, err = git.PlainClone(path, false, cloneOpts)
		if err != nil {
			return stacktrace.Errorf("failed to clone repository: %w", err)
		}

		// For SHA references, we may need to fetch more to find the specific commit
		if refType == refTypeSHA {
			log.Debug("Fetching to locate SHA commit",
				zap.String("sha", version),
			)

			// Try to resolve the SHA first
			_, resolveErr := repo.ResolveRevision(plumbing.Revision(version))
			if resolveErr != nil {
				// SHA not found in shallow clone, need to fetch more
				log.Debug("SHA not in shallow clone, fetching full history")
				err = repo.Fetch(&git.FetchOptions{
					RemoteName: "origin",
					RefSpecs: []config.RefSpec{
						config.RefSpec("+refs/heads/*:refs/remotes/origin/*"),
					},
					Depth: 0, // Full fetch to find the SHA
					Tags:  git.AllTags,
				})
				if err != nil && err != git.NoErrAlreadyUpToDate {
					return stacktrace.Errorf("failed to fetch for SHA: %w", err)
				}
			}
		}
	} else {
		return stacktrace.Errorf("failed to check directory: %w", statErr)
	}

	// Resolve version to commit hash
	hash, err := repo.ResolveRevision(plumbing.Revision(version))
	if err != nil {
		return stacktrace.Errorf("failed to resolve version %s: %w", version, err)
	}

	log.Debug("Resolved version",
		zap.String("version", version),
		zap.String("hash", hash.String()),
	)

	// Get worktree and checkout
	worktree, err := repo.Worktree()
	if err != nil {
		return stacktrace.Errorf("failed to get worktree: %w", err)
	}

	branchName := fmt.Sprintf("local/%s", version)
	branchRef := plumbing.NewBranchReferenceName(branchName)

	log.Debug("Checking out version",
		zap.String("hash", hash.String()),
		zap.String("branch", branchName),
	)

	err = worktree.Checkout(&git.CheckoutOptions{
		Hash:   *hash,
		Branch: branchRef,
		Create: true,
		Force:  true,
	})
	if err != nil {
		return stacktrace.Errorf("failed to checkout version %s: %w", version, err)
	}

	// Verify that we're at the correct version
	if err := verifyCloneVersion(repo, version, log); err != nil {
		return stacktrace.Wrap(err)
	}

	// Success message with full hash for verification
	log.Info("Successfully checked out",
		zap.String("version", version),
		zap.String("hash", hash.String()),
	)

	return nil
}

// refType represents the type of git reference
type refType string

const (
	refTypeBranch  refType = "branch"
	refTypeTag     refType = "tag"
	refTypeSHA     refType = "sha"
	refTypeUnknown refType = "unknown"
)

// analyzeVersion determines what type of reference the version string represents
func analyzeVersion(version string, refs []*plumbing.Reference, log logging.Logger) (string, refType) {
	// Check if version matches a branch
	for _, ref := range refs {
		if ref.Name().IsBranch() {
			branchName := ref.Name().Short()
			if branchName == version {
				log.Debug("Version matches branch", zap.String("branch", branchName))
				return branchName, refTypeBranch
			}
		}
	}

	// Check if version matches a tag
	for _, ref := range refs {
		if ref.Name().IsTag() {
			tagName := ref.Name().Short()
			if tagName == version {
				log.Debug("Version matches tag", zap.String("tag", tagName))
				return tagName, refTypeTag
			}
		}
	}

	// Check if version looks like a SHA (hex string, 7-40 chars)
	if matched, _ := regexp.MatchString(`^[0-9a-f]{7,40}$`, version); matched {
		log.Debug("Version appears to be SHA", zap.String("sha", version))
		return version, refTypeSHA
	}

	// Check if version matches a SHA prefix
	for _, ref := range refs {
		if strings.HasPrefix(ref.Hash().String(), version) {
			log.Debug("Version matches SHA prefix",
				zap.String("prefix", version),
				zap.String("full_sha", ref.Hash().String()),
			)
			return version, refTypeSHA
		}
	}

	log.Debug("Version type unknown", zap.String("version", version))
	return version, refTypeUnknown
}

// verifyCloneVersion verifies that the current HEAD matches the expected version
func verifyCloneVersion(repo *git.Repository, version string, log logging.Logger) error {
	// Get current HEAD
	head, err := repo.Head()
	if err != nil {
		return stacktrace.Errorf("failed to get HEAD: %w", err)
	}
	headHash := head.Hash()

	// Resolve expected version
	expectedHash, err := repo.ResolveRevision(plumbing.Revision(version))
	if err != nil {
		return stacktrace.Errorf("failed to resolve version %s: %w", version, err)
	}

	log.Debug("Verifying clone version",
		zap.String("headHash", headHash.String()),
		zap.String("expectedHash", expectedHash.String()),
	)

	// Compare hashes
	if headHash != *expectedHash {
		return stacktrace.Errorf("verification failed: HEAD %s does not match version %s (%s)",
			headHash.String(), version, expectedHash.String())
	}

	return nil
}

// setupModReplace sets up go mod replace directives based on current module and clone target
func setupModReplace(target RepoTarget, clonedPath string, log logging.Logger) error {
	// Get current working directory (where we started)
	originalDir, err := os.Getwd()
	if err != nil {
		return stacktrace.Errorf("failed to get working directory: %w", err)
	}

	log.Debug("Setting up go mod replace directives",
		zap.String("originalDir", originalDir),
		zap.String("clonedPath", clonedPath),
	)

	// Get current module name
	currentModule, err := getCurrentModuleName()
	if err != nil {
		return stacktrace.Wrap(err)
	}
	log.Debug("Current module",
		zap.String("module", currentModule),
	)

	// Determine which go.mod to modify and what replacements to make
	var modDir string
	var replacements []replacement

	switch {
	case strings.HasPrefix(currentModule, "github.com/ava-labs/coreth") && target == TargetAvalanchego:
		// coreth cloning avalanchego: replace coreth in avalanchego's go.mod
		modDir = clonedPath
		relPath, err := filepath.Rel(clonedPath, originalDir)
		if err != nil {
			return stacktrace.Errorf("failed to compute relative path: %w", err)
		}
		replacements = []replacement{
			{old: "github.com/ava-labs/coreth", new: relPath},
		}

	case strings.HasPrefix(currentModule, "github.com/ava-labs/firewood-go-ethhash/ffi") && target == TargetAvalanchego:
		// firewood cloning avalanchego: replace firewood in avalanchego's go.mod
		modDir = clonedPath
		relPath, err := filepath.Rel(clonedPath, originalDir)
		if err != nil {
			return stacktrace.Errorf("failed to compute relative path: %w", err)
		}
		replacements = []replacement{
			{old: "github.com/ava-labs/firewood-go-ethhash/ffi", new: relPath},
		}

	case currentModule == "github.com/ava-labs/avalanchego" && target == TargetFirewood:
		// avalanchego cloning firewood: replace firewood in current go.mod
		modDir = originalDir
		// Firewood module is in /ffi subdirectory
		ffiPath := filepath.Join(clonedPath, "ffi")
		relPath, err := filepath.Rel(originalDir, ffiPath)
		if err != nil {
			return stacktrace.Errorf("failed to compute relative path: %w", err)
		}
		replacements = []replacement{
			{old: "github.com/ava-labs/firewood-go-ethhash/ffi", new: relPath},
		}

	case currentModule == "github.com/ava-labs/avalanchego" && target == TargetCoreth:
		// avalanchego cloning coreth: replace coreth in current go.mod
		modDir = originalDir
		relPath, err := filepath.Rel(originalDir, clonedPath)
		if err != nil {
			return stacktrace.Errorf("failed to compute relative path: %w", err)
		}
		replacements = []replacement{
			{old: "github.com/ava-labs/coreth", new: relPath},
		}

	default:
		// No replacement needed for this combination
		log.Debug("No go mod replace needed for this module/target combination")
		return nil
	}

	log.Debug("Determined replacements",
		zap.String("modDir", modDir),
		zap.Int("replacementCount", len(replacements)),
	)

	// Apply replacements
	defer func() {
		_ = os.Chdir(originalDir)
	}()

	log.Debug("Changing to mod directory",
		zap.String("modDir", modDir),
	)
	if err := os.Chdir(modDir); err != nil {
		return stacktrace.Errorf("failed to change to %s: %w", modDir, err)
	}

	for _, repl := range replacements {
		// Ensure relative paths start with ./ for go mod edit
		replacePath := repl.new
		if !filepath.IsAbs(replacePath) && !strings.HasPrefix(replacePath, ".") {
			replacePath = "./" + replacePath
		}
		log.Debug("Applying go mod replace",
			zap.String("old", repl.old),
			zap.String("new", replacePath),
		)
		cmd := exec.Command("go", "mod", "edit", fmt.Sprintf("-replace=%s=%s", repl.old, replacePath))
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			return stacktrace.Errorf("failed to run go mod edit: %w", err)
		}
	}

	// Run go mod tidy
	cmd := exec.Command("go", "mod", "tidy")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		// Don't fail on go mod tidy errors - the replace might point to a path
		// that doesn't have all dependencies yet (e.g., in test scenarios)
		log.Warn("go mod tidy failed (this may be expected)",
			zap.Error(err),
		)
	}

	return nil
}

// replacement represents a go mod replace directive
type replacement struct {
	old string
	new string
}

// getCurrentModuleName reads the module name from go.mod in the current directory
func getCurrentModuleName() (string, error) {
	goModData, err := os.ReadFile("go.mod")
	if err != nil {
		return "", stacktrace.Errorf("failed to read go.mod: %w", err)
	}

	modFile, err := modfile.Parse("go.mod", goModData, nil)
	if err != nil {
		return "", stacktrace.Errorf("failed to parse go.mod: %w", err)
	}

	if modFile.Module == nil {
		return "", stacktrace.Errorf("no module declaration found in go.mod")
	}

	return modFile.Module.Mod.Path, nil
}

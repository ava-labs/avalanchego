// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package core

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/ava-labs/avalanchego/tests/fixture/stacktrace"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"go.uber.org/zap"
)

// ParseRepoAndVersion parses a string like "repo@version" and returns the repo name and version
func ParseRepoAndVersion(input string) (string, string, error) {
	if input == "" {
		return "", "", stacktrace.Errorf("empty input")
	}

	parts := strings.Split(input, "@")
	if len(parts) > 2 {
		return "", "", stacktrace.Errorf("invalid format: multiple @ symbols")
	}

	repo := parts[0]
	version := ""
	if len(parts) == 2 {
		version = parts[1]
	}

	return repo, version, nil
}

// GetRepoClonePath returns the path where a repo should be cloned
func GetRepoClonePath(repoName, baseDir string) string {
	return filepath.Join(baseDir, repoName)
}

// CloneRepo clones a repository to the specified path
// depth: 0 for full clone, 1 for shallow clone (default), >1 for partial clone
// ref can be a branch name, tag, or commit SHA. For SHAs, the repository is cloned
// without SingleBranch and the ref is checked out after cloning.
func CloneRepo(log logging.Logger, url, path, ref string, depth int) error {
	log.Debug("entering CloneRepo",
		zap.String("url", url),
		zap.String("path", path),
		zap.String("ref", ref),
		zap.Int("depth", depth),
	)

	opts := &git.CloneOptions{
		URL: url,
	}

	// depth of 0 means full clone, otherwise set depth
	if depth > 0 {
		opts.Depth = depth
		log.Debug("setting clone depth",
			zap.Int("depth", depth),
		)
	} else {
		log.Debug("performing full clone (depth=0)")
	}

	// If ref is specified and looks like a branch or tag (not a SHA),
	// set it as the reference to clone. Otherwise, we'll checkout after cloning.
	isSHA := looksLikeSHA(ref)
	log.Debug("checked if ref is SHA",
		zap.String("ref", ref),
		zap.Bool("isSHA", isSHA),
	)

	if ref != "" && !isSHA {
		opts.ReferenceName = plumbing.NewBranchReferenceName(ref)
		opts.SingleBranch = true
		log.Debug("configuring git clone for branch/tag",
			zap.String("referenceName", ref),
			zap.Bool("singleBranch", true),
		)
	} else {
		log.Debug("configuring git clone without single branch restriction")
	}

	log.Debug("executing git clone")
	_, err := git.PlainClone(path, false, opts)
	if err != nil {
		return stacktrace.Errorf("failed to clone repository: %w", err)
	}

	log.Debug("git clone completed successfully",
		zap.String("path", path),
	)

	return nil
}

// looksLikeSHA returns true if the ref looks like a git commit SHA
func looksLikeSHA(ref string) bool {
	// Git SHAs are 40 character hex strings (or 7+ for short SHAs)
	// Check if it's at least 7 chars and all hex
	if len(ref) < 7 {
		return false
	}
	for _, c := range ref {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
			return false
		}
	}
	return true
}

// CheckoutRef checks out a specific ref (tag, branch, or commit) in a repository
func CheckoutRef(log logging.Logger, repoPath, ref string) error {
	log.Debug("entering CheckoutRef",
		zap.String("repoPath", repoPath),
		zap.String("ref", ref),
	)

	repo, err := git.PlainOpen(repoPath)
	if err != nil {
		return stacktrace.Errorf("failed to open repository: %w", err)
	}

	w, err := repo.Worktree()
	if err != nil {
		return stacktrace.Errorf("failed to get worktree: %w", err)
	}

	// Try to resolve the ref
	log.Debug("resolving revision",
		zap.String("ref", ref),
	)
	hash, err := repo.ResolveRevision(plumbing.Revision(ref))
	if err != nil {
		return stacktrace.Errorf("failed to resolve ref %s: %w", ref, err)
	}

	log.Debug("resolved revision to hash",
		zap.String("ref", ref),
		zap.String("hash", hash.String()),
	)

	log.Debug("executing git checkout",
		zap.String("hash", hash.String()),
	)
	err = w.Checkout(&git.CheckoutOptions{
		Hash: *hash,
	})
	if err != nil {
		return stacktrace.Errorf("failed to checkout ref %s: %w", ref, err)
	}

	log.Debug("checkout completed successfully")

	return nil
}

// GetCurrentRef returns the current ref (branch or commit) of a repository
func GetCurrentRef(log logging.Logger, repoPath string) (string, error) {
	log.Debug("getting current ref",
		zap.String("repoPath", repoPath),
	)

	repo, err := git.PlainOpen(repoPath)
	if err != nil {
		return "", stacktrace.Errorf("failed to open repository: %w", err)
	}

	head, err := repo.Head()
	if err != nil {
		return "", stacktrace.Errorf("failed to get HEAD: %w", err)
	}

	// If HEAD is a branch, return the branch name
	if head.Name().IsBranch() {
		branchName := head.Name().Short()
		log.Debug("HEAD is branch reference",
			zap.Bool("isBranch", true),
			zap.String("branchName", branchName),
		)
		return branchName, nil
	}

	// Otherwise return the commit hash (detached HEAD)
	commitHash := head.Hash().String()
	log.Debug("HEAD is detached (not a branch)",
		zap.String("commitHash", commitHash),
	)
	return commitHash, nil
}

// IsRepoDirty checks if a repository has uncommitted changes
func IsRepoDirty(log logging.Logger, repoPath string) (bool, error) {
	log.Debug("checking if repository is dirty",
		zap.String("repoPath", repoPath),
	)

	repo, err := git.PlainOpen(repoPath)
	if err != nil {
		return false, stacktrace.Errorf("failed to open repository: %w", err)
	}

	w, err := repo.Worktree()
	if err != nil {
		return false, stacktrace.Errorf("failed to get worktree: %w", err)
	}

	status, err := w.Status()
	if err != nil {
		return false, stacktrace.Errorf("failed to get status: %w", err)
	}

	isDirty := !status.IsClean()
	log.Debug("repository dirty check complete",
		zap.Bool("isDirty", isDirty),
		zap.Int("modifiedFiles", len(status)),
	)

	return isDirty, nil
}

// CloneOrUpdateRepo clones a repository or updates an existing one
// Handles the case where a SHA is requested with shallow clone by falling back to full clone
func CloneOrUpdateRepo(log logging.Logger, url, path, ref string, depth int, force bool) error {
	log.Debug("entering CloneOrUpdateRepo",
		zap.String("url", url),
		zap.String("path", path),
		zap.String("ref", ref),
		zap.Int("depth", depth),
		zap.Bool("force", force),
	)

	// Check if repo already exists
	_, err := git.PlainOpen(path)
	repoExists := err == nil
	log.Debug("checked if repository exists",
		zap.String("path", path),
		zap.Bool("exists", repoExists),
	)

	if repoExists {
		log.Debug("repository exists, checking force flag",
			zap.Bool("force", force),
		)
		if !force {
			log.Debug("returning error: repo exists and force=false")
			return ErrRepoAlreadyExists(filepath.Base(path), path)
		}
		log.Debug("proceeding to update existing repository")
		// Update existing repo
		return UpdateRepo(log, path, ref)
	}

	log.Debug("repository does not exist, proceeding to clone")

	// Repo doesn't exist, clone it
	// First try with the requested depth
	log.Debug("attempting clone with specified depth",
		zap.Int("depth", depth),
	)
	err = CloneRepo(log, url, path, ref, depth)
	if err != nil {
		log.Debug("clone failed, checking if should retry with full clone",
			zap.Int("requestedDepth", depth),
			zap.String("ref", ref),
			zap.Error(err),
		)

		// If shallow clone fails and we're trying to clone a specific ref,
		// it might be a SHA that's not in the shallow history
		// Try a full clone instead
		if depth > 0 && ref != "" {
			log.Debug("retrying with full clone (depth=0)")
			// This will be tested in integration tests since it requires
			// actual git operations with SHA resolution
			return CloneRepo(log, url, path, ref, 0)
		}
		return err
	}

	// If we cloned successfully and the ref looks like a SHA, we need to checkout
	// (since CloneRepo doesn't set branch reference for SHAs)
	if ref != "" && looksLikeSHA(ref) {
		log.Debug("ref is SHA, attempting checkout",
			zap.String("ref", ref),
		)
		err = CheckoutRef(log, path, ref)
		if err != nil && depth > 0 {
			log.Debug("checkout failed with shallow clone, retrying with full clone",
				zap.Error(err),
			)
			// Checkout failed, likely because the SHA isn't in shallow history
			// Remove the shallow clone and retry with full clone
			os.RemoveAll(path)
			err = CloneRepo(log, url, path, ref, 0)
			if err != nil {
				return err
			}
			return CheckoutRef(log, path, ref)
		}
		return err
	}

	log.Debug("clone completed successfully")
	return nil
}

// UpdateRepo updates an existing repository to a specific ref
func UpdateRepo(log logging.Logger, repoPath, ref string) error {
	log.Debug("entering UpdateRepo",
		zap.String("repoPath", repoPath),
		zap.String("ref", ref),
	)

	repo, err := git.PlainOpen(repoPath)
	if err != nil {
		return stacktrace.Errorf("failed to open repository: %w", err)
	}

	// Fetch latest changes
	log.Debug("fetching latest changes from remote")
	err = repo.Fetch(&git.FetchOptions{
		RefSpecs: []config.RefSpec{"refs/*:refs/*"},
	})
	if err != nil && err != git.NoErrAlreadyUpToDate {
		return stacktrace.Errorf("failed to fetch: %w", err)
	}

	if err == git.NoErrAlreadyUpToDate {
		log.Debug("repository already up to date")
	} else {
		log.Debug("fetch completed successfully")
	}

	// Checkout the ref
	log.Debug("checking out ref after fetch",
		zap.String("ref", ref),
	)
	return CheckoutRef(log, repoPath, ref)
}

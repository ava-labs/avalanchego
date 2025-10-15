// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package core

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/google/go-github/v68/github"
	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/tests/fixture/stacktrace"
	"github.com/ava-labs/avalanchego/utils/logging"
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
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') && (c < 'A' || c > 'F') {
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
		return UpdateRepo(log, url, path, ref, force)
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

// parseGitHubURL extracts owner and repo from a GitHub URL
// Returns empty strings if not a GitHub URL
func parseGitHubURL(url string) (owner, repo string) {
	// Match patterns like:
	// https://github.com/owner/repo
	// https://github.com/owner/repo.git
	// git@github.com:owner/repo.git
	patterns := []string{
		`github\.com[:/]([^/]+)/([^/\.]+)`,
	}

	for _, pattern := range patterns {
		re := regexp.MustCompile(pattern)
		matches := re.FindStringSubmatch(url)
		if len(matches) == 3 {
			return matches[1], matches[2]
		}
	}
	return "", ""
}

// findGitHubBranchForSHA queries GitHub API to find which branch has the SHA as HEAD
// Returns empty string if not found or if there's an error
func findGitHubBranchForSHA(log logging.Logger, url, sha string) string {
	owner, repo := parseGitHubURL(url)
	if owner == "" || repo == "" {
		log.Debug("not a GitHub URL, skipping branch lookup",
			zap.String("url", url),
		)
		return ""
	}

	log.Debug("querying GitHub API for branch containing SHA",
		zap.String("owner", owner),
		zap.String("repo", repo),
		zap.String("sha", sha),
	)

	client := github.NewClient(nil)
	ctx := context.Background()

	branches, _, err := client.Repositories.ListBranchesHeadCommit(ctx, owner, repo, sha)
	if err != nil {
		log.Debug("failed to query GitHub API",
			zap.Error(err),
		)
		return ""
	}

	if len(branches) == 0 {
		log.Debug("no branches found with SHA as HEAD")
		return ""
	}

	// Return the first branch found
	branchName := branches[0].GetName()
	log.Debug("found branch for SHA",
		zap.String("branch", branchName),
	)
	return branchName
}

// UpdateRepo updates an existing repository to a specific ref
// If force is true and trying to update a shallow clone to a SHA, it will
// proactively remove the repo and re-clone it with full depth to avoid issues
// with incomplete history (which can cause problems for tools like nix).
func UpdateRepo(log logging.Logger, url, repoPath, ref string, force bool) error {
	log.Debug("entering UpdateRepo",
		zap.String("url", url),
		zap.String("repoPath", repoPath),
		zap.String("ref", ref),
		zap.Bool("force", force),
	)

	repo, err := git.PlainOpen(repoPath)
	if err != nil {
		return stacktrace.Errorf("failed to open repository: %w", err)
	}

	// Check if this is a shallow clone
	isShallow := isShallowRepo(repoPath)
	log.Debug("checked if repository is shallow",
		zap.Bool("isShallow", isShallow),
	)

	// If we're updating a shallow clone to a SHA with --force, remove and re-clone
	// to avoid inconsistent shallow boundaries. The issue: shallow cloning main then
	// checking out a different SHA leaves the shallow file pointing to main's boundaries,
	// causing "object not found" errors when tools like nix try to access parent commits.
	if force && isShallow && looksLikeSHA(ref) {
		log.Info("shallow clone with SHA detected, removing and re-cloning at target SHA",
			zap.String("ref", ref),
			zap.String("path", repoPath),
		)
		os.RemoveAll(repoPath)

		// Try to find which branch has this SHA as HEAD (GitHub only)
		// This allows us to do a single-branch shallow clone (much smaller)
		branch := findGitHubBranchForSHA(log, url, ref)
		if branch != "" {
			log.Info("found branch for SHA, cloning with single-branch",
				zap.String("branch", branch),
			)
			// Clone the specific branch shallow, then checkout the SHA
			err = CloneRepo(log, url, repoPath, branch, 1)
			if err == nil {
				return CheckoutRef(log, repoPath, ref)
			}
			log.Info("single-branch clone failed, falling back to multi-branch",
				zap.Error(err),
			)
		}

		// Fallback: clone without single-branch (fetches all branches, larger but more reliable)
		err = CloneRepo(log, url, repoPath, ref, 1)
		if err != nil {
			// If shallow clone fails, fallback to full clone
			log.Info("shallow clone failed, retrying with full depth",
				zap.Error(err),
			)
			err = CloneRepo(log, url, repoPath, ref, 0)
			if err != nil {
				return err
			}
		}
		return CheckoutRef(log, repoPath, ref)
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

// isShallowRepo checks if a repository is a shallow clone
func isShallowRepo(repoPath string) bool {
	shallowFile := filepath.Join(repoPath, ".git", "shallow")
	_, err := os.Stat(shallowFile)
	return err == nil
}

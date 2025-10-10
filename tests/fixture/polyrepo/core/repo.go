// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package core

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
)

// ParseRepoAndVersion parses a string like "repo@version" and returns the repo name and version
func ParseRepoAndVersion(input string) (string, string, error) {
	if input == "" {
		return "", "", fmt.Errorf("empty input")
	}

	parts := strings.Split(input, "@")
	if len(parts) > 2 {
		return "", "", fmt.Errorf("invalid format: multiple @ symbols")
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
func CloneRepo(url, path, ref string, depth int) error {
	opts := &git.CloneOptions{
		URL: url,
	}

	// depth of 0 means full clone, otherwise set depth
	if depth > 0 {
		opts.Depth = depth
	}

	if ref != "" {
		opts.ReferenceName = plumbing.NewBranchReferenceName(ref)
		opts.SingleBranch = true
	}

	_, err := git.PlainClone(path, false, opts)
	if err != nil {
		return fmt.Errorf("failed to clone repository: %w", err)
	}

	return nil
}

// CheckoutRef checks out a specific ref (tag, branch, or commit) in a repository
func CheckoutRef(repoPath, ref string) error {
	repo, err := git.PlainOpen(repoPath)
	if err != nil {
		return fmt.Errorf("failed to open repository: %w", err)
	}

	w, err := repo.Worktree()
	if err != nil {
		return fmt.Errorf("failed to get worktree: %w", err)
	}

	// Try to resolve the ref
	hash, err := repo.ResolveRevision(plumbing.Revision(ref))
	if err != nil {
		return fmt.Errorf("failed to resolve ref %s: %w", ref, err)
	}

	err = w.Checkout(&git.CheckoutOptions{
		Hash: *hash,
	})
	if err != nil {
		return fmt.Errorf("failed to checkout ref %s: %w", ref, err)
	}

	return nil
}

// IsRepoDirty checks if a repository has uncommitted changes
func IsRepoDirty(repoPath string) (bool, error) {
	repo, err := git.PlainOpen(repoPath)
	if err != nil {
		return false, fmt.Errorf("failed to open repository: %w", err)
	}

	w, err := repo.Worktree()
	if err != nil {
		return false, fmt.Errorf("failed to get worktree: %w", err)
	}

	status, err := w.Status()
	if err != nil {
		return false, fmt.Errorf("failed to get status: %w", err)
	}

	return !status.IsClean(), nil
}

// CloneOrUpdateRepo clones a repository or updates an existing one
// Handles the case where a SHA is requested with shallow clone by falling back to full clone
func CloneOrUpdateRepo(url, path, ref string, depth int, force bool) error {
	// Check if repo already exists
	_, err := git.PlainOpen(path)
	if err == nil {
		// Repo exists
		if !force {
			return ErrRepoAlreadyExists(filepath.Base(path), path)
		}
		// Update existing repo
		return UpdateRepo(path, ref)
	}

	// Repo doesn't exist, clone it
	// First try with the requested depth
	err = CloneRepo(url, path, ref, depth)
	if err != nil {
		// If shallow clone fails and we're trying to clone a specific ref,
		// it might be a SHA that's not in the shallow history
		// Try a full clone instead
		if depth > 0 && ref != "" {
			// This will be tested in integration tests since it requires
			// actual git operations with SHA resolution
			return CloneRepo(url, path, ref, 0)
		}
		return err
	}

	// If we cloned successfully but need to checkout a specific ref
	if ref != "" {
		return CheckoutRef(path, ref)
	}

	return nil
}

// UpdateRepo updates an existing repository to a specific ref
func UpdateRepo(repoPath, ref string) error {
	repo, err := git.PlainOpen(repoPath)
	if err != nil {
		return fmt.Errorf("failed to open repository: %w", err)
	}

	// Fetch latest changes
	err = repo.Fetch(&git.FetchOptions{
		RefSpecs: []config.RefSpec{"refs/*:refs/*"},
	})
	if err != nil && err != git.NoErrAlreadyUpToDate {
		return fmt.Errorf("failed to fetch: %w", err)
	}

	// Checkout the ref
	return CheckoutRef(repoPath, ref)
}

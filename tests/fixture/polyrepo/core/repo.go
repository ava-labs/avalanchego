// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package core

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/go-git/go-git/v5"
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
func CloneRepo(url, path, ref string, shallow bool) error {
	opts := &git.CloneOptions{
		URL: url,
	}

	if shallow {
		opts.Depth = 1
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

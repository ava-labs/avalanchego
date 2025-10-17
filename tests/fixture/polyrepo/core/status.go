// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package core

import (
	"fmt"

	"github.com/ava-labs/avalanchego/utils/logging"
)

// RepoStatus represents the status of a repository
type RepoStatus struct {
	Name         string
	Path         string
	Exists       bool
	CurrentRef   string
	CommitSHA    string   // 8-digit short SHA of current commit
	Tags         []string // tags pointing to current commit
	Branches     []string // branches pointing to current commit
	IsDirty      bool
	Replacements map[string]string // module path -> replacement path/version
}

// GetRepoStatus returns the status of a repository
// If isPrimary is true, the repo path is baseDir itself (we're currently in this repo)
// If isPrimary is false, the repo path is baseDir/repoName (synced repo)
func GetRepoStatus(log logging.Logger, repoName, baseDir, goModPath string, isPrimary bool) (*RepoStatus, error) {
	config, err := GetRepoConfig(repoName)
	if err != nil {
		return nil, err
	}

	// Determine the path to the repository
	var repoPath string
	if isPrimary {
		repoPath = baseDir
	} else {
		repoPath = GetRepoClonePath(repoName, baseDir)
	}

	status := &RepoStatus{
		Name:         repoName,
		Path:         repoPath,
		Exists:       false,
		Replacements: make(map[string]string),
	}

	// Check if repo exists
	isDirty, err := IsRepoDirty(log, status.Path)
	if err == nil {
		status.Exists = true
		status.IsDirty = isDirty

		// Get current ref
		currentRef, err := GetCurrentRef(log, status.Path)
		if err == nil {
			status.CurrentRef = currentRef
		}

		// Get commit SHA
		commitSHA, err := GetCommitSHA(log, status.Path)
		if err == nil {
			status.CommitSHA = commitSHA

			// Get tags for this commit
			tags, err := GetTagsForCommit(log, status.Path, commitSHA)
			if err == nil {
				status.Tags = tags
			}

			// Get branches for this commit
			branches, err := GetBranchesForCommit(log, status.Path, commitSHA)
			if err == nil {
				status.Branches = branches
			}
		}
	}

	// Get replace directives from THIS repo's go.mod (if it exists)
	var repoGoModPath string
	if isPrimary {
		// For primary repo, use the provided goModPath
		repoGoModPath = goModPath
	} else if config.GoModPath != "" {
		// For synced repos, construct path to their go.mod
		// Use config.GoModPath which is relative to repo root
		// e.g., "go.mod" for most repos, "ffi/go.mod" for firewood
		repoGoModPath = GetRepoClonePath(repoName, baseDir) + "/" + config.GoModPath
	}

	if repoGoModPath != "" {
		replacements, err := GetAllReplaceDirectives(log, repoGoModPath)
		if err == nil {
			status.Replacements = replacements
		}
	}

	return status, nil
}

// FormatRepoStatus formats a repo status for display
func FormatRepoStatus(status *RepoStatus) string {
	if !status.Exists {
		return status.Name + ": not cloned"
	}

	result := fmt.Sprintf("%s: %s", status.Name, status.Path)
	if status.CommitSHA != "" {
		result += fmt.Sprintf(" (%s)", status.CommitSHA)
	}

	// Add branches
	if len(status.Branches) > 0 {
		result += "\n    branches: " + joinStrings(status.Branches)
	} else {
		result += "\n    branches: none"
	}

	// Add tags
	if len(status.Tags) > 0 {
		result += "\n    tags: " + joinStrings(status.Tags)
	} else {
		result += "\n    tags: none"
	}

	// Add replacements
	if len(status.Replacements) > 0 {
		result += "\n    replacements:"
		for module, path := range status.Replacements {
			result += fmt.Sprintf("\n      %s: %s", module, path)
		}
	} else {
		result += "\n    replacements: none"
	}

	return result
}

// joinStrings joins a slice of strings with ", "
func joinStrings(strs []string) string {
	if len(strs) == 0 {
		return ""
	}
	result := strs[0]
	for i := 1; i < len(strs); i++ {
		result += ", " + strs[i]
	}
	return result
}

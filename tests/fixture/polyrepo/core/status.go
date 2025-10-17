// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package core

import (
	"fmt"

	"github.com/ava-labs/avalanchego/utils/logging"
)

// RepoStatus represents the status of a repository
type RepoStatus struct {
	Name        string
	Path        string
	Exists      bool
	CurrentRef  string
	IsDirty     bool
	HasReplace  bool
	ReplacePath string
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
		Name:   repoName,
		Path:   repoPath,
		Exists: false,
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
	}

	// Check for replace directive in go.mod
	if goModPath != "" {
		modFile, err := ReadGoMod(log, goModPath)
		if err == nil {
			for _, replace := range modFile.Replace {
				if replace.Old.Path == config.GoModule {
					status.HasReplace = true
					status.ReplacePath = replace.New.Path
					break
				}
			}
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
	if status.HasReplace {
		result += fmt.Sprintf(" (replace: %s)", status.ReplacePath)
	}
	return result
}

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
func GetRepoStatus(log logging.Logger, repoName, baseDir, goModPath string) (*RepoStatus, error) {
	config, err := GetRepoConfig(repoName)
	if err != nil {
		return nil, err
	}

	status := &RepoStatus{
		Name:   repoName,
		Path:   GetRepoClonePath(repoName, baseDir),
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

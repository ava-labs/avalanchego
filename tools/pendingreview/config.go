// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package pendingreview

import (
	"os"
	"path/filepath"
)

const (
	defaultRepo             = "ava-labs/avalanchego"
	defaultGitHubAPIBaseURL = "https://api.github.com"
)

func defaultConfigDir() string {
	if dir := os.Getenv("GH_PENDING_REVIEW_CONFIG_DIR"); dir != "" {
		return dir
	}
	if dir := os.Getenv("GH_DRAFT_REVIEW_CONFIG_DIR"); dir != "" {
		return dir
	}

	configHome := os.Getenv("XDG_CONFIG_HOME")
	if configHome == "" {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return ".config/gh-pending-review"
		}
		configHome = filepath.Join(homeDir, ".config")
	}
	return filepath.Join(configHome, "gh-pending-review")
}

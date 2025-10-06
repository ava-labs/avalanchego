// Copyright (C) 2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package dep

import "fmt"

// RepoTarget represents a supported repository target
type RepoTarget string

const (
	TargetAvalanchego RepoTarget = "avalanchego"
	TargetFirewood    RepoTarget = "firewood"
	TargetCoreth      RepoTarget = "coreth"
)

// repoInfo contains the resolved paths for a repository target
type repoInfo struct {
	modulePath string
	cloneURL   string
}

// Resolve returns the module path and clone URL for the repository target
func (r RepoTarget) Resolve() (modulePath, cloneURL string, err error) {
	info, ok := repoMap[r]
	if !ok {
		return "", "", fmt.Errorf("invalid repo target %q, must be one of: avalanchego, firewood, coreth", r)
	}
	return info.modulePath, info.cloneURL, nil
}

// repoMap maps repository targets to their module paths and clone URLs
var repoMap = map[RepoTarget]repoInfo{
	TargetAvalanchego: {
		modulePath: "github.com/ava-labs/avalanchego",
		cloneURL:   "https://github.com/ava-labs/avalanchego",
	},
	TargetFirewood: {
		modulePath: "github.com/ava-labs/firewood-go-ethhash/ffi",
		cloneURL:   "https://github.com/ava-labs/firewood",
	},
	TargetCoreth: {
		modulePath: "github.com/ava-labs/coreth",
		cloneURL:   "https://github.com/ava-labs/coreth",
	},
}

// ParseRepoTarget converts a string to a RepoTarget, defaulting to avalanchego if empty
func ParseRepoTarget(s string) RepoTarget {
	if s == "" {
		return TargetAvalanchego
	}
	return RepoTarget(s)
}

// IsValid checks if the RepoTarget is one of the supported targets
func (r RepoTarget) IsValid() bool {
	_, ok := repoMap[r]
	return ok
}

// String returns the string representation of the RepoTarget
func (r RepoTarget) String() string {
	return string(r)
}

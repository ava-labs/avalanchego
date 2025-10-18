// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package core

import "fmt"

// RepoConfig contains the configuration for a repository
type RepoConfig struct {
	Name                  string
	GoModule              string
	ModuleReplacementPath string
	GitRepo               string
	DefaultBranch         string
	GoModPath             string
	RequiresNixBuild      bool
	NixBuildPath          string
}

var (
	avalanchegoConfig = &RepoConfig{
		Name:                  "avalanchego",
		GoModule:              "github.com/ava-labs/avalanchego",
		ModuleReplacementPath: ".",
		GitRepo:               "https://github.com/ava-labs/avalanchego",
		DefaultBranch:         "master",
		GoModPath:             "go.mod",
		RequiresNixBuild:      false,
		NixBuildPath:          "",
	}

	corethConfig = &RepoConfig{
		Name:                  "coreth",
		GoModule:              "github.com/ava-labs/coreth",
		ModuleReplacementPath: ".",
		GitRepo:               "https://github.com/ava-labs/coreth",
		DefaultBranch:         "master",
		GoModPath:             "go.mod",
		RequiresNixBuild:      false,
		NixBuildPath:          "",
	}

	firewoodConfig = &RepoConfig{
		Name:                  "firewood",
		GoModule:              "github.com/ava-labs/firewood/ffi",
		ModuleReplacementPath: "./ffi/result/ffi",
		GitRepo:               "https://github.com/ava-labs/firewood",
		DefaultBranch:         "main",
		GoModPath:             "ffi/go.mod",
		RequiresNixBuild:      true,
		NixBuildPath:          "ffi",
	}

	allRepos = map[string]*RepoConfig{
		"avalanchego": avalanchegoConfig,
		"coreth":      corethConfig,
		"firewood":    firewoodConfig,
	}
)

// GetRepoConfig returns the configuration for a repository by name
func GetRepoConfig(name string) (*RepoConfig, error) {
	config, ok := allRepos[name]
	if !ok {
		return nil, fmt.Errorf("unknown repository: %s", name)
	}
	return config, nil
}

// GetAllRepoConfigs returns all repository configurations
func GetAllRepoConfigs() []*RepoConfig {
	return []*RepoConfig{avalanchegoConfig, corethConfig, firewoodConfig}
}

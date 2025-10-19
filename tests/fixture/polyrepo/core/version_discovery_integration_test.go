// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

//go:build integration
// +build integration

package core

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/stretchr/testify/require"
)

// TestDiscoverAvalanchegoCorethVersions_RealRepos is a smoke test that verifies
// the version discovery functions work with real cloned repositories.
// This test validates that:
// - Real repos can be cloned
// - Real go.mod files can be parsed
// - Pseudo-versions can be extracted from real dependencies
func TestDiscoverAvalanchegoCorethVersions_RealRepos(t *testing.T) {
	log := logging.NoLog{}
	tmpDir := t.TempDir()

	// Clone avalanchego at a known version
	avalanchegoPath := filepath.Join(tmpDir, "avalanchego")
	avalanchegoConfig, err := GetRepoConfig("avalanchego")
	require.NoError(t, err, "failed to get avalanchego config")

	avalanchegoRef := "v1.11.0"
	err = CloneRepo(log, avalanchegoConfig.GitRepo, avalanchegoPath, avalanchegoRef, 1)
	require.NoError(t, err, "failed to clone avalanchego")

	// Discover coreth version from real go.mod
	requestedRepos := []string{"avalanchego", "coreth"}
	explicitRefs := map[string]string{
		"avalanchego": avalanchegoRef,
	}

	versions, err := DiscoverAvalanchegoCorethVersions(log, tmpDir, requestedRepos, explicitRefs)
	require.NoError(t, err, "DiscoverAvalanchegoCorethVersions failed")
	require.Equal(t, avalanchegoRef, versions["avalanchego"])
	require.NotEmpty(t, versions["coreth"], "coreth version should be discovered from real go.mod")

	// Verify the discovered version matches what's in go.mod
	avalanchegoGoMod := filepath.Join(avalanchegoPath, "go.mod")
	corethConfig, err := GetRepoConfig("coreth")
	require.NoError(t, err)

	corethVersion, err := GetDependencyVersion(log, avalanchegoGoMod, corethConfig.GoModule)
	require.NoError(t, err, "should find coreth in real go.mod")

	expectedRef, err := ConvertVersionToGitRef(log, corethVersion)
	require.NoError(t, err)
	require.Equal(t, expectedRef, versions["coreth"])
}

// TestDiscoverFirewoodVersion_RealRepos is a smoke test that verifies
// firewood version discovery works with real cloned repositories.
// This test validates that:
// - Firewood can be discovered from coreth's real go.mod
// - The firewood module name (firewood-go-ethhash/ffi) is correctly handled
func TestDiscoverFirewoodVersion_RealRepos(t *testing.T) {
	log := logging.NoLog{}
	tmpDir := t.TempDir()

	// Clone coreth at default branch
	corethPath := filepath.Join(tmpDir, "coreth")
	corethConfig, err := GetRepoConfig("coreth")
	require.NoError(t, err)

	err = CloneRepo(log, corethConfig.GitRepo, corethPath, corethConfig.DefaultBranch, 1)
	require.NoError(t, err, "failed to clone coreth")

	// Discover firewood version
	requestedRepos := []string{"coreth", "firewood"}
	discoveredVersions := map[string]string{
		"coreth": corethConfig.DefaultBranch,
	}

	version, err := DiscoverFirewoodVersion(log, tmpDir, "", requestedRepos, discoveredVersions)
	require.NoError(t, err)
	require.NotEmpty(t, version, "firewood version should be discovered from real coreth go.mod")

	// Verify it matches what's in coreth's go.mod
	corethGoMod := filepath.Join(corethPath, "go.mod")
	firewoodConfig, err := GetRepoConfig("firewood")
	require.NoError(t, err)

	firewoodVersion, err := GetDependencyVersion(log, corethGoMod, firewoodConfig.GoModule)
	require.NoError(t, err, "should find firewood in real coreth go.mod")

	expectedRef, err := ConvertVersionToGitRef(log, firewoodVersion)
	require.NoError(t, err)
	require.Equal(t, expectedRef, version)
}

// TestDiscoverVersions_DefaultBranches_RealRepos verifies that the version
// discovery functions handle default branches correctly when no explicit
// versions are provided and repos need to be cloned.
func TestDiscoverVersions_DefaultBranches_RealRepos(t *testing.T) {
	log := logging.NoLog{}
	tmpDir := t.TempDir()

	// Request both without explicit refs - should default avalanchego to master
	// and discover coreth from its go.mod
	requestedRepos := []string{"avalanchego", "coreth"}
	explicitRefs := map[string]string{}

	versions, err := DiscoverAvalanchegoCorethVersions(log, tmpDir, requestedRepos, explicitRefs)
	require.NoError(t, err)

	avalanchegoConfig, err := GetRepoConfig("avalanchego")
	require.NoError(t, err)

	require.Equal(t, avalanchegoConfig.DefaultBranch, versions["avalanchego"])
	require.NotEmpty(t, versions["coreth"], "coreth should be discovered after cloning avalanchego")

	// Verify avalanchego was actually cloned
	avalanchegoPath := filepath.Join(tmpDir, "avalanchego")
	_, err = os.Stat(filepath.Join(avalanchegoPath, ".git"))
	require.NoError(t, err, "avalanchego should be cloned")
}

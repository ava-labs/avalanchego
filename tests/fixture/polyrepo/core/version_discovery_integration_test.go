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

// TestDiscoverAvalanchegoCorethVersions_BothExplicit tests that explicit versions are used when provided
func TestDiscoverAvalanchegoCorethVersions_BothExplicit(t *testing.T) {
	log := logging.NoLog{}
	tmpDir := t.TempDir()

	requestedRepos := []string{"avalanchego", "coreth"}
	explicitRefs := map[string]string{
		"avalanchego": "v1.11.0",
		"coreth":      "v0.13.8",
	}

	versions, err := DiscoverAvalanchegoCorethVersions(log, tmpDir, requestedRepos, explicitRefs)
	require.NoError(t, err, "DiscoverAvalanchegoCorethVersions failed")
	require.Equal(t, "v1.11.0", versions["avalanchego"], "expected avalanchego v1.11.0")
	require.Equal(t, "v0.13.8", versions["coreth"], "expected coreth v0.13.8")
}

// TestDiscoverAvalanchegoCorethVersions_AvalanchegoExplicit_CorethDiscovered tests discovering
// coreth version from avalanchego's go.mod when avalanchego version is explicit
func TestDiscoverAvalanchegoCorethVersions_AvalanchegoExplicit_CorethDiscovered(t *testing.T) {
	log := logging.NoLog{}
	tmpDir := t.TempDir()

	// Clone avalanchego at a specific version
	avalanchegoPath := filepath.Join(tmpDir, "avalanchego")
	avalanchegoConfig, err := GetRepoConfig("avalanchego")
	require.NoError(t, err, "failed to get avalanchego config")

	// Use a known version
	avalanchegoRef := "v1.11.0"
	err = CloneRepo(log, avalanchegoConfig.GitRepo, avalanchegoPath, avalanchegoRef, 1)
	require.NoError(t, err, "failed to clone avalanchego")

	requestedRepos := []string{"avalanchego", "coreth"}
	explicitRefs := map[string]string{
		"avalanchego": avalanchegoRef,
	}

	versions, err := DiscoverAvalanchegoCorethVersions(log, tmpDir, requestedRepos, explicitRefs)
	require.NoError(t, err, "DiscoverAvalanchegoCorethVersions failed")
	require.Equal(t, avalanchegoRef, versions["avalanchego"], "expected explicit avalanchego version")
	require.NotEmpty(t, versions["coreth"], "expected coreth version to be discovered")

	// Verify the discovered coreth version exists in avalanchego's go.mod
	avalanchegoGoMod := filepath.Join(avalanchegoPath, "go.mod")
	corethConfig, err := GetRepoConfig("coreth")
	require.NoError(t, err, "failed to get coreth config")

	corethVersionInGoMod, err := GetDependencyVersion(log, avalanchegoGoMod, corethConfig.GoModule)
	require.NoError(t, err, "failed to get coreth version from avalanchego go.mod")

	// Convert version to git ref for comparison
	corethGitRef, err := ConvertVersionToGitRef(log, corethVersionInGoMod)
	require.NoError(t, err, "failed to convert coreth version to git ref")
	require.Equal(t, corethGitRef, versions["coreth"], "discovered coreth version should match avalanchego's go.mod")
}

// TestDiscoverAvalanchegoCorethVersions_CorethExplicit_AvalanchegoDiscovered tests discovering
// avalanchego version from coreth's go.mod when coreth version is explicit
func TestDiscoverAvalanchegoCorethVersions_CorethExplicit_AvalanchegoDiscovered(t *testing.T) {
	log := logging.NoLog{}
	tmpDir := t.TempDir()

	// Clone coreth at a specific version
	corethPath := filepath.Join(tmpDir, "coreth")
	corethConfig, err := GetRepoConfig("coreth")
	require.NoError(t, err, "failed to get coreth config")

	// Use a known version
	corethRef := "v0.13.8"
	err = CloneRepo(log, corethConfig.GitRepo, corethPath, corethRef, 1)
	require.NoError(t, err, "failed to clone coreth")

	requestedRepos := []string{"avalanchego", "coreth"}
	explicitRefs := map[string]string{
		"coreth": corethRef,
	}

	versions, err := DiscoverAvalanchegoCorethVersions(log, tmpDir, requestedRepos, explicitRefs)
	require.NoError(t, err, "DiscoverAvalanchegoCorethVersions failed")
	require.Equal(t, corethRef, versions["coreth"], "expected explicit coreth version")
	require.NotEmpty(t, versions["avalanchego"], "expected avalanchego version to be discovered")

	// Verify the discovered avalanchego version exists in coreth's go.mod
	corethGoMod := filepath.Join(corethPath, "go.mod")
	avalanchegoConfig, err := GetRepoConfig("avalanchego")
	require.NoError(t, err, "failed to get avalanchego config")

	avalanchegoVersionInGoMod, err := GetDependencyVersion(log, corethGoMod, avalanchegoConfig.GoModule)
	require.NoError(t, err, "failed to get avalanchego version from coreth go.mod")

	// Convert version to git ref for comparison
	avalanchegoGitRef, err := ConvertVersionToGitRef(log, avalanchegoVersionInGoMod)
	require.NoError(t, err, "failed to convert avalanchego version to git ref")
	require.Equal(t, avalanchegoGitRef, versions["avalanchego"], "discovered avalanchego version should match coreth's go.mod")
}

// TestDiscoverAvalanchegoCorethVersions_AvalanchegoCloned_DiscoverCoreth tests discovering
// coreth version when avalanchego is already cloned locally
func TestDiscoverAvalanchegoCorethVersions_AvalanchegoCloned_DiscoverCoreth(t *testing.T) {
	log := logging.NoLog{}
	tmpDir := t.TempDir()

	// Clone avalanchego first at master
	avalanchegoPath := filepath.Join(tmpDir, "avalanchego")
	avalanchegoConfig, err := GetRepoConfig("avalanchego")
	require.NoError(t, err, "failed to get avalanchego config")

	err = CloneRepo(log, avalanchegoConfig.GitRepo, avalanchegoPath, avalanchegoConfig.DefaultBranch, 1)
	require.NoError(t, err, "failed to clone avalanchego")

	// Now discover coreth version
	requestedRepos := []string{"coreth"}
	explicitRefs := map[string]string{}

	versions, err := DiscoverAvalanchegoCorethVersions(log, tmpDir, requestedRepos, explicitRefs)
	require.NoError(t, err, "DiscoverAvalanchegoCorethVersions failed")
	require.NotEmpty(t, versions["coreth"], "expected coreth version to be discovered from avalanchego")

	// Verify it matches avalanchego's go.mod
	avalanchegoGoMod := filepath.Join(avalanchegoPath, "go.mod")
	corethConfig, err := GetRepoConfig("coreth")
	require.NoError(t, err, "failed to get coreth config")

	corethVersionInGoMod, err := GetDependencyVersion(log, avalanchegoGoMod, corethConfig.GoModule)
	require.NoError(t, err, "failed to get coreth version from avalanchego go.mod")

	corethGitRef, err := ConvertVersionToGitRef(log, corethVersionInGoMod)
	require.NoError(t, err, "failed to convert coreth version to git ref")
	require.Equal(t, corethGitRef, versions["coreth"], "discovered coreth version should match avalanchego's go.mod")
}

// TestDiscoverAvalanchegoCorethVersions_CorethCloned_DiscoverAvalanchego tests discovering
// avalanchego version when coreth is already cloned locally
func TestDiscoverAvalanchegoCorethVersions_CorethCloned_DiscoverAvalanchego(t *testing.T) {
	log := logging.NoLog{}
	tmpDir := t.TempDir()

	// Clone coreth first at main
	corethPath := filepath.Join(tmpDir, "coreth")
	corethConfig, err := GetRepoConfig("coreth")
	require.NoError(t, err, "failed to get coreth config")

	err = CloneRepo(log, corethConfig.GitRepo, corethPath, corethConfig.DefaultBranch, 1)
	require.NoError(t, err, "failed to clone coreth")

	// Now discover avalanchego version
	requestedRepos := []string{"avalanchego"}
	explicitRefs := map[string]string{}

	versions, err := DiscoverAvalanchegoCorethVersions(log, tmpDir, requestedRepos, explicitRefs)
	require.NoError(t, err, "DiscoverAvalanchegoCorethVersions failed")
	require.NotEmpty(t, versions["avalanchego"], "expected avalanchego version to be discovered from coreth")

	// Verify it matches coreth's go.mod
	corethGoMod := filepath.Join(corethPath, "go.mod")
	avalanchegoConfig, err := GetRepoConfig("avalanchego")
	require.NoError(t, err, "failed to get avalanchego config")

	avalanchegoVersionInGoMod, err := GetDependencyVersion(log, corethGoMod, avalanchegoConfig.GoModule)
	require.NoError(t, err, "failed to get avalanchego version from coreth go.mod")

	avalanchegoGitRef, err := ConvertVersionToGitRef(log, avalanchegoVersionInGoMod)
	require.NoError(t, err, "failed to convert avalanchego version to git ref")
	require.Equal(t, avalanchegoGitRef, versions["avalanchego"], "discovered avalanchego version should match coreth's go.mod")
}

// TestDiscoverAvalanchegoCorethVersions_NeitherExplicit_DefaultAvalanchego tests that when
// neither version is explicit and neither is cloned, avalanchego defaults to master and
// coreth is discovered from avalanchego's go.mod
func TestDiscoverAvalanchegoCorethVersions_NeitherExplicit_DefaultAvalanchego(t *testing.T) {
	log := logging.NoLog{}
	tmpDir := t.TempDir()

	requestedRepos := []string{"avalanchego", "coreth"}
	explicitRefs := map[string]string{}

	versions, err := DiscoverAvalanchegoCorethVersions(log, tmpDir, requestedRepos, explicitRefs)
	require.NoError(t, err, "DiscoverAvalanchegoCorethVersions failed")

	avalanchegoConfig, err := GetRepoConfig("avalanchego")
	require.NoError(t, err, "failed to get avalanchego config")

	require.Equal(t, avalanchegoConfig.DefaultBranch, versions["avalanchego"], "expected avalanchego to default to master")
	require.NotEmpty(t, versions["coreth"], "expected coreth version to be discovered")

	// The function should have cloned avalanchego and discovered coreth from it
	// Verify avalanchego was cloned
	avalanchegoPath := filepath.Join(tmpDir, "avalanchego")
	_, err = os.Stat(filepath.Join(avalanchegoPath, ".git"))
	require.NoError(t, err, "expected avalanchego to be cloned")
}

// TestDiscoverAvalanchegoCorethVersions_FromStandaloneMode tests version discovery from
// a directory with no primary repo (standalone mode)
func TestDiscoverAvalanchegoCorethVersions_FromStandaloneMode(t *testing.T) {
	log := logging.NoLog{}
	tmpDir := t.TempDir()

	// Don't create any go.mod - this simulates standalone mode

	requestedRepos := []string{"avalanchego", "coreth"}
	explicitRefs := map[string]string{}

	versions, err := DiscoverAvalanchegoCorethVersions(log, tmpDir, requestedRepos, explicitRefs)
	require.NoError(t, err, "DiscoverAvalanchegoCorethVersions failed")

	avalanchegoConfig, err := GetRepoConfig("avalanchego")
	require.NoError(t, err, "failed to get avalanchego config")

	require.Equal(t, avalanchegoConfig.DefaultBranch, versions["avalanchego"], "expected avalanchego master in standalone mode")
	require.NotEmpty(t, versions["coreth"], "expected coreth to be discovered")
}

// TestDiscoverAvalanchegoCorethVersions_FromFirewoodPrimary tests version discovery when
// firewood is the primary repo (it doesn't depend on avalanchego/coreth)
func TestDiscoverAvalanchegoCorethVersions_FromFirewoodPrimary(t *testing.T) {
	log := logging.NoLog{}
	tmpDir := t.TempDir()

	// Create firewood's ffi/go.mod to simulate being in firewood repo
	ffiDir := filepath.Join(tmpDir, "ffi")
	err := os.MkdirAll(ffiDir, 0o755)
	require.NoError(t, err, "failed to create ffi directory")

	firewoodConfig, err := GetRepoConfig("firewood")
	require.NoError(t, err, "failed to get firewood config")

	// Write a minimal go.mod
	goModContent := "module " + firewoodConfig.GoModule + "\n\ngo 1.24\n"
	err = os.WriteFile(filepath.Join(ffiDir, "go.mod"), []byte(goModContent), 0o600)
	require.NoError(t, err, "failed to write go.mod")

	requestedRepos := []string{"avalanchego", "coreth"}
	explicitRefs := map[string]string{}

	versions, err := DiscoverAvalanchegoCorethVersions(log, tmpDir, requestedRepos, explicitRefs)
	require.NoError(t, err, "DiscoverAvalanchegoCorethVersions failed")

	avalanchegoConfig, err := GetRepoConfig("avalanchego")
	require.NoError(t, err, "failed to get avalanchego config")

	require.Equal(t, avalanchegoConfig.DefaultBranch, versions["avalanchego"], "expected avalanchego master from firewood")
	require.NotEmpty(t, versions["coreth"], "expected coreth to be discovered")
}

// TestDiscoverFirewoodVersion_Explicit tests that explicit firewood version is used
func TestDiscoverFirewoodVersion_Explicit(t *testing.T) {
	log := logging.NoLog{}
	tmpDir := t.TempDir()

	explicitRef := "v0.2.0"
	requestedRepos := []string{"firewood"}
	discoveredVersions := map[string]string{}

	version, err := DiscoverFirewoodVersion(log, tmpDir, explicitRef, requestedRepos, discoveredVersions)
	require.NoError(t, err, "DiscoverFirewoodVersion failed")
	require.Equal(t, explicitRef, version, "expected explicit firewood version")
}

// TestDiscoverFirewoodVersion_FromCoreth tests discovering firewood version from coreth's go.mod
// when coreth is being synced
func TestDiscoverFirewoodVersion_FromCoreth(t *testing.T) {
	log := logging.NoLog{}
	tmpDir := t.TempDir()

	// Clone coreth
	corethPath := filepath.Join(tmpDir, "coreth")
	corethConfig, err := GetRepoConfig("coreth")
	require.NoError(t, err, "failed to get coreth config")

	err = CloneRepo(log, corethConfig.GitRepo, corethPath, corethConfig.DefaultBranch, 1)
	require.NoError(t, err, "failed to clone coreth")

	// Discover firewood version
	requestedRepos := []string{"coreth", "firewood"}
	discoveredVersions := map[string]string{
		"coreth": corethConfig.DefaultBranch,
	}

	version, err := DiscoverFirewoodVersion(log, tmpDir, "", requestedRepos, discoveredVersions)
	require.NoError(t, err, "DiscoverFirewoodVersion failed")
	require.NotEmpty(t, version, "expected firewood version to be discovered from coreth")

	// Verify it matches coreth's go.mod
	corethGoMod := filepath.Join(corethPath, "go.mod")
	firewoodConfig, err := GetRepoConfig("firewood")
	require.NoError(t, err, "failed to get firewood config")

	firewoodVersionInGoMod, err := GetDependencyVersion(log, corethGoMod, firewoodConfig.GoModule)
	require.NoError(t, err, "failed to get firewood version from coreth go.mod")

	firewoodGitRef, err := ConvertVersionToGitRef(log, firewoodVersionInGoMod)
	require.NoError(t, err, "failed to convert firewood version to git ref")
	require.Equal(t, firewoodGitRef, version, "discovered firewood version should match coreth's go.mod")
}

// TestDiscoverFirewoodVersion_FromAvalanchego tests discovering firewood version from
// avalanchego's go.mod when only avalanchego is involved (indirect dependency)
func TestDiscoverFirewoodVersion_FromAvalanchego(t *testing.T) {
	log := logging.NoLog{}
	tmpDir := t.TempDir()

	// Clone avalanchego
	avalanchegoPath := filepath.Join(tmpDir, "avalanchego")
	avalanchegoConfig, err := GetRepoConfig("avalanchego")
	require.NoError(t, err, "failed to get avalanchego config")

	err = CloneRepo(log, avalanchegoConfig.GitRepo, avalanchegoPath, avalanchegoConfig.DefaultBranch, 1)
	require.NoError(t, err, "failed to clone avalanchego")

	// Discover firewood version (no coreth involved)
	requestedRepos := []string{"avalanchego", "firewood"}
	discoveredVersions := map[string]string{
		"avalanchego": avalanchegoConfig.DefaultBranch,
	}

	version, err := DiscoverFirewoodVersion(log, tmpDir, "", requestedRepos, discoveredVersions)
	require.NoError(t, err, "DiscoverFirewoodVersion failed")
	require.NotEmpty(t, version, "expected firewood version to be discovered from avalanchego")

	// Verify it matches avalanchego's go.mod (indirect dependency)
	avalanchegoGoMod := filepath.Join(avalanchegoPath, "go.mod")
	firewoodConfig, err := GetRepoConfig("firewood")
	require.NoError(t, err, "failed to get firewood config")

	// avalanchego has indirect dependency on firewood through coreth
	// The version should be in go.mod as an indirect dependency
	firewoodVersionInGoMod, err := GetDependencyVersion(log, avalanchegoGoMod, firewoodConfig.GoModule)
	require.NoError(t, err, "failed to get firewood version from avalanchego go.mod")

	firewoodGitRef, err := ConvertVersionToGitRef(log, firewoodVersionInGoMod)
	require.NoError(t, err, "failed to convert firewood version to git ref")
	require.Equal(t, firewoodGitRef, version, "discovered firewood version should match avalanchego's go.mod")
}

// TestDiscoverFirewoodVersion_StandaloneDefaultBranch tests that firewood defaults to
// main when synced alone with no primary repo
func TestDiscoverFirewoodVersion_StandaloneDefaultBranch(t *testing.T) {
	log := logging.NoLog{}
	tmpDir := t.TempDir()

	// No other repos involved, no primary repo
	requestedRepos := []string{"firewood"}
	discoveredVersions := map[string]string{}

	version, err := DiscoverFirewoodVersion(log, tmpDir, "", requestedRepos, discoveredVersions)
	require.NoError(t, err, "DiscoverFirewoodVersion failed")

	firewoodConfig, err := GetRepoConfig("firewood")
	require.NoError(t, err, "failed to get firewood config")

	require.Equal(t, firewoodConfig.DefaultBranch, version, "expected firewood to default to main in standalone mode")
}

// TestDiscoverFirewoodVersion_CorethAlreadyCloned tests discovering firewood version
// when coreth is already cloned locally
func TestDiscoverFirewoodVersion_CorethAlreadyCloned(t *testing.T) {
	log := logging.NoLog{}
	tmpDir := t.TempDir()

	// Clone coreth first
	corethPath := filepath.Join(tmpDir, "coreth")
	corethConfig, err := GetRepoConfig("coreth")
	require.NoError(t, err, "failed to get coreth config")

	err = CloneRepo(log, corethConfig.GitRepo, corethPath, corethConfig.DefaultBranch, 1)
	require.NoError(t, err, "failed to clone coreth")

	// Now discover firewood (coreth not in requested repos, but already cloned)
	requestedRepos := []string{"firewood"}
	discoveredVersions := map[string]string{}

	version, err := DiscoverFirewoodVersion(log, tmpDir, "", requestedRepos, discoveredVersions)
	require.NoError(t, err, "DiscoverFirewoodVersion failed")
	require.NotEmpty(t, version, "expected firewood version to be discovered from cloned coreth")

	// Verify it matches coreth's go.mod
	corethGoMod := filepath.Join(corethPath, "go.mod")
	firewoodConfig, err := GetRepoConfig("firewood")
	require.NoError(t, err, "failed to get firewood config")

	firewoodVersionInGoMod, err := GetDependencyVersion(log, corethGoMod, firewoodConfig.GoModule)
	require.NoError(t, err, "failed to get firewood version from coreth go.mod")

	firewoodGitRef, err := ConvertVersionToGitRef(log, firewoodVersionInGoMod)
	require.NoError(t, err, "failed to convert firewood version to git ref")
	require.Equal(t, firewoodGitRef, version, "discovered firewood version should match coreth's go.mod")
}

// TestDiscoverFirewoodVersion_AvalanchegoAlreadyCloned tests discovering firewood version
// when avalanchego is already cloned locally (but not coreth)
func TestDiscoverFirewoodVersion_AvalanchegoAlreadyCloned(t *testing.T) {
	log := logging.NoLog{}
	tmpDir := t.TempDir()

	// Clone avalanchego first (no coreth)
	avalanchegoPath := filepath.Join(tmpDir, "avalanchego")
	avalanchegoConfig, err := GetRepoConfig("avalanchego")
	require.NoError(t, err, "failed to get avalanchego config")

	err = CloneRepo(log, avalanchegoConfig.GitRepo, avalanchegoPath, avalanchegoConfig.DefaultBranch, 1)
	require.NoError(t, err, "failed to clone avalanchego")

	// Now discover firewood (avalanchego not in requested repos, but already cloned, no coreth)
	requestedRepos := []string{"firewood"}
	discoveredVersions := map[string]string{}

	version, err := DiscoverFirewoodVersion(log, tmpDir, "", requestedRepos, discoveredVersions)
	require.NoError(t, err, "DiscoverFirewoodVersion failed")
	require.NotEmpty(t, version, "expected firewood version to be discovered from cloned avalanchego")

	// Verify it matches avalanchego's go.mod (indirect dependency)
	avalanchegoGoMod := filepath.Join(avalanchegoPath, "go.mod")
	firewoodConfig, err := GetRepoConfig("firewood")
	require.NoError(t, err, "failed to get firewood config")

	firewoodVersionInGoMod, err := GetDependencyVersion(log, avalanchegoGoMod, firewoodConfig.GoModule)
	require.NoError(t, err, "failed to get firewood version from avalanchego go.mod")

	firewoodGitRef, err := ConvertVersionToGitRef(log, firewoodVersionInGoMod)
	require.NoError(t, err, "failed to convert firewood version to git ref")
	require.Equal(t, firewoodGitRef, version, "discovered firewood version should match avalanchego's go.mod")
}

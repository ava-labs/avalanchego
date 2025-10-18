// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package core

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// Test that we can get config for avalanchego
func TestGetRepoConfig_Avalanchego(t *testing.T) {
	config, err := GetRepoConfig("avalanchego")
	require.NoError(t, err)

	require.Equal(t, "avalanchego", config.Name)
	require.Equal(t, "github.com/ava-labs/avalanchego", config.GoModule)
	require.Equal(t, "master", config.DefaultBranch)
	require.Equal(t, ".", config.ModuleReplacementPath)
	require.False(t, config.RequiresNixBuild)
}

// Test that we can get config for coreth
func TestGetRepoConfig_Coreth(t *testing.T) {
	config, err := GetRepoConfig("coreth")
	require.NoError(t, err)

	require.Equal(t, "coreth", config.Name)
	require.Equal(t, "github.com/ava-labs/coreth", config.GoModule)
	require.Equal(t, "master", config.DefaultBranch)
	require.False(t, config.RequiresNixBuild)
}

// Test that we can get config for firewood
func TestGetRepoConfig_Firewood(t *testing.T) {
	config, err := GetRepoConfig("firewood")
	require.NoError(t, err)

	require.Equal(t, "firewood", config.Name)
	require.Equal(t, "github.com/ava-labs/firewood-go-ethhash", config.GoModule)
	require.Equal(t, "main", config.DefaultBranch)
	require.Equal(t, "./ffi/result/ffi", config.ModuleReplacementPath)
	require.True(t, config.RequiresNixBuild)
	require.Equal(t, "ffi", config.NixBuildPath)
}

// Test that we get an error for unknown repos
func TestGetRepoConfig_Unknown(t *testing.T) {
	_, err := GetRepoConfig("unknown")
	if err == nil {
		require.Fail(t, "expected error for unknown repo")
	}
}

// Test that we can get all repo configs
func TestGetAllRepoConfigs(t *testing.T) {
	configs := GetAllRepoConfigs()
	require.Len(t, configs, 3)

	// Verify we have all three repos
	names := make(map[string]bool)
	for _, config := range configs {
		names[config.Name] = true
	}

	require.True(t, names["avalanchego"], "missing avalanchego config")
	require.True(t, names["coreth"], "missing coreth config")
	require.True(t, names["firewood"], "missing firewood config")
}

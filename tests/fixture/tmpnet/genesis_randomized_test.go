// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package tmpnet

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/genesis"
)

const networkID = uint32(147147)

func TestNewRandomizedTestGenesis(t *testing.T) {
	require := require.New(t)

	// Test without ANTITHESIS_RANDOM_SEED - should behave like normal genesis
	nodes := NewNodesOrPanic(5)
	keys, err := NewPrivateKeys(3)
	require.NoError(err)

	// Normal genesis without randomization
	originalGenesis, err := NewTestGenesis(networkID, nodes, keys)
	require.NoError(err)

	// Randomized genesis without env var should be the same
	randomizedGenesis, err := NewRandomizedTestGenesis(networkID, nodes, keys)
	require.NoError(err)

	// Should behave the same when no seed is set
	require.Equal(originalGenesis.NetworkID, randomizedGenesis.NetworkID)
	require.Len(randomizedGenesis.Allocations, len(originalGenesis.Allocations))

	// Test with ANTITHESIS_RANDOM_SEED set
	t.Setenv("ANTITHESIS_RANDOM_SEED", "12345")

	randomizedGenesis2, err := NewRandomizedTestGenesis(networkID, nodes, keys)
	require.NoError(err)

	// Should still have same basic structure
	require.Equal(originalGenesis.NetworkID, randomizedGenesis2.NetworkID)
	require.Len(randomizedGenesis2.Allocations, len(originalGenesis.Allocations))

	// But may have different timing parameters
	require.NotEqual(randomizedGenesis.InitialStakeDuration, randomizedGenesis2.InitialStakeDuration)
	require.NotEqual(randomizedGenesis.InitialStakeDurationOffset, randomizedGenesis2.InitialStakeDurationOffset)
}

func TestRandomizedParams(t *testing.T) {
	require := require.New(t)

	// Test with local params as base
	baseParams := genesis.LocalParams

	t.Run("without_env_var", func(t *testing.T) {
		// Ensure no env var is set for this subtest
		t.Setenv("ANTITHESIS_RANDOM_SEED", "")

		params := GetRandomizedParams(networkID)

		// Should return original config when no env var
		require.Equal(baseParams.TxFeeConfig.TxFee, params.TxFee)
		require.Equal(baseParams.StakingConfig.MinValidatorStake, params.MinValidatorStake)
	})

	t.Run("with_env_var", func(t *testing.T) {
		// Test with environment variable
		t.Setenv("ANTITHESIS_RANDOM_SEED", "54321")

		randomizedParams := GetRandomizedParams(networkID)

		// Should have randomized values
		require.NotEqual(baseParams.TxFeeConfig.DynamicFeeConfig.MinPrice, randomizedParams.DynamicFeeConfig.MinPrice)
		require.NotEqual(baseParams.StakingConfig.MinValidatorStake, randomizedParams.MinValidatorStake)

		// Test determinism - same seed should produce same results
		randomizedParams2 := GetRandomizedParams(networkID)

		require.Equal(randomizedParams.DynamicFeeConfig.MinPrice, randomizedParams2.DynamicFeeConfig.MinPrice)
		require.Equal(randomizedParams.MinValidatorStake, randomizedParams2.MinValidatorStake)
	})
}

func TestRandomizedParamsValidation(t *testing.T) {
	require := require.New(t)

	// Test with valid seeds
	testCases := []struct {
		seed string
		name string
	}{
		{"123456789", "positive integer"},
		{"0", "zero"},
		{"999999999999", "large number"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("ANTITHESIS_RANDOM_SEED", tc.seed)

			// Should not panic or error
			params := GetRandomizedParams(networkID)

			// Should have valid randomized values
			require.Greater(params.DynamicFeeConfig.MinPrice, genesis.LocalParams.DynamicFeeConfig.MinPrice)
			require.Positive(params.MinValidatorStake)
		})
	}
}

func TestRandomizedGenesisWithDifferentSeeds(t *testing.T) {
	require := require.New(t)

	nodes := NewNodesOrPanic(3)
	keys, err := NewPrivateKeys(2)
	require.NoError(err)

	// Test with different seeds produce different results
	seeds := []string{"111", "222", "333"}
	allParams := make([]genesis.Params, 0, len(seeds))

	for _, seed := range seeds {
		t.Setenv("ANTITHESIS_RANDOM_SEED", seed)

		// Test randomized params
		params := GetRandomizedParams(networkID)
		allParams = append(allParams, params)

		// Test randomized genesis creation works
		genesis, err := NewRandomizedTestGenesis(networkID, nodes, keys)
		require.NoError(err)
		require.NotNil(genesis)
		require.Equal(networkID, genesis.NetworkID)
	}

	// Verify different seeds produce different values (at least one pair should be different)
	pricesAllSame := allParams[0].DynamicFeeConfig.MinPrice == allParams[1].DynamicFeeConfig.MinPrice &&
		allParams[1].DynamicFeeConfig.MinPrice == allParams[2].DynamicFeeConfig.MinPrice
	require.False(pricesAllSame, "All P-chain min gas prices should not be the same")

	stakesAllSame := allParams[0].MinValidatorStake == allParams[1].MinValidatorStake &&
		allParams[1].MinValidatorStake == allParams[2].MinValidatorStake
	require.False(stakesAllSame, "All min validator stakes should not be the same")
}

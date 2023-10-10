// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package snowball

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/utils/sampler"
)

// Test that a network running the lower AlphaPreference converges faster than a
// network running equal Alpha values.
func TestDualAlphaOptimization(t *testing.T) {
	require := require.New(t)

	numColors := 10
	numNodes := 100
	params := Parameters{
		K:               20,
		AlphaPreference: 15,
		AlphaConfidence: 15,
		BetaVirtuous:    15,
		BetaRogue:       20,
	}
	seed := int64(0)

	singleAlphaNetwork := Network{}
	singleAlphaNetwork.Initialize(params, numColors)

	params.AlphaPreference = params.K/2 + 1
	dualAlphaNetwork := Network{}
	dualAlphaNetwork.Initialize(params, numColors)

	sampler.Seed(seed)
	for i := 0; i < numNodes; i++ {
		dualAlphaNetwork.AddNode(NewTree)
	}

	sampler.Seed(seed)
	for i := 0; i < numNodes; i++ {
		singleAlphaNetwork.AddNode(NewTree)
	}

	// Although this can theoretically fail with a correct implementation, it
	// shouldn't in practice
	runNetworksInLockstep(require, seed, &dualAlphaNetwork, &singleAlphaNetwork)
}

// Test that a network running the snowball tree converges faster than a network
// running the flat snowball protocol.
func TestTreeConvergenceOptimization(t *testing.T) {
	require := require.New(t)

	numColors := 10
	numNodes := 100
	params := DefaultParameters
	seed := int64(0)

	treeNetwork := Network{}
	treeNetwork.Initialize(params, numColors)

	flatNetwork := treeNetwork

	sampler.Seed(seed)
	for i := 0; i < numNodes; i++ {
		treeNetwork.AddNode(NewTree)
	}

	sampler.Seed(seed)
	for i := 0; i < numNodes; i++ {
		flatNetwork.AddNode(NewFlat)
	}

	// Although this can theoretically fail with a correct implementation, it
	// shouldn't in practice
	runNetworksInLockstep(require, seed, &treeNetwork, &flatNetwork)
}

func runNetworksInLockstep(require *require.Assertions, seed int64, fast *Network, slow *Network) {
	numRounds := 0
	for !fast.Finalized() && !fast.Disagreement() && !slow.Finalized() && !slow.Disagreement() {
		sampler.Seed(int64(numRounds) + seed)
		fast.Round()

		sampler.Seed(int64(numRounds) + seed)
		slow.Round()
		numRounds++
	}

	require.False(fast.Disagreement())
	require.False(slow.Disagreement())
	require.True(fast.Finalized())
	require.True(fast.Agreement())
}

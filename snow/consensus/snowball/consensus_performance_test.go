// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package snowball

import (
	"testing"

	"github.com/stretchr/testify/require"
	"gonum.org/v1/gonum/mathext/prng"
)

// Test that a network running the lower AlphaPreference converges faster than a
// network running equal Alpha values.
func TestDualAlphaOptimization(t *testing.T) {
	require := require.New(t)

	var (
		numColors = 10
		numNodes  = 100
		params    = Parameters{
			K:               20,
			AlphaPreference: 15,
			AlphaConfidence: 15,
			Beta:            20,
		}
		seed   uint64 = 0
		source        = prng.NewMT19937()
	)

	singleAlphaNetwork := NewNetwork(SnowballFactory, params, numColors, source)

	params.AlphaPreference = params.K/2 + 1
	dualAlphaNetwork := NewNetwork(SnowballFactory, params, numColors, source)

	source.Seed(seed)
	for i := 0; i < numNodes; i++ {
		dualAlphaNetwork.AddNode(NewTree)
	}

	source.Seed(seed)
	for i := 0; i < numNodes; i++ {
		singleAlphaNetwork.AddNode(NewTree)
	}

	// Although this can theoretically fail with a correct implementation, it
	// shouldn't in practice
	runNetworksInLockstep(require, seed, source, dualAlphaNetwork, singleAlphaNetwork)
}

// Test that a network running the snowball tree converges faster than a network
// running the flat snowball protocol.
func TestTreeConvergenceOptimization(t *testing.T) {
	require := require.New(t)

	var (
		numColors        = 10
		numNodes         = 100
		params           = DefaultParameters
		seed      uint64 = 0
		source           = prng.NewMT19937()
	)

	treeNetwork := NewNetwork(SnowballFactory, params, numColors, source)
	flatNetwork := NewNetwork(SnowballFactory, params, numColors, source)

	source.Seed(seed)
	for i := 0; i < numNodes; i++ {
		treeNetwork.AddNode(NewTree)
	}

	source.Seed(seed)
	for i := 0; i < numNodes; i++ {
		flatNetwork.AddNode(NewFlat)
	}

	// Although this can theoretically fail with a correct implementation, it
	// shouldn't in practice
	runNetworksInLockstep(require, seed, source, treeNetwork, flatNetwork)
}

func runNetworksInLockstep(require *require.Assertions, seed uint64, source *prng.MT19937, fast *Network, slow *Network) {
	numRounds := 0
	for !fast.Finalized() && !fast.Disagreement() && !slow.Finalized() && !slow.Disagreement() {
		source.Seed(uint64(numRounds) + seed)
		fast.Round()

		source.Seed(uint64(numRounds) + seed)
		slow.Round()
		numRounds++
	}

	require.False(fast.Disagreement())
	require.False(slow.Disagreement())
	require.True(fast.Finalized())
	require.True(fast.Agreement())
}

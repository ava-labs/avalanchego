// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package snowball

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/utils/sampler"
)

func TestSnowballGovernance(t *testing.T) {
	require := require.New(t)

	numColors := 2
	numNodes := 100
	numByzantine := 10
	numRed := 55
	params := Parameters{
		K: 20, Alpha: 15, BetaVirtuous: 20, BetaRogue: 30,
	}
	seed := int64(0)

	nBitwise := Network{}
	nBitwise.Initialize(params, numColors)

	sampler.Seed(seed)
	for i := 0; i < numRed; i++ {
		nBitwise.AddNodeSpecificColor(&Tree{}, 0, []int{1})
	}

	for _, node := range nBitwise.nodes {
		require.Equal(nBitwise.colors[0], node.Preference())
	}

	for i := 0; i < numNodes-numByzantine-numRed; i++ {
		nBitwise.AddNodeSpecificColor(&Tree{}, 1, []int{0})
	}

	for i := 0; i < numByzantine; i++ {
		nBitwise.AddNodeSpecificColor(&Byzantine{}, 1, []int{0})
	}

	for !nBitwise.Finalized() {
		nBitwise.Round()
	}

	for _, node := range nBitwise.nodes {
		if _, ok := node.(*Byzantine); ok {
			continue
		}
		require.Equal(nBitwise.colors[0], node.Preference())
	}
}

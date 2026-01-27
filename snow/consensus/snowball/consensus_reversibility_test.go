// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package snowball

import (
	"testing"

	"github.com/stretchr/testify/require"
	"gonum.org/v1/gonum/mathext/prng"
)

func TestSnowballGovernance(t *testing.T) {
	require := require.New(t)

	var (
		numColors           = 2
		numNodes            = 100
		numByzantine        = 10
		numRed              = 55
		params              = DefaultParameters
		seed         uint64 = 0
		source              = prng.NewMT19937()
	)

	nBitwise := NewNetwork(SnowballFactory, params, numColors, source)

	source.Seed(seed)
	for i := 0; i < numRed; i++ {
		nBitwise.AddNodeSpecificColor(NewTree, 0, []int{1})
	}

	for _, node := range nBitwise.nodes {
		require.Equal(nBitwise.colors[0], node.Preference())
	}

	for i := 0; i < numNodes-numByzantine-numRed; i++ {
		nBitwise.AddNodeSpecificColor(NewTree, 1, []int{0})
	}

	for i := 0; i < numByzantine; i++ {
		nBitwise.AddNodeSpecificColor(NewByzantine, 1, []int{0})
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

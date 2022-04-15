// Copyright (C) 2022, Chain4Travel AG. All rights reserved.
//
// This file is a derived work, based on ava-labs code whose
// original notices appear below.
//
// It is distributed under the same license conditions as the
// original code from which it is derived.
//
// Much love to the original authors for their work.
// **********************************************************

// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package snowball

import (
	"testing"

	"github.com/chain4travel/caminogo/utils/sampler"
)

func TestSnowballGovernance(t *testing.T) {
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
		nBitwise.AddNodeSpecificColor(&Tree{}, []int{0, 1})
	}

	for _, node := range nBitwise.nodes {
		if node.Preference() != nBitwise.colors[0] {
			t.Fatalf("Wrong preferences")
		}
	}

	for i := 0; i < numNodes-numByzantine-numRed; i++ {
		nBitwise.AddNodeSpecificColor(&Tree{}, []int{1, 0})
	}

	for i := 0; i < numByzantine; i++ {
		nBitwise.AddNodeSpecificColor(&Byzantine{}, []int{1, 0})
	}

	for !nBitwise.Finalized() {
		nBitwise.Round()
	}

	for _, node := range nBitwise.nodes {
		if _, ok := node.(*Byzantine); ok {
			continue
		}
		if node.Preference() != nBitwise.colors[0] {
			t.Fatalf("Wrong preferences")
		}
	}
}

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

func TestSnowballOptimized(t *testing.T) {
	numColors := 10
	numNodes := 100
	params := Parameters{
		K: 20, Alpha: 15, BetaVirtuous: 20, BetaRogue: 30,
	}
	seed := int64(0)

	nBitwise := Network{}
	nBitwise.Initialize(params, numColors)

	nNaive := nBitwise

	sampler.Seed(seed)
	for i := 0; i < numNodes; i++ {
		nBitwise.AddNode(&Tree{})
	}

	sampler.Seed(seed)
	for i := 0; i < numNodes; i++ {
		nNaive.AddNode(&Flat{})
	}

	numRounds := 0
	for !nBitwise.Finalized() && !nBitwise.Disagreement() && !nNaive.Finalized() && !nNaive.Disagreement() {
		sampler.Seed(int64(numRounds) + seed)
		nBitwise.Round()

		sampler.Seed(int64(numRounds) + seed)
		nNaive.Round()
		numRounds++
	}

	if nBitwise.Disagreement() || nNaive.Disagreement() {
		t.Fatalf("Network agreed on inconsistent values")
	}

	// Although this can theoretically fail with a correct implementation, it
	// shouldn't in practice
	if !nBitwise.Finalized() {
		t.Fatalf("Network agreed on values faster with naive implementation")
	}
	if !nBitwise.Agreement() {
		t.Fatalf("Network agreed on inconsistent values")
	}
}

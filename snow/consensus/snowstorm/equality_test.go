// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package snowstorm

import (
	"math/rand"
	"testing"

	"github.com/prometheus/client_golang/prometheus"

	sbcon "github.com/ava-labs/avalanchego/snow/consensus/snowball"
)

func TestConflictGraphEquality(t *testing.T) {
	Setup()

	numColors := 5
	colorsPerConsumer := 2
	maxInputConflicts := 2
	numNodes := 100
	params := sbcon.Parameters{
		Metrics:      prometheus.NewRegistry(),
		K:            20,
		Alpha:        11,
		BetaVirtuous: 20,
		BetaRogue:    30,
	}
	seed := int64(0)

	nDirected := Network{}
	rand.Seed(seed)
	nDirected.Initialize(params, numColors, colorsPerConsumer, maxInputConflicts)

	nInput := Network{}
	rand.Seed(seed)
	nInput.Initialize(params, numColors, colorsPerConsumer, maxInputConflicts)

	rand.Seed(seed)
	for i := 0; i < numNodes; i++ {
		nDirected.AddNode(&Directed{})
	}

	rand.Seed(seed)
	for i := 0; i < numNodes; i++ {
		nInput.AddNode(&Input{})
	}

	numRounds := 0
	for !nDirected.Finalized() && !nDirected.Disagreement() && !nInput.Finalized() && !nInput.Disagreement() {
		rand.Seed(int64(numRounds) + seed)
		nDirected.Round()

		rand.Seed(int64(numRounds) + seed)
		nInput.Round()
		numRounds++
	}

	if nDirected.Disagreement() || nInput.Disagreement() {
		t.Fatalf("Network agreed on inconsistent values")
	}

	if !nDirected.Finalized() ||
		!nInput.Finalized() {
		t.Fatalf("Network agreed on values faster with one of the implementations")
	}
	if !nDirected.Agreement() || !nInput.Agreement() {
		t.Fatalf("Network agreed on inconsistent values")
	}
}

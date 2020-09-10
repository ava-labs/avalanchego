// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package snowstorm

import (
	"math/rand"
	"testing"

	"github.com/prometheus/client_golang/prometheus"

	sbcon "github.com/ava-labs/avalanche-go/snow/consensus/snowball"
)

func Simulate(
	numColors, colorsPerConsumer, maxInputConflicts, numNodes int,
	params sbcon.Parameters,
	seed int64,
	fact Factory,
) {
	net := Network{}
	rand.Seed(seed)
	net.Initialize(
		params,
		numColors,
		colorsPerConsumer,
		maxInputConflicts,
	)

	rand.Seed(seed)
	for i := 0; i < numNodes; i++ {
		net.AddNode(fact.New())
	}

	numRounds := 0
	for !net.Finalized() && !net.Disagreement() && numRounds < 50 {
		rand.Seed(int64(numRounds) + seed)
		net.Round()
		numRounds++
	}
}

/*
 ******************************************************************************
 ********************************** Virtuous **********************************
 ******************************************************************************
 */

func BenchmarkVirtuousDirected(b *testing.B) {
	for n := 0; n < b.N; n++ {
		Simulate(
			/*numColors=*/ 25,
			/*colorsPerConsumer=*/ 1,
			/*maxInputConflicts=*/ 1,
			/*numNodes=*/ 50,
			/*params=*/ sbcon.Parameters{
				Metrics:      prometheus.NewRegistry(),
				K:            20,
				Alpha:        11,
				BetaVirtuous: 20,
				BetaRogue:    30,
			},
			/*seed=*/ 0,
			/*fact=*/ DirectedFactory{},
		)
	}
}

func BenchmarkVirtuousInput(b *testing.B) {
	for n := 0; n < b.N; n++ {
		Simulate(
			/*numColors=*/ 25,
			/*colorsPerConsumer=*/ 1,
			/*maxInputConflicts=*/ 1,
			/*numNodes=*/ 50,
			/*params=*/ sbcon.Parameters{
				Metrics:      prometheus.NewRegistry(),
				K:            20,
				Alpha:        11,
				BetaVirtuous: 20,
				BetaRogue:    30,
			},
			/*seed=*/ 0,
			/*fact=*/ InputFactory{},
		)
	}
}

/*
 ******************************************************************************
 *********************************** Rogue ************************************
 ******************************************************************************
 */

func BenchmarkRogueDirected(b *testing.B) {
	for n := 0; n < b.N; n++ {
		Simulate(
			/*numColors=*/ 25,
			/*colorsPerConsumer=*/ 1,
			/*maxInputConflicts=*/ 3,
			/*numNodes=*/ 50,
			/*params=*/ sbcon.Parameters{
				Metrics:      prometheus.NewRegistry(),
				K:            20,
				Alpha:        11,
				BetaVirtuous: 20,
				BetaRogue:    30,
			},
			/*seed=*/ 0,
			/*fact=*/ DirectedFactory{},
		)
	}
}

func BenchmarkRogueInput(b *testing.B) {
	for n := 0; n < b.N; n++ {
		Simulate(
			/*numColors=*/ 25,
			/*colorsPerConsumer=*/ 1,
			/*maxInputConflicts=*/ 3,
			/*numNodes=*/ 50,
			/*params=*/ sbcon.Parameters{
				Metrics:      prometheus.NewRegistry(),
				K:            20,
				Alpha:        11,
				BetaVirtuous: 20,
				BetaRogue:    30,
			},
			/*seed=*/ 0,
			/*fact=*/ InputFactory{},
		)
	}
}

/*
 ******************************************************************************
 ******************************** Many Inputs *********************************
 ******************************************************************************
 */

func BenchmarkMultiDirected(b *testing.B) {
	for n := 0; n < b.N; n++ {
		Simulate(
			/*numColors=*/ 50,
			/*colorsPerConsumer=*/ 10,
			/*maxInputConflicts=*/ 1,
			/*numNodes=*/ 50,
			/*params=*/ sbcon.Parameters{
				Metrics:      prometheus.NewRegistry(),
				K:            20,
				Alpha:        11,
				BetaVirtuous: 20,
				BetaRogue:    30,
			},
			/*seed=*/ 0,
			/*fact=*/ DirectedFactory{},
		)
	}
}

func BenchmarkMultiInput(b *testing.B) {
	for n := 0; n < b.N; n++ {
		Simulate(
			/*numColors=*/ 50,
			/*colorsPerConsumer=*/ 10,
			/*maxInputConflicts=*/ 1,
			/*numNodes=*/ 50,
			/*params=*/ sbcon.Parameters{
				Metrics:      prometheus.NewRegistry(),
				K:            20,
				Alpha:        11,
				BetaVirtuous: 20,
				BetaRogue:    30,
			},
			/*seed=*/ 0,
			/*fact=*/ InputFactory{},
		)
	}
}

/*
 ******************************************************************************
 ***************************** Many Rogue Inputs ******************************
 ******************************************************************************
 */

func BenchmarkMultiRogueDirected(b *testing.B) {
	for n := 0; n < b.N; n++ {
		Simulate(
			/*numColors=*/ 50,
			/*colorsPerConsumer=*/ 10,
			/*maxInputConflicts=*/ 3,
			/*numNodes=*/ 50,
			/*params=*/ sbcon.Parameters{
				Metrics:      prometheus.NewRegistry(),
				K:            20,
				Alpha:        11,
				BetaVirtuous: 20,
				BetaRogue:    30,
			},
			/*seed=*/ 0,
			/*fact=*/ DirectedFactory{},
		)
	}
}

func BenchmarkMultiRogueInput(b *testing.B) {
	for n := 0; n < b.N; n++ {
		Simulate(
			/*numColors=*/ 50,
			/*colorsPerConsumer=*/ 10,
			/*maxInputConflicts=*/ 3,
			/*numNodes=*/ 50,
			/*params=*/ sbcon.Parameters{
				Metrics:      prometheus.NewRegistry(),
				K:            20,
				Alpha:        11,
				BetaVirtuous: 20,
				BetaRogue:    30,
			},
			/*seed=*/ 0,
			/*fact=*/ InputFactory{},
		)
	}
}

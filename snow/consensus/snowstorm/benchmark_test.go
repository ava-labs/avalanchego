// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package snowstorm

import (
	"testing"

	"github.com/ava-labs/avalanchego/utils/sampler"

	sbcon "github.com/ava-labs/avalanchego/snow/consensus/snowball"
)

func Simulate(
	numColors, colorsPerConsumer, maxInputConflicts, numNodes int,
	params sbcon.Parameters,
	seed int64,
	fact Factory,
) error {
	net := Network{}
	sampler.Seed(seed)
	net.Initialize(
		params,
		numColors,
		colorsPerConsumer,
		maxInputConflicts,
	)

	sampler.Seed(seed)
	for i := 0; i < numNodes; i++ {
		if err := net.AddNode(fact.New()); err != nil {
			return err
		}
	}

	numRounds := 0
	for !net.Finalized() && !net.Disagreement() && numRounds < 50 {
		sampler.Seed(int64(numRounds) + seed)
		if err := net.Round(); err != nil {
			return err
		}
		numRounds++
	}
	return nil
}

/*
 ******************************************************************************
 ********************************** Virtuous **********************************
 ******************************************************************************
 */

func BenchmarkVirtuousDirected(b *testing.B) {
	for n := 0; n < b.N; n++ {
		err := Simulate(
			/*numColors=*/ 25,
			/*colorsPerConsumer=*/ 1,
			/*maxInputConflicts=*/ 1,
			/*numNodes=*/ 50,
			/*params=*/ sbcon.Parameters{
				K:                 20,
				Alpha:             11,
				BetaVirtuous:      20,
				BetaRogue:         30,
				ConcurrentRepolls: 1,
			},
			/*seed=*/ 0,
			/*fact=*/ DirectedFactory{},
		)
		if err != nil {
			b.Fatal(err)
		}
	}
}

/*
 ******************************************************************************
 *********************************** Rogue ************************************
 ******************************************************************************
 */

func BenchmarkRogueDirected(b *testing.B) {
	for n := 0; n < b.N; n++ {
		err := Simulate(
			/*numColors=*/ 25,
			/*colorsPerConsumer=*/ 1,
			/*maxInputConflicts=*/ 3,
			/*numNodes=*/ 50,
			/*params=*/ sbcon.Parameters{
				K:                 20,
				Alpha:             11,
				BetaVirtuous:      20,
				BetaRogue:         30,
				ConcurrentRepolls: 1,
			},
			/*seed=*/ 0,
			/*fact=*/ DirectedFactory{},
		)
		if err != nil {
			b.Fatal(err)
		}
	}
}

/*
 ******************************************************************************
 ******************************** Many Inputs *********************************
 ******************************************************************************
 */

func BenchmarkMultiDirected(b *testing.B) {
	for n := 0; n < b.N; n++ {
		err := Simulate(
			/*numColors=*/ 50,
			/*colorsPerConsumer=*/ 10,
			/*maxInputConflicts=*/ 1,
			/*numNodes=*/ 50,
			/*params=*/ sbcon.Parameters{
				K:                 20,
				Alpha:             11,
				BetaVirtuous:      20,
				BetaRogue:         30,
				ConcurrentRepolls: 1,
			},
			/*seed=*/ 0,
			/*fact=*/ DirectedFactory{},
		)
		if err != nil {
			b.Fatal(err)
		}
	}
}

/*
 ******************************************************************************
 ***************************** Many Rogue Inputs ******************************
 ******************************************************************************
 */

func BenchmarkMultiRogueDirected(b *testing.B) {
	for n := 0; n < b.N; n++ {
		err := Simulate(
			/*numColors=*/ 50,
			/*colorsPerConsumer=*/ 10,
			/*maxInputConflicts=*/ 3,
			/*numNodes=*/ 50,
			/*params=*/ sbcon.Parameters{
				K:                 20,
				Alpha:             11,
				BetaVirtuous:      20,
				BetaRogue:         30,
				ConcurrentRepolls: 1,
			},
			/*seed=*/ 0,
			/*fact=*/ DirectedFactory{},
		)
		if err != nil {
			b.Fatal(err)
		}
	}
}

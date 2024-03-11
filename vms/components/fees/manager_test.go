// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package fees

import (
	"math"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestComputeNextEmptyWindows(t *testing.T) {
	require := require.New(t)

	var (
		initialUnitFees  = Dimensions{1, 1, 1, 1}
		consumedUnits    = Dimensions{5, 25, 30, 2500}
		targetComplexity = Dimensions{25, 25, 25, 25}

		// last block happened within Window
		lastBlkTime = time.Now().Truncate(time.Second)
		currBlkTime = lastBlkTime.Add(time.Second)

		updateCoefficient = Dimensions{1, 2, 5, 10}
		minUnitFees       = Dimensions{0, 0, 0, 0}
	)

	m := &Manager{
		unitFees:       initialUnitFees,
		windows:        [FeeDimensions]Window{},
		cumulatedUnits: consumedUnits,
	}
	next := m.ComputeNext(
		lastBlkTime.Unix(),
		currBlkTime.Unix(),
		targetComplexity,
		updateCoefficient,
		minUnitFees,
	)

	// Bandwidth units are below target, next unit fees are pushed to the minimum
	require.Equal(minUnitFees[Bandwidth], next.unitFees[Bandwidth])

	// UTXORead units are at target, next unit fees are kept equal
	require.Equal(initialUnitFees[UTXORead], next.unitFees[UTXORead])

	// UTXOWrite units are above target, next unit fees increased
	require.Equal(uint64(4), next.unitFees[UTXOWrite])

	// Compute units are way above target, next unit fees are increased to the max
	require.Equal(uint64(4*1<<60), next.unitFees[Compute])

	// next cumulated units are zeroed
	require.Equal(Dimensions{}, next.cumulatedUnits)
}

func TestComputeNextNonEmptyWindows(t *testing.T) {
	require := require.New(t)

	var (
		initialUnitFees  = Dimensions{1, 1, 1, 1}
		consumedUnits    = Dimensions{0, 0, 0, 0}
		targetComplexity = Dimensions{25, 25, 25, 25}

		// last block happened within Window
		lastBlkTime = time.Now().Truncate(time.Second)
		currBlkTime = lastBlkTime.Add(time.Second)

		updateCoefficient = Dimensions{1, 2, 5, 10}
		minUnitFees       = Dimensions{0, 0, 0, 0}
	)

	m := &Manager{
		unitFees: initialUnitFees,
		windows: [FeeDimensions]Window{
			{1, 1, 1, 2, 2, 3, 3, 4, 5, 0},                                   // increasing but overall below target
			{0, 0, math.MaxUint64, 0, 0, 0, 0, 0, 0, 0},                      // spike within window
			{10, 20, 30, 40, 50, 35, 25, 15, 10, 10},                         // decreasing but overall above target
			{0, 0, 0, 0, 0, math.MaxUint64 / 2, math.MaxUint64 / 2, 0, 0, 0}, // again pretty spiky
		},
		cumulatedUnits: consumedUnits,
	}
	next := m.ComputeNext(
		lastBlkTime.Unix(),
		currBlkTime.Unix(),
		targetComplexity,
		updateCoefficient,
		minUnitFees,
	)

	// Bandwidth units are below target, next unit fees are pushed to the minimum
	require.Equal(minUnitFees[Bandwidth], next.unitFees[Bandwidth])

	// UTXORead units are at above target, due to spike in window. Next unit fees are increased
	require.Equal(uint64(4*1<<60), next.unitFees[UTXORead])

	// UTXOWrite units are above target, even if they are decreasing within the window. Next unit fees increased.
	require.Equal(uint64(2*1<<60), next.unitFees[UTXOWrite])

	// Compute units are above target, next unit fees are increased.
	require.Equal(uint64(4*1<<60), next.unitFees[Compute])

	// next cumulated units are zeroed
	require.Equal(Dimensions{}, next.cumulatedUnits)
}

func TestComputeNextEdgeCases(t *testing.T) {
	require := require.New(t)

	var (
		initialUnitFees  = Dimensions{1, 2, 2, 2}
		consumedUnits    = Dimensions{0, 0, 0, 0}
		targetComplexity = Dimensions{math.MaxUint64, 1, 1, 1} // a very skewed requirement for block complexity

		// last block happened within Window
		lastBlkTime = time.Now().Truncate(time.Second)
		currBlkTime = lastBlkTime.Add(time.Second)

		updateCoefficient = Dimensions{1, 1, 1, 1}
		minUnitFees       = Dimensions{1, 0, 0, 0}
	)

	m := &Manager{
		unitFees: initialUnitFees,
		windows: [FeeDimensions]Window{
			{0, 0, 0, math.MaxUint64, 0, 0, 0, 0, 0, 0}, // a huge spike in the past, on the non-constrained dimension
			{0, 1, 0, 0, 0, 0, 0, 0, 0, 0},              // a small spike, but it hits the small constrain set for this dimension
			{},
			{},
		},
		cumulatedUnits: consumedUnits,
	}
	next := m.ComputeNext(
		lastBlkTime.Unix(),
		currBlkTime.Unix(),
		targetComplexity,
		updateCoefficient,
		minUnitFees,
	)

	// Bandwidth units are below target, next unit fees are pushed to the minimum
	require.Equal(minUnitFees[Bandwidth], next.unitFees[Bandwidth])

	// UTXORead units are at target, due to spike in window. Unit fees are kept unchanged.
	require.Equal(initialUnitFees[UTXORead], next.unitFees[UTXORead])

	// UTXOWrite units are below target. Unit fees are decreased.
	require.Equal(minUnitFees[UTXOWrite], next.unitFees[UTXOWrite])

	// Compute units are below target. Unit fees are decreased.
	require.Equal(minUnitFees[Compute], next.unitFees[Compute])

	// next cumulated units are zeroed
	require.Equal(Dimensions{}, next.cumulatedUnits)
}

func TestComputeNextStability(t *testing.T) {
	// The advantage of using an exponential fee update scheme
	// (vs e.g. the EIP-1559 scheme we use in the C-chain) is that
	// it is more stable against dithering.
	// We prove here that if consumed used oscillate around the target
	// unit fees are unchanged.

	require := require.New(t)

	var (
		initialUnitFees  = Dimensions{10, 100, 1_000, 1_000_000_000}
		consumedUnits1   = Dimensions{24, 45, 70, 500}
		consumedUnits2   = Dimensions{26, 55, 130, 1500}
		targetComplexity = Dimensions{25, 50, 100, 1000}

		lastBlkTime = time.Now().Truncate(time.Second)
		currBlkTime = lastBlkTime.Add(time.Second)

		updateCoefficient = Dimensions{1, 2, 5, 10}
		minUnitFees       = Dimensions{0, 0, 0, 0}
	)

	// step1: cumulated units are below target. Unit fees must decrease
	m1 := &Manager{
		unitFees:       initialUnitFees,
		windows:        [FeeDimensions]Window{},
		cumulatedUnits: consumedUnits1,
	}
	next1 := m1.ComputeNext(
		lastBlkTime.Unix(),
		currBlkTime.Unix(),
		targetComplexity,
		updateCoefficient,
		minUnitFees,
	)

	require.Less(next1.unitFees[Bandwidth], initialUnitFees[Bandwidth])
	require.Less(next1.unitFees[UTXORead], initialUnitFees[UTXORead])
	require.Less(next1.unitFees[UTXOWrite], initialUnitFees[UTXOWrite])
	require.Less(next1.unitFees[Compute], initialUnitFees[Compute])

	// step2: cumulated units go slight above target, so that average consumed units are at target.
	// Unit fees go back to the original value
	m2 := &Manager{
		unitFees:       next1.unitFees,
		windows:        [FeeDimensions]Window{},
		cumulatedUnits: consumedUnits2,
	}
	next2 := m2.ComputeNext(
		lastBlkTime.Unix(),
		currBlkTime.Unix(),
		targetComplexity,
		updateCoefficient,
		minUnitFees,
	)

	require.Equal(initialUnitFees[Bandwidth], next2.unitFees[Bandwidth])
	require.Equal(initialUnitFees[UTXORead], next2.unitFees[UTXORead])
	require.Equal(initialUnitFees[UTXOWrite], next2.unitFees[UTXOWrite])
	require.Equal(initialUnitFees[Compute], next2.unitFees[Compute])
}

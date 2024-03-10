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
	require.Equal(uint64(3), next.unitFees[UTXOWrite])

	// Compute units are way above target, next unit fees are increased to the max
	require.Equal(uint64(math.MaxUint64), next.unitFees[Compute])

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
	require.Equal(initialUnitFees[Bandwidth], next.unitFees[Bandwidth])

	// UTXORead units are at above target, due to spike in window. Next unit fees are increased
	require.Equal(uint64(math.MaxUint64), next.unitFees[UTXORead])

	// UTXOWrite units are above target, even if they are decreasing within the window. Next unit fees increased.
	require.Equal(uint64(1739274941520501248), next.unitFees[UTXOWrite])

	// Compute units are above target, next unit fees are increased.
	require.Equal(uint64(math.MaxUint64), next.unitFees[Compute])

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
	require.Equal(initialUnitFees[UTXOWrite]/2, next.unitFees[UTXOWrite])

	// Compute units are below target. Unit fees are decreased.
	require.Equal(initialUnitFees[Compute]/2, next.unitFees[Compute])

	// next cumulated units are zeroed
	require.Equal(Dimensions{}, next.cumulatedUnits)
}

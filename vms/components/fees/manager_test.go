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
		feesCfg = DynamicFeesConfig{
			MinUnitFees:       Dimensions{1, 1, 1, 1},
			UpdateCoefficient: Dimensions{1, 2, 5, 10},
			BlockUnitsTarget:  Dimensions{25, 25, 25, 25},
		}
		currentUnitFees = Dimensions{1, 1, 1, 1}
		consumedUnits   = Dimensions{2500, 25, 20, 50}
		feeWindows      = Windows{}

		// last block happened within Window
		lastBlkTime = time.Now().Truncate(time.Second)
		currBlkTime = lastBlkTime.Add(time.Second)
	)

	m := &Manager{
		unitFees:       currentUnitFees,
		cumulatedUnits: consumedUnits,
	}
	m.UpdateUnitFees(
		feesCfg,
		feeWindows,
		lastBlkTime.Unix(),
		currBlkTime.Unix(),
	)

	// Bandwidth units are way above target, next unit fees are pushed up
	require.Equal(feesCfg.MinUnitFees[Bandwidth], m.unitFees[Bandwidth])

	// UTXORead units are below target, next unit fees are pushed to the minimum
	require.Equal(feesCfg.MinUnitFees[UTXORead], m.unitFees[UTXORead])

	// UTXOWriteunits are below target, next unit fees are pushed to the minimum
	require.Equal(feesCfg.MinUnitFees[UTXOWrite], m.unitFees[UTXOWrite])

	// Compute units are below target, next unit fees are pushed to the minimum
	require.Equal(feesCfg.MinUnitFees[Compute], m.unitFees[Compute])

	m.UpdateWindows(&feeWindows, lastBlkTime.Unix(), currBlkTime.Unix())
	m.UpdateUnitFees(
		feesCfg,
		feeWindows,
		lastBlkTime.Unix(),
		currBlkTime.Unix(),
	)

	// Bandwidth units are above target, next unit fees are pushed up
	require.Equal(uint64(0x4000000000000000), m.unitFees[Bandwidth])

	// UTXORead units are at target, unit fees are not changed
	require.Equal(feesCfg.MinUnitFees[UTXORead], m.unitFees[UTXORead])

	// UTXOWrite units are below target, next unit fees are pushed down if there is space
	require.Equal(feesCfg.MinUnitFees[UTXOWrite], m.unitFees[UTXOWrite])

	// Compute units are above target, next unit fees are pushed up
	require.Equal(uint64(0x4000), m.unitFees[Compute])
}

func TestComputeNextNonEmptyWindows(t *testing.T) {
	require := require.New(t)

	var (
		feesCfg = DynamicFeesConfig{
			MinUnitFees:       Dimensions{0, 0, 0, 0},
			UpdateCoefficient: Dimensions{1, 2, 5, 10},
			BlockUnitsTarget:  Dimensions{25, 25, 25, 25},
		}
		currentUnitFees = Dimensions{1, 1, 1, 1}
		consumedUnits   = Dimensions{0, 0, 0, 0}
		feeWindows      = Windows{
			{1, 1, 1, 2, 2, 3, 3, 4, 5, 0},                                   // increasing but overall below target
			{0, 0, math.MaxUint64, 0, 0, 0, 0, 0, 0, 0},                      // spike within window
			{10, 20, 30, 40, 50, 35, 25, 15, 10, 10},                         // decreasing but overall above target
			{0, 0, 0, 0, 0, math.MaxUint64 / 2, math.MaxUint64 / 2, 0, 0, 0}, // again pretty spiky
		}

		// last block happened within Window
		lastBlkTime = time.Now().Truncate(time.Second)
		currBlkTime = lastBlkTime.Add(time.Second)
	)

	m := &Manager{
		unitFees:       currentUnitFees,
		cumulatedUnits: consumedUnits,
	}
	m.UpdateUnitFees(
		feesCfg,
		feeWindows,
		lastBlkTime.Unix(),
		currBlkTime.Unix(),
	)

	// Bandwidth units are below target, next unit fees are pushed down
	require.Equal(uint64(0x1), m.unitFees[Bandwidth])

	// UTXORead units are at above target, due to spike in window. Next unit fees are increased
	require.Equal(uint64(0x4000000000000000), m.unitFees[UTXORead])

	// UTXOWrite units are above target, even if they are decreasing within the window. Next unit fees are increased.
	require.Equal(uint64(0x1000000000000000), m.unitFees[UTXOWrite])

	// Compute units are above target, next unit fees are increased.
	require.Equal(uint64(0x4000000000000000), m.unitFees[Compute])
}

func TestComputeNextEdgeCases(t *testing.T) {
	require := require.New(t)

	var (
		feesCfg = DynamicFeesConfig{
			MinUnitFees:       Dimensions{1, 0, 0, 0},
			UpdateCoefficient: Dimensions{1, 1, 1, 1},
			BlockUnitsTarget:  Dimensions{math.MaxUint64, 1, 1, 1}, // a very skewed requirement for block complexity
		}
		currentUnitFees = Dimensions{1, 2, 2, 2}
		consumedUnits   = Dimensions{0, 0, 0, 0}
		feeWindows      = Windows{
			{0, 0, 0, math.MaxUint64, 0, 0, 0, 0, 0, 0}, // a huge spike in the past, on the non-constrained dimension
			{0, 1, 0, 0, 0, 0, 0, 0, 0, 0},              // a small spike, but it hits the small constrain set for this dimension
			{},
			{},
		}

		// last block happened within Window
		lastBlkTime = time.Now().Truncate(time.Second)
		currBlkTime = lastBlkTime.Add(time.Second)
	)

	m := &Manager{
		unitFees:       currentUnitFees,
		cumulatedUnits: consumedUnits,
	}

	m.UpdateUnitFees(
		feesCfg,
		feeWindows,
		lastBlkTime.Unix(),
		currBlkTime.Unix(),
	)

	// Bandwidth units are below target, next unit fees are pushed to the minimum
	require.Equal(feesCfg.MinUnitFees[Bandwidth], m.unitFees[Bandwidth])

	// UTXORead units are at target, due to spike in window. Unit fees are kept unchanged.
	require.Equal(currentUnitFees[UTXORead], m.unitFees[UTXORead])

	// UTXOWrite units are below target. Unit fees are decreased.
	require.Equal(uint64(0x1), m.unitFees[UTXOWrite])

	// Compute units are below target. Unit fees are decreased.
	require.Equal(uint64(0x1), m.unitFees[Compute])
}

func TestComputeNextStability(t *testing.T) {
	// The advantage of using an exponential fee update scheme
	// (vs e.g. the EIP-1559 scheme we use in the C-chain) is that
	// it is more stable against dithering.
	// We prove here that if consumed used oscillate around the target
	// unit fees are unchanged.

	require := require.New(t)

	var (
		feesCfg = DynamicFeesConfig{
			MinUnitFees:       Dimensions{0, 0, 0, 0},
			UpdateCoefficient: Dimensions{2, 4, 5, 10},
			BlockUnitsTarget:  Dimensions{25, 50, 100, 1000},
		}
		initialUnitFees = Dimensions{10, 100, 1_000, 1_000_000_000}
		feeWindows1     = Windows{}
		feeWindows2     = Windows{}
		consumedUnits1  = Dimensions{24, 45, 70, 500}
		consumedUnits2  = Dimensions{26, 55, 130, 1500}

		// last block happened within Window
		lastBlkTime = time.Now().Truncate(time.Second)
		currBlkTime = lastBlkTime.Add(time.Second)
	)

	// step1: cumulated units are below target. Unit fees must decrease
	m1 := &Manager{
		unitFees:       initialUnitFees,
		cumulatedUnits: consumedUnits1,
	}
	m1.UpdateWindows(&feeWindows1, lastBlkTime.Unix(), currBlkTime.Unix())
	m1.UpdateUnitFees(
		feesCfg,
		feeWindows1,
		lastBlkTime.Unix(),
		currBlkTime.Unix(),
	)

	require.LessOrEqual(m1.unitFees[Bandwidth], initialUnitFees[Bandwidth])
	require.LessOrEqual(m1.unitFees[UTXORead], initialUnitFees[UTXORead])
	require.Less(m1.unitFees[UTXOWrite], initialUnitFees[UTXOWrite])
	require.Less(m1.unitFees[Compute], initialUnitFees[Compute])

	// step2: cumulated units go slight above target, so that average consumed units are at target.
	// Unit fees go back to the original value
	m2 := &Manager{
		unitFees:       m1.unitFees,
		cumulatedUnits: consumedUnits2,
	}
	m2.UpdateWindows(&feeWindows2, lastBlkTime.Unix(), currBlkTime.Unix())
	m2.UpdateUnitFees(
		feesCfg,
		feeWindows2,
		lastBlkTime.Unix(),
		currBlkTime.Unix(),
	)

	require.Equal(initialUnitFees[Bandwidth], m2.unitFees[Bandwidth])
	require.Equal(initialUnitFees[UTXORead], m2.unitFees[UTXORead])
	require.Equal(initialUnitFees[UTXOWrite], m2.unitFees[UTXOWrite])
	require.Equal(initialUnitFees[Compute], m2.unitFees[Compute])
}

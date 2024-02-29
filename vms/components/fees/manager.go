// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package fees

import (
	"fmt"
	"math"

	safemath "github.com/ava-labs/avalanchego/utils/math"
)

type Manager struct {
	// Avax denominated unit fees for all fee dimensions
	unitFees Dimensions

	// consumed units window per each fee dimension.
	windows Windows

	// cumulatedUnits helps aggregating the units consumed by a block
	// so that we can verify it's not too big/build it properly.
	cumulatedUnits Dimensions
}

func NewManager(unitFees Dimensions, windows Windows) *Manager {
	return &Manager{
		unitFees: unitFees,
		windows:  windows,
	}
}

func (m *Manager) GetUnitFees() Dimensions {
	return m.unitFees
}

func (m *Manager) GetFeeWindows() Windows {
	return m.windows
}

func (m *Manager) GetCumulatedUnits() Dimensions {
	return m.cumulatedUnits
}

// CalculateFee must be a stateless method
func (m *Manager) CalculateFee(units Dimensions) (uint64, error) {
	fee := uint64(0)

	for i := Dimension(0); i < FeeDimensions; i++ {
		contribution, err := safemath.Mul64(m.unitFees[i], units[i])
		if err != nil {
			return 0, err
		}
		fee, err = safemath.Add64(contribution, fee)
		if err != nil {
			return 0, err
		}
	}
	return fee, nil
}

// CumulateUnits tries to cumulate the consumed units [units]. Before
// actually cumulating them, it checks whether the result would breach [bounds].
// If so, it returns the first dimension to breach bounds.
func (m *Manager) CumulateUnits(units, bounds Dimensions) (bool, Dimension) {
	// Ensure we can consume (don't want partial update of values)
	for i := Dimension(0); i < FeeDimensions; i++ {
		consumed, err := safemath.Add64(m.cumulatedUnits[i], units[i])
		if err != nil {
			return true, i
		}
		if consumed > bounds[i] {
			return true, i
		}
	}

	// Commit to consumption
	for i := Dimension(0); i < FeeDimensions; i++ {
		consumed, err := safemath.Add64(m.cumulatedUnits[i], units[i])
		if err != nil {
			return true, i
		}
		m.cumulatedUnits[i] = consumed
	}
	return false, 0
}

// Sometimes, e.g. while building a tx, we'd like freedom to speculatively add units
// and to remove them later on. [RemoveUnits] grants this freedom
func (m *Manager) RemoveUnits(unitsToRm Dimensions) error {
	var revertedUnits Dimensions
	for i := Dimension(0); i < FeeDimensions; i++ {
		prev, err := safemath.Sub(m.cumulatedUnits[i], unitsToRm[i])
		if err != nil {
			return fmt.Errorf("%w: dimension %d", err, i)
		}
		revertedUnits[i] = prev
	}

	m.cumulatedUnits = revertedUnits
	return nil
}

func (m *Manager) ComputeNext(
	lastTime,
	currTime int64,
	targetUnits,
	priceChangeDenominator,
	minUnitPrice Dimensions,
) *Manager {
	since := int(currTime - lastTime)
	nextManager := &Manager{}
	for i := Dimension(0); i < FeeDimensions; i++ {
		nextUnitPrice, nextUnitWindow := computeNextPriceWindow(
			m.windows[i],
			m.cumulatedUnits[i],
			m.unitFees[i],
			targetUnits[i],
			priceChangeDenominator[i],
			minUnitPrice[i],
			since,
		)

		nextManager.unitFees[i] = nextUnitPrice
		nextManager.windows[i] = nextUnitWindow
		// unit consumed are zeroed in nextManager
	}
	return nextManager
}

func computeNextPriceWindow(
	current Window,
	currentUnitsConsumed uint64,
	currentUnitFee uint64,
	target uint64, /* per window, must be non-zero */
	changeDenom uint64,
	minUnitFee uint64,
	since int, /* seconds */
) (uint64, Window) {
	newRollupWindow := Roll(current, since)
	if since < WindowSize {
		// add in the units used by the parent block in the correct place
		// If the parent consumed units within the rollup window, add the consumed
		// units in.
		start := WindowSize - 1 - since
		Update(&newRollupWindow, start, currentUnitsConsumed)
	}

	var (
		totalUnitsConsumed = Sum(newRollupWindow)
		nextUnitFee        = currentUnitFee
	)

	switch {
	case totalUnitsConsumed == target:
		return nextUnitFee, newRollupWindow
	case totalUnitsConsumed > target:
		// If the parent block used more units than its target, the baseFee should increase.
		delta := currentUnitFee * (totalUnitsConsumed - target) / target / changeDenom

		// make sure that delta is non zero. We want to move unit fees
		// of at least a unit (they should not stay the same since totalUnitsConsumed > target)
		delta = max(delta, 1)

		var over error
		nextUnitFee, over = safemath.Add64(nextUnitFee, delta)
		if over != nil {
			nextUnitFee = math.MaxUint64
		}

	case totalUnitsConsumed < target:
		// Otherwise if the parent block used less units than its target, the baseFee should decrease.
		delta := currentUnitFee * (target - totalUnitsConsumed) / target / changeDenom

		// make sure that delta is non zero. We want to move unit fees
		// of at least a unit (they should not stay the same since totalUnitsConsumed < target)
		delta = max(delta, 1)

		// if we had no blocks for more than [WindowSize] seconds, we reduce fees even more,
		// to try and account for all the low activity interval
		if since > WindowSize {
			delta *= uint64(since / WindowSize)
		}

		var under error
		nextUnitFee, under = safemath.Sub(nextUnitFee, delta)
		if under != nil {
			nextUnitFee = 0
		}
	}

	nextUnitFee = max(nextUnitFee, minUnitFee)
	return nextUnitFee, newRollupWindow
}

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
	updateCoefficients,
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
			updateCoefficients[i],
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
	updateCoefficient uint64,
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

	totalUnitsConsumed := Sum(newRollupWindow)
	nextUnitFee := nextFeeRate(currentUnitFee, updateCoefficient, totalUnitsConsumed, target)
	nextUnitFee = max(nextUnitFee, minUnitFee)
	return nextUnitFee, newRollupWindow
}

func nextFeeRate(currentUnitFee, updateCoefficient, unitsConsumed, target uint64) uint64 {
	// We approximate e^{k(u-t)/t} with 2^{k(u-t)/(t ln(2))}
	// 1/ln(2) is approx. 1,442695

	switch {
	case unitsConsumed > target:
		exp := float64(1.442695) * float64(updateCoefficient*(unitsConsumed-target)) / float64(target)
		intExp := min(uint64(math.Ceil(exp)), 62) // we cap the exponent to avoid an overflow of uint64 type
		res, over := safemath.Mul64(currentUnitFee, 1<<intExp)
		if over != nil {
			return math.MaxUint64
		}
		return res

	case unitsConsumed < target:
		exp := float64(1.442695) * float64(updateCoefficient*(target-unitsConsumed)) / float64(target)
		intExp := min(uint64(math.Ceil(exp)), 62) // we cap the exponent to avoid an overflow of uint64 type
		return currentUnitFee / (1 << intExp)

	default:
		return currentUnitFee // unitsConsumed == target
	}
}

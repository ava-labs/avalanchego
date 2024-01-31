// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package fees

import (
	"fmt"
	"math"

	safemath "github.com/ava-labs/avalanchego/utils/math"
)

// Fees are not dynamic yet. We'll add mechanism for fee updating iterativelly

type Manager struct {
	// Avax denominated unit fees for all fee dimensions
	unitFees Dimensions

	// cunsumed units window per each fee dimension.
	windows [FeeDimensions]Window

	lastConsumed Dimensions

	// cumulatedUnits helps aggregating the units consumed by a block
	// so that we can verify it's not too big/build it properly.
	cumulatedUnits Dimensions
}

func NewManager(unitFees Dimensions) *Manager {
	return &Manager{
		unitFees: unitFees,
	}
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

func (m *Manager) GetCumulatedUnits() Dimensions {
	return m.cumulatedUnits
}

func (m *Manager) ComputeNext(
	lastTime,
	currTime int64,
	targetUnits,
	priceChangeDenominator,
	minUnitPrice Dimensions,
) (*Manager, error) {
	since := int((currTime - lastTime) / 1000 /*milliseconds per second*/)
	nextManager := &Manager{}
	for i := Bandwidth; i < FeeDimensions; i++ {
		nextUnitPrice, nextUnitWindow := computeNextPriceWindow(
			m.windows[i],
			m.lastConsumed[i],
			m.unitFees[i],
			targetUnits[i],
			priceChangeDenominator[i],
			minUnitPrice[i],
			since,
		)

		nextManager.unitFees[i] = nextUnitPrice
		nextManager.windows[i] = nextUnitWindow

		// TODO ABENEGIA: how about last consumed

		// start := dimensionStateLen * i
		// binary.BigEndian.PutUint64(bytes[start:start+consts.Uint64Len], nextUnitPrice)
		// copy(bytes[start+consts.Uint64Len:start+consts.Uint64Len+WindowSliceSize], nextUnitWindow[:])
		// // Usage must be set after block is processed (we leave as 0 for now)
	}
	return nextManager, nil
}

func computeNextPriceWindow(
	previous Window,
	previousConsumed uint64,
	previousPrice uint64,
	target uint64, /* per window */
	changeDenom uint64,
	minPrice uint64,
	since int, /* seconds */
) (uint64, Window) {
	newRollupWindow := Roll(previous, since)
	if since < WindowSize {
		// add in the units used by the parent block in the correct place
		// If the parent consumed units within the rollup window, add the consumed
		// units in.
		start := WindowSize - 1 - since
		Update(&newRollupWindow, start, previousConsumed)
	}
	total := Sum(newRollupWindow)

	nextPrice := previousPrice
	switch {
	case total == target:
		return nextPrice, newRollupWindow
	case total > target:
		// If the parent block used more units than its target, the baseFee should increase.
		delta := total - target
		x := previousPrice * delta
		y := x / target
		baseDelta := y / changeDenom
		if baseDelta < 1 {
			baseDelta = 1
		}
		n, over := safemath.Add64(nextPrice, baseDelta)
		if over != nil {
			nextPrice = math.MaxUint64
		} else {
			nextPrice = n
		}
	case total < target:
		// Otherwise if the parent block used less units than its target, the baseFee should decrease.
		delta := target - total
		x := previousPrice * delta
		y := x / target
		baseDelta := y / changeDenom
		if baseDelta < 1 {
			baseDelta = 1
		}

		// If [roll] is greater than [rollupWindow], apply the state transition to the base fee to account
		// for the interval during which no blocks were produced.
		// We use roll/rollupWindow, so that the transition is applied for every [rollupWindow] seconds
		// that has elapsed between the parent and this block.
		if since > WindowSize {
			// Note: roll/rollupWindow must be greater than 1 since we've checked that roll > rollupWindow
			baseDelta *= uint64(since / WindowSize)
		}
		n, under := safemath.Sub(nextPrice, baseDelta)
		if under != nil {
			nextPrice = 0
		} else {
			nextPrice = n
		}
	}
	nextPrice = safemath.Max(nextPrice, minPrice)
	return nextPrice, newRollupWindow
}

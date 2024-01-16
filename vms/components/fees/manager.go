// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package fees

import (
	"fmt"

	"github.com/ava-labs/avalanchego/utils/math"
)

// Fees are not dynamic yet. We'll add mechanism for fee updating iterativelly

const (
	Bandwidth Dimension = 0
	UTXORead  Dimension = 1
	UTXOWrite Dimension = 2 // includes delete
	Compute   Dimension = 3 // any other cost, tx-specific

	FeeDimensions = 4
)

type (
	Dimension  int
	Dimensions [FeeDimensions]uint64
)

func Add(lhs, rhs Dimensions) (Dimensions, error) {
	var res Dimensions
	for i := 0; i < FeeDimensions; i++ {
		v, err := math.Add64(lhs[i], rhs[i])
		if err != nil {
			return res, err
		}
		res[i] = v
	}
	return res, nil
}

type Manager struct {
	// Avax denominated unit fees for all fee dimensions
	unitFees Dimensions

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
		contribution, err := math.Mul64(m.unitFees[i], units[i])
		if err != nil {
			return 0, err
		}
		fee, err = math.Add64(contribution, fee)
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
		consumed, err := math.Add64(m.cumulatedUnits[i], units[i])
		if err != nil {
			return true, i
		}
		if consumed > bounds[i] {
			return true, i
		}
	}

	// Commit to consumption
	for i := Dimension(0); i < FeeDimensions; i++ {
		consumed, err := math.Add64(m.cumulatedUnits[i], units[i])
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
		prev, err := math.Sub(m.cumulatedUnits[i], unitsToRm[i])
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

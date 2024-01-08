// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package fees

import "github.com/ava-labs/avalanchego/utils/math"

// 1. Fees are not dynamic yet. We'll add mechanism for fee updating iterativelly
// 2. Not all fees dimensions are correctly prices. We'll add other dimensions iteratively

const (
	Bandwidth Dimension = 0
	// Compute   Dimension = 1
	// UTXORead  Dimension = 2
	// UTXOWrite Dimension = 3 // includes delete

	FeeDimensions = 4
)

type (
	Dimension  int
	Dimensions [FeeDimensions]uint64
)

type Manager struct {
	// Avax prices per units for all fee dimensions
	unitPrices Dimensions

	// cumulatedUnits helps aggregating the units consumed by a block
	// so that we can verify it's not too big/build it properly.
	cumulatedUnits Dimensions
}

func NewManager(unitPrices Dimensions) *Manager {
	return &Manager{
		unitPrices: unitPrices,
	}
}

func (m *Manager) CalculateFee(units Dimensions) (uint64, error) {
	fee := uint64(0)

	for i := Dimension(0); i < FeeDimensions; i++ {
		contribution, err := math.Mul64(m.unitPrices[i], units[i])
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
			return false, i
		}
		if consumed > bounds[i] {
			return false, i
		}
	}

	// Commit to consumption
	for i := Dimension(0); i < FeeDimensions; i++ {
		consumed, err := math.Add64(m.cumulatedUnits[i], units[i])
		if err != nil {
			return false, i
		}
		m.cumulatedUnits[i] = consumed
	}
	return true, 0
}

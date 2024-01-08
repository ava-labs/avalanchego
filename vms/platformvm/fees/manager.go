// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package fees

import "github.com/ava-labs/avalanchego/utils/math"

const (
	Bandwidth Dimension = 0
	// Compute   Dimension = 1 // TODO ABENEGIA: we'll add others interatively
	UTXORead  Dimension = 1
	UTXOWrite Dimension = 2 // includes delete

	FeeDimensions = 3
)

type (
	Dimension  int
	Dimensions [FeeDimensions]uint64
)

type Manager struct {
	// Avax prices per units for all fee dimensions
	unitPrices Dimensions

	// cumulatedUnits helps aggregating the units consumed by a block
	// so that we can verify it's not too big/build it properly
	cumulatedUnits Dimensions
}

func NewManager(initialUnitPrices Dimensions) *Manager {
	return &Manager{
		unitPrices: initialUnitPrices,
	}
}

func (m *Manager) UnitPrices() Dimensions {
	var d Dimensions
	for i := Dimension(0); i < FeeDimensions; i++ {
		d[i] = m.unitPrices[i]
	}
	return d
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

// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package fees

import (
	"errors"
	"fmt"

	safemath "github.com/ava-labs/avalanchego/utils/math"
)

var errGasBoundBreached = errors.New("gas bound breached")

type Manager struct {
	// Avax denominated gas price, i.e. fee per unit of complexity.
	gasPrice GasPrice

	// cumulatedGas helps aggregating the gas consumed
	// by a block so that we can verify it's not too big/build it properly.
	cumulatedGas Gas
}

func NewManager(gasPrice GasPrice) *Manager {
	return &Manager{
		gasPrice: gasPrice,
	}
}

func (m *Manager) GetGasPrice() GasPrice {
	return m.gasPrice
}

func (m *Manager) GetGas() Gas {
	return m.cumulatedGas
}

// CalculateFee must be a stateless method
func (m *Manager) CalculateFee(g Gas) (uint64, error) {
	return safemath.Mul64(uint64(m.gasPrice), uint64(g))
}

// CumulateComplexity tries to cumulate the consumed complexity [units]. Before
// actually cumulating them, it checks whether the result would breach [bounds].
// If so, it returns the first dimension to breach bounds.
func (m *Manager) CumulateComplexity(gas, bound Gas) error {
	// Ensure we can consume (don't want partial update of values)
	consumed, err := safemath.Add64(uint64(m.gasPrice), uint64(gas))
	if err != nil {
		return fmt.Errorf("%w: %w", errGasBoundBreached, err)
	}
	if Gas(consumed) > bound {
		return errGasBoundBreached
	}

	m.cumulatedGas = Gas(consumed)
	return nil
}

// Sometimes, e.g. while building a tx, we'd like freedom to speculatively add complexity
// and to remove it later on. [RemoveComplexity] grants this freedom
func (m *Manager) RemoveComplexity(gasToRm Gas) error {
	revertedGas, err := safemath.Sub(m.cumulatedGas, gasToRm)
	if err != nil {
		return fmt.Errorf("%w: current Gas %d, gas to revert %d", err, m.cumulatedGas, gasToRm)
	}
	m.cumulatedGas = revertedGas
	return nil
}

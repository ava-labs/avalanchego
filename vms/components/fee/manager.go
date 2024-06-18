// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package fee

import (
	"errors"
	"fmt"

	safemath "github.com/ava-labs/avalanchego/utils/math"
)

var errGasBoundBreached = errors.New("gas bound breached")

type Manager struct {
	// gas cap enforced with adding gas via CumulateGas
	gasCap Gas

	// Avax denominated gas price, i.e. fee per unit of complexity.
	gasPrice GasPrice

	// blockGas helps aggregating the gas consumed in a single block
	// so that we can verify it's not too big/build it properly.
	blockGas Gas
}

func NewManager(gasPrice GasPrice, gasCap Gas) *Manager {
	return &Manager{
		gasCap:   gasCap,
		gasPrice: gasPrice,
	}
}

func (m *Manager) GetGasPrice() GasPrice {
	return m.gasPrice
}

func (m *Manager) GetBlockGas() Gas {
	return m.blockGas
}

// CalculateFee must be a stateless method
func (m *Manager) CalculateFee(g Gas) (uint64, error) {
	return safemath.Mul64(uint64(m.gasPrice), uint64(g))
}

// CumulateGas tries to cumulate the consumed gas [units]. Before
// actually cumulating it, it checks whether the result would breach [bounds].
// If so, it returns the first dimension to breach bounds.
func (m *Manager) CumulateGas(gas Gas) error {
	// Ensure we can consume (don't want partial update of values)
	blkGas, err := safemath.Add64(uint64(m.blockGas), uint64(gas))
	if err != nil {
		return fmt.Errorf("%w: %w", errGasBoundBreached, err)
	}
	if Gas(blkGas) > m.gasCap {
		return errGasBoundBreached
	}

	m.blockGas = Gas(blkGas)
	return nil
}

// Sometimes, e.g. while building a tx, we'd like freedom to speculatively
// add gas and to remove it later on. [RemoveGas] grants this freedom
func (m *Manager) RemoveGas(gasToRm Gas) error {
	rBlkdGas, err := safemath.Sub(m.blockGas, gasToRm)
	if err != nil {
		return fmt.Errorf("%w: current Gas %d, gas to revert %d", err, m.blockGas, gasToRm)
	}

	m.blockGas = rBlkdGas
	return nil
}

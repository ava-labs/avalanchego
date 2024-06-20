// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package fee

import (
	"errors"
	"fmt"

	safemath "github.com/ava-labs/avalanchego/utils/math"
)

var errGasBoundBreached = errors.New("gas bound breached")

// Calculator performs fee-related operations that are share move P-chain and X-chain
// Calculator is supposed to be embedded with chain specific calculators.
type Calculator struct {
	// gas cap enforced with adding gas via CumulateGas
	gasCap Gas

	// Avax denominated gas price, i.e. fee per unit of complexity.
	gasPrice GasPrice

	// blockGas helps aggregating the gas consumed in a single block
	// so that we can verify it's not too big/build it properly.
	blockGas Gas
}

func NewCalculator(gasPrice GasPrice, gasCap Gas) *Calculator {
	return &Calculator{
		gasCap:   gasCap,
		gasPrice: gasPrice,
	}
}

func (c *Calculator) GetGasPrice() GasPrice {
	return c.gasPrice
}

func (c *Calculator) GetBlockGas() Gas {
	return c.blockGas
}

func (c *Calculator) GetGasCap() Gas {
	return c.gasCap
}

// CalculateFee must be a stateless method
func (c *Calculator) CalculateFee(g Gas) (uint64, error) {
	return safemath.Mul64(uint64(c.gasPrice), uint64(g))
}

// CumulateGas tries to cumulate the consumed gas [units]. Before
// actually cumulating it, it checks whether the result would breach [bounds].
// If so, it returns the first dimension to breach bounds.
func (c *Calculator) CumulateGas(gas Gas) error {
	// Ensure we can consume (don't want partial update of values)
	blkGas, err := safemath.Add64(uint64(c.blockGas), uint64(gas))
	if err != nil {
		return fmt.Errorf("%w: %w", errGasBoundBreached, err)
	}
	if Gas(blkGas) > c.gasCap {
		return errGasBoundBreached
	}

	c.blockGas = Gas(blkGas)
	return nil
}

// Sometimes, e.g. while building a tx, we'd like freedom to speculatively
// add gas and to remove it later on. [RemoveGas] grants this freedom
func (c *Calculator) RemoveGas(gasToRm Gas) error {
	rBlkdGas, err := safemath.Sub(c.blockGas, gasToRm)
	if err != nil {
		return fmt.Errorf("%w: current Gas %d, gas to revert %d", err, c.blockGas, gasToRm)
	}

	c.blockGas = rBlkdGas
	return nil
}

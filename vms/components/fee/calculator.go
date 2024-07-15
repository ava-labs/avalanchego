// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package fee

import (
	"errors"
	"fmt"

	safemath "github.com/ava-labs/avalanchego/utils/math"
)

var errGasBoundBreached = errors.New("gas bound breached")

// Calculator performs fee-related operations that are shared among P-chain and X-chain
// Calculator is supposed to be embedded within chain specific calculators.
type Calculator struct {
	// feeWeights help consolidating complexity into gas
	feeWeights Dimensions

	// gas cap enforced with adding gas via AddFeesFor
	gasCap Gas

	// Avax denominated gas price, i.e. fee per unit of gas.
	gasPrice GasPrice

	// cumulatedGas helps aggregating the gas consumed in a single block
	// so that we can verify it's not too big/build it properly.
	cumulatedGas Gas

	// latestTxComplexity tracks complexity of latest tx being processed.
	// latestTxComplexity is especially helpful while building a tx.
	latestTxComplexity Dimensions
}

func NewCalculator(feeWeights Dimensions, gasPrice GasPrice, gasCap Gas) *Calculator {
	return &Calculator{
		feeWeights: feeWeights,
		gasCap:     gasCap,
		gasPrice:   gasPrice,
	}
}

func (c *Calculator) GetGasPrice() GasPrice {
	return c.gasPrice
}

func (c *Calculator) GetBlockGas() (Gas, error) {
	txGas, err := ToGas(c.feeWeights, c.latestTxComplexity)
	if err != nil {
		return ZeroGas, err
	}
	return c.cumulatedGas + txGas, nil
}

func (c *Calculator) GetGasCap() Gas {
	return c.gasCap
}

// AddFeesFor updates latest tx complexity. It should be called once when tx is being verified
// and may be called multiple times when tx is being built (and tx components are added in time).
// AddFeesFor checks that gas cap is not breached. It also returns the updated tx fee for convenience.
func (c *Calculator) AddFeesFor(complexity Dimensions) (uint64, error) {
	if complexity == Empty {
		return c.GetLatestTxFee()
	}

	// Ensure we can consume (don't want partial update of values)
	uc, err := Add(c.latestTxComplexity, complexity)
	if err != nil {
		return 0, fmt.Errorf("%w: %w", errGasBoundBreached, err)
	}
	c.latestTxComplexity = uc

	totalGas, err := c.GetBlockGas()
	if err != nil {
		return 0, fmt.Errorf("%w: %w", errGasBoundBreached, err)
	}
	if totalGas > c.gasCap {
		return 0, fmt.Errorf("%w: %w", errGasBoundBreached, err)
	}

	return c.GetLatestTxFee()
}

// Sometimes, e.g. while building a tx, we'd like freedom to speculatively add complexity
// and to remove it later on. [RemoveFeesFor] grants this freedom
func (c *Calculator) RemoveFeesFor(complexity Dimensions) (uint64, error) {
	if complexity == Empty {
		return c.GetLatestTxFee()
	}

	rc, err := Remove(c.latestTxComplexity, complexity)
	if err != nil {
		return 0, fmt.Errorf("%w: current Gas %d, gas to revert %d", err, c.cumulatedGas, complexity)
	}
	c.latestTxComplexity = rc
	return c.GetLatestTxFee()
}

// DoneWithLatestTx should be invoked one a tx has been fully processed, before moving to the next one
func (c *Calculator) DoneWithLatestTx() error {
	txGas, err := ToGas(c.feeWeights, c.latestTxComplexity)
	if err != nil {
		return err
	}
	c.cumulatedGas += txGas
	c.latestTxComplexity = Empty
	return nil
}

// CalculateFee must be a stateless method
func (c *Calculator) GetLatestTxFee() (uint64, error) {
	gas, err := ToGas(c.feeWeights, c.latestTxComplexity)
	if err != nil {
		return 0, err
	}
	return safemath.Mul64(uint64(c.gasPrice), uint64(gas))
}

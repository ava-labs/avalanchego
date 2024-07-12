// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package fee

import (
	"errors"
	"fmt"
	"math"
	"math/big"
	"time"

	safemath "github.com/ava-labs/avalanchego/utils/math"
)

var errGasBoundBreached = errors.New("gas bound breached")

// Calculator performs fee-related operations that are share move P-chain and X-chain
// Calculator is supposed to be embedded with chain specific calculators.
type Calculator struct {
	// feeWeights help consolidating complexity into gas
	feeWeights Dimensions

	// gas cap enforced with adding gas via CumulateGas
	gasCap Gas

	// Avax denominated gas price, i.e. fee per unit of gas.
	gasPrice GasPrice

	// cumulatedGas helps aggregating the gas consumed in a single block
	// so that we can verify it's not too big/build it properly.
	cumulatedGas Gas

	// latestTxComplexity tracks complexity of latest tx being processed.
	// latestTxComplexity is especially helpful while building a tx.
	latestTxComplexity Dimensions

	// currentExcessGas stores current excess gas, cumulated over time
	// to be updated once a block is accepted with cumulatedGas
	currentExcessGas Gas
}

func NewCalculator(feeWeights Dimensions, gasPrice GasPrice, gasCap Gas) *Calculator {
	return &Calculator{
		feeWeights: feeWeights,
		gasCap:     gasCap,
		gasPrice:   gasPrice,
	}
}

func NewUpdatedManager(
	feesConfig DynamicFeesConfig,
	gasCap, currentExcessGas Gas,
	parentBlkTime, childBlkTime time.Time,
) (*Calculator, error) {
	res := &Calculator{
		feeWeights:       feesConfig.FeeDimensionWeights,
		gasCap:           gasCap,
		currentExcessGas: currentExcessGas,
	}

	targetGas, err := TargetGas(feesConfig, parentBlkTime, childBlkTime)
	if err != nil {
		return nil, fmt.Errorf("failed calculating target gas: %w", err)
	}

	if currentExcessGas > targetGas {
		currentExcessGas -= targetGas
	} else {
		currentExcessGas = ZeroGas
	}

	res.gasPrice = fakeExponential(feesConfig.MinGasPrice, currentExcessGas, feesConfig.UpdateDenominator)
	return res, nil
}

func TargetGas(feesConfig DynamicFeesConfig, parentBlkTime, childBlkTime time.Time) (Gas, error) {
	if parentBlkTime.Compare(childBlkTime) > 0 {
		return ZeroGas, fmt.Errorf("unexpected block times, parentBlkTim %v, childBlkTime %v", parentBlkTime, childBlkTime)
	}

	elapsedTime := uint64(childBlkTime.Unix() - parentBlkTime.Unix())
	targetGas, over := safemath.Mul64(uint64(feesConfig.GasTargetRate), elapsedTime)
	if over != nil {
		targetGas = math.MaxUint64
	}
	return Gas(targetGas), nil
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

func (c *Calculator) GetExcessGas() (Gas, error) {
	g, err := safemath.Add64(uint64(c.currentExcessGas), uint64(c.cumulatedGas))
	if err != nil {
		return ZeroGas, err
	}
	return Gas(g), nil
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

// fakeExponential approximates factor * e ** (numerator / denominator) using
// Taylor expansion.
func fakeExponential(f GasPrice, n, d Gas) GasPrice {
	var (
		factor      = new(big.Int).SetUint64(uint64(f))
		numerator   = new(big.Int).SetUint64(uint64(n))
		denominator = new(big.Int).SetUint64(uint64(d))
		output      = new(big.Int)
		accum       = new(big.Int).Mul(factor, denominator)
	)
	for i := 1; accum.Sign() > 0; i++ {
		output.Add(output, accum)

		accum.Mul(accum, numerator)
		accum.Div(accum, denominator)
		accum.Div(accum, big.NewInt(int64(i)))
	}
	output = output.Div(output, denominator)
	if !output.IsUint64() {
		return math.MaxUint64
	}
	return GasPrice(output.Uint64())
}

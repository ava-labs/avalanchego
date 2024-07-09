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

var (
	errGasBoundBreached                   = errors.New("gas bound breached")
	errRequestedGasBlockWhileProcessingTx = errors.New("requested gas block while processing tx")
)

// Calculator performs fee-related operations that are share move P-chain and X-chain
// Calculator is supposed to be embedded with chain specific calculators.
type Calculator struct {
	// feeWeights help consolidating complexity into gas
	feeWeights Dimensions

	// gas cap enforced with adding gas via CumulateGas
	gasCap Gas

	// Avax denominated gas price, i.e. fee per unit of complexity.
	gasPrice GasPrice

	// blkGas helps aggregating the gas consumed in a single block
	// so that we can verify it's not too big/build it properly.
	blkGas Gas

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
	if c.latestTxComplexity != Empty {
		return ZeroGas, errRequestedGasBlockWhileProcessingTx
	}
	return c.blkGas, nil
}

func (c *Calculator) GetGasCap() Gas {
	return c.gasCap
}

func (c *Calculator) GetExcessGas() (Gas, error) {
	g, err := safemath.Add64(uint64(c.currentExcessGas), uint64(c.blkGas))
	if err != nil {
		return ZeroGas, err
	}
	return Gas(g), nil
}

// CumulateComplexity tries to cumulate the consumed gas [units]. Before
// actually cumulating it, it checks whether the result would breach [bounds].
// If so, it returns the first dimension to breach bounds.
func (c *Calculator) CumulateComplexity(complexity Dimensions) error {
	// Ensure we can consume (don't want partial update of values)
	uc, err := Add(c.latestTxComplexity, complexity)
	if err != nil {
		return fmt.Errorf("%w: %w", errGasBoundBreached, err)
	}
	c.latestTxComplexity = uc
	return nil
}

// Sometimes, e.g. while building a tx, we'd like freedom to speculatively add complexity
// and to remove it later on. [RemoveGas] grants this freedom
func (c *Calculator) RemoveComplexity(complexity Dimensions) error {
	rc, err := Remove(c.latestTxComplexity, complexity)
	if err != nil {
		return fmt.Errorf("%w: current Gas %d, gas to revert %d", err, c.blkGas, complexity)
	}
	c.latestTxComplexity = rc
	return nil
}

// DoneWithLatestTx should be invoked one a tx has been fully processed, before moving to the next one
func (c *Calculator) DoneWithLatestTx() error {
	txGas, err := ToGas(c.feeWeights, c.latestTxComplexity)
	if err != nil {
		return err
	}
	c.blkGas += txGas
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

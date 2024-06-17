// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package fees

import (
	"errors"
	"fmt"
	"math"
	"math/big"

	safemath "github.com/ava-labs/avalanchego/utils/math"
)

var errGasBoundBreached = errors.New("gas bound breached")

type Manager struct {
	// Avax denominated gas price, i.e. fee per unit of complexity.
	gasPrice GasPrice

	// cumulatedGas helps aggregating the gas consumed
	// by a block so that we can verify it's not too big/build it properly.
	cumulatedGas Gas

	// currentExcessGas stores current excess gas, cumulated over time
	// to be updated once a block is accepted with cumulatedGas
	currentExcessGas Gas
}

func NewManager(gasPrice GasPrice) *Manager {
	return &Manager{
		gasPrice: gasPrice,
	}
}

func NewUpdatedManager(
	feesConfig DynamicFeesConfig,
	excessGas Gas,
	parentBlkTime, childBlkTime int64,
) (*Manager, error) {
	res := &Manager{
		currentExcessGas: excessGas,
	}

	targetGas, err := TargetGas(feesConfig, parentBlkTime, childBlkTime)
	if err != nil {
		return nil, fmt.Errorf("failed calculating target block gas: %w", err)
	}

	if excessGas > targetGas {
		excessGas -= targetGas
	} else {
		excessGas = ZeroGas
	}

	res.gasPrice = fakeExponential(feesConfig.MinGasPrice, excessGas, feesConfig.UpdateDenominator)
	return res, nil
}

func TargetGas(feesConfig DynamicFeesConfig, parentBlkTime, childBlkTime int64) (Gas, error) {
	if childBlkTime < parentBlkTime {
		return ZeroGas, fmt.Errorf("unexpected block times, parentBlkTim %v, childBlkTime %v", parentBlkTime, childBlkTime)
	}

	elapsedTime := uint64(childBlkTime - parentBlkTime)
	targetGas, over := safemath.Mul64(uint64(feesConfig.GasTargetRate), elapsedTime)
	if over != nil {
		targetGas = math.MaxUint64
	}
	return Gas(targetGas), nil
}

func (m *Manager) GetGasPrice() GasPrice {
	return m.gasPrice
}

func (m *Manager) GetGas() Gas {
	return m.cumulatedGas
}

func (m *Manager) GetCurrentExcessComplexity() Gas {
	return m.currentExcessGas
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
	consumed, err := safemath.Add64(uint64(m.cumulatedGas), uint64(gas))
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

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

	// blockGas helps aggregating the gas consumed in a single block
	// so that we can verify it's not too big/build it properly.
	blockGas Gas

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
	currentExcessGas Gas,
	parentBlkTime, childBlkTime int64,
) (*Manager, error) {
	res := &Manager{
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

func (m *Manager) GetBlockGas() Gas {
	return m.blockGas
}

func (m *Manager) GetExcessGas() Gas {
	return m.currentExcessGas
}

// CalculateFee must be a stateless method
func (m *Manager) CalculateFee(g Gas) (uint64, error) {
	return safemath.Mul64(uint64(m.gasPrice), uint64(g))
}

// CumulateGas tries to cumulate the consumed gas [units]. Before
// actually cumulating it, it checks whether the result would breach [bounds].
// If so, it returns the first dimension to breach bounds.
func (m *Manager) CumulateGas(gas, bound Gas) error {
	// Ensure we can consume (don't want partial update of values)
	blkGas, err := safemath.Add64(uint64(m.blockGas), uint64(gas))
	if err != nil {
		return fmt.Errorf("%w: %w", errGasBoundBreached, err)
	}
	if Gas(blkGas) > bound {
		return errGasBoundBreached
	}

	excessGas, err := safemath.Add64(uint64(m.currentExcessGas), uint64(gas))
	if err != nil {
		return fmt.Errorf("%w: %w", errGasBoundBreached, err)
	}

	m.blockGas = Gas(blkGas)
	m.currentExcessGas = Gas(excessGas)
	return nil
}

// Sometimes, e.g. while building a tx, we'd like freedom to speculatively add complexity
// and to remove it later on. [RemoveGas] grants this freedom
func (m *Manager) RemoveGas(gasToRm Gas) error {
	rBlkdGas, err := safemath.Sub(m.blockGas, gasToRm)
	if err != nil {
		return fmt.Errorf("%w: current Gas %d, gas to revert %d", err, m.blockGas, gasToRm)
	}
	rExcessGas, err := safemath.Sub(m.currentExcessGas, gasToRm)
	if err != nil {
		return fmt.Errorf("%w: current Excess gas %d, gas to revert %d", err, m.currentExcessGas, gasToRm)
	}

	m.blockGas = rBlkdGas
	m.currentExcessGas = rExcessGas
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

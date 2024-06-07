// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package fees

import (
	"fmt"
	"math"
	"math/big"

	safemath "github.com/ava-labs/avalanchego/utils/math"
)

type Manager struct {
	// Avax denominated fee rates, i.e. fees per unit of complexity.
	feeRates Dimensions

	currentExcessComplexity Dimensions
}

func NewManager(feeRate Dimensions) *Manager {
	return &Manager{
		feeRates: feeRate,
	}
}

func NewUpdatedManager(
	feesConfig DynamicFeesConfig,
	excessComplexity Dimensions,
	parentBlkTime, childBlkTime int64,
) (*Manager, error) {
	res := &Manager{
		currentExcessComplexity: excessComplexity,
	}

	targetBlkComplexity, err := TargetBlockComplexity(feesConfig, parentBlkTime, childBlkTime)
	if err != nil {
		return nil, fmt.Errorf("failed calculating target block complexity: %w", err)
	}
	for i := Dimension(0); i < FeeDimensions; i++ {
		if excessComplexity[i] > targetBlkComplexity[i] {
			excessComplexity[i] -= targetBlkComplexity[i]
		} else {
			excessComplexity[i] = 0
		}

		res.feeRates[i] = fakeExponential(feesConfig.MinFeeRate[i], excessComplexity[i], feesConfig.UpdateDenominators[i])
	}
	return res, nil
}

func TargetBlockComplexity(feesConfig DynamicFeesConfig, parentBlkTime, childBlkTime int64) (Dimensions, error) {
	res := Empty

	if childBlkTime < parentBlkTime {
		return res, fmt.Errorf("unexpected block times, parentBlkTim %v, childBlkTime %v", parentBlkTime, childBlkTime)
	}

	elapsedTime := uint64(childBlkTime - parentBlkTime)
	for i := Dimension(0); i < FeeDimensions; i++ {
		targetComplexity, over := safemath.Mul64(feesConfig.BlockTargetComplexityRate[i], elapsedTime)
		if over != nil {
			targetComplexity = math.MaxUint64
		}
		res[i] = targetComplexity
	}
	return res, nil
}

func (m *Manager) GetFeeRates() Dimensions {
	return m.feeRates
}

func (m *Manager) GetCurrentExcessComplexity() Dimensions {
	return m.currentExcessComplexity
}

// CalculateFee must be a stateless method
func (m *Manager) CalculateFee(units Dimensions) (uint64, error) {
	fee := uint64(0)

	for i := Dimension(0); i < FeeDimensions; i++ {
		contribution, err := safemath.Mul64(m.feeRates[i], units[i])
		if err != nil {
			return 0, err
		}
		fee, err = safemath.Add64(contribution, fee)
		if err != nil {
			return 0, err
		}
	}
	return fee, nil
}

// CumulateComplexity tries to cumulate the consumed complexity [units]. Before
// actually cumulating them, it checks whether the result would breach [bounds].
// If so, it returns the first dimension to breach bounds.
func (m *Manager) CumulateComplexity(units, bounds Dimensions) (bool, Dimension) {
	// Ensure we can consume (don't want partial update of values)
	for i := Dimension(0); i < FeeDimensions; i++ {
		consumed, err := safemath.Add64(m.currentExcessComplexity[i], units[i])
		if err != nil {
			return true, i
		}
		if consumed > bounds[i] {
			return true, i
		}
	}

	// Commit to consumption
	for i := Dimension(0); i < FeeDimensions; i++ {
		consumed, err := safemath.Add64(m.currentExcessComplexity[i], units[i])
		if err != nil {
			return true, i
		}
		m.currentExcessComplexity[i] = consumed
	}
	return false, 0
}

// Sometimes, e.g. while building a tx, we'd like freedom to speculatively add complexity
// and to remove it later on. [RemoveComplexity] grants this freedom
func (m *Manager) RemoveComplexity(unitsToRm Dimensions) error {
	var revertedUnits Dimensions
	for i := Dimension(0); i < FeeDimensions; i++ {
		prev, err := safemath.Sub(m.currentExcessComplexity[i], unitsToRm[i])
		if err != nil {
			return fmt.Errorf("%w: dimension %d", err, i)
		}
		revertedUnits[i] = prev
	}

	m.currentExcessComplexity = revertedUnits
	return nil
}

// fakeExponential approximates factor * e ** (numerator / denominator) using
// Taylor expansion.
func fakeExponential(f, n, d uint64) uint64 {
	var (
		factor      = new(big.Int).SetUint64(f)
		numerator   = new(big.Int).SetUint64(n)
		denominator = new(big.Int).SetUint64(d)
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
	return output.Uint64()
}

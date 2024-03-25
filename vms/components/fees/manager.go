// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package fees

import (
	"fmt"
	"math"

	safemath "github.com/ava-labs/avalanchego/utils/math"
)

type Manager struct {
	// Avax denominated fee rates, i.e. fees per unit of complexity.
	feeRates Dimensions

	// cumulatedComplexity helps aggregating the units of complexity consumed
	// by a block so that we can verify it's not too big/build it properly.
	cumulatedComplexity Dimensions
}

func NewManager(feeRate Dimensions) *Manager {
	return &Manager{
		feeRates: feeRate,
	}
}

func (m *Manager) GetFeeRates() Dimensions {
	return m.feeRates
}

func (m *Manager) GetCumulatedComplexity() Dimensions {
	return m.cumulatedComplexity
}

// CalculateFee must be a stateless method
// tipPercentage is assumed to have been validated
func (m *Manager) CalculateFee(complexity Dimensions, tipPercentage TipPercentage) (uint64, error) {
	var (
		baseFee = uint64(0)
		tip     = uint64(0)
	)

	for i := Dimension(0); i < FeeDimensions; i++ {
		baseAddend, err := safemath.Mul64(m.feeRates[i], complexity[i])
		if err != nil {
			return 0, err
		}
		baseFee, err = safemath.Add64(baseAddend, baseFee)
		if err != nil {
			return 0, err
		}

		tipAddend, err := safemath.Mul64(baseFee, uint64(tipPercentage))
		if err != nil {
			tipAddend, err = safemath.Mul64(baseFee/uint64(MaxTipPercentage), uint64(tipPercentage))
			if err != nil {
				return 0, err
			}
			tip, err = safemath.Add64(tip, tipAddend)
			if err != nil {
				return 0, err
			}
		} else {
			tip, err = safemath.Add64(tip, tipAddend/MaxTipPercentage)
			if err != nil {
				return 0, err
			}
		}
	}

	return safemath.Add64(baseFee, tip)
}

// CumulateComplexity tries to cumulate the consumed complexity [units]. Before
// actually cumulating them, it checks whether the result would breach [bounds].
// If so, it returns the first dimension to breach bounds.
func (m *Manager) CumulateComplexity(units, bounds Dimensions) (bool, Dimension) {
	// Ensure we can consume (don't want partial update of values)
	for i := Dimension(0); i < FeeDimensions; i++ {
		consumed, err := safemath.Add64(m.cumulatedComplexity[i], units[i])
		if err != nil {
			return true, i
		}
		if consumed > bounds[i] {
			return true, i
		}
	}

	// Commit to consumption
	for i := Dimension(0); i < FeeDimensions; i++ {
		consumed, err := safemath.Add64(m.cumulatedComplexity[i], units[i])
		if err != nil {
			return true, i
		}
		m.cumulatedComplexity[i] = consumed
	}
	return false, 0
}

// Sometimes, e.g. while building a tx, we'd like freedom to speculatively add complexity
// and to remove it later on. [RemoveComplexity] grants this freedom
func (m *Manager) RemoveComplexity(unitsToRm Dimensions) error {
	var revertedUnits Dimensions
	for i := Dimension(0); i < FeeDimensions; i++ {
		prev, err := safemath.Sub(m.cumulatedComplexity[i], unitsToRm[i])
		if err != nil {
			return fmt.Errorf("%w: dimension %d", err, i)
		}
		revertedUnits[i] = prev
	}

	m.cumulatedComplexity = revertedUnits
	return nil
}

func (m *Manager) ResetComplexity() {
	m.cumulatedComplexity = Empty
}

func (m *Manager) UpdateFeeRates(
	feesConfig DynamicFeesConfig,
	parentBlkComplexity Dimensions,
	parentBlkTime, childBlkTime int64,
) error {
	if childBlkTime < parentBlkTime {
		return fmt.Errorf("unexpected block times, parentBlkTim %v, childBlkTime %v", parentBlkTime, childBlkTime)
	}

	elapsedTime := uint64(childBlkTime - parentBlkTime)
	for i := Dimension(0); i < FeeDimensions; i++ {
		nextFeeRates := nextFeeRate(
			m.feeRates[i],
			feesConfig.UpdateCoefficient[i],
			parentBlkComplexity[i],
			feesConfig.BlockTargetComplexityRate[i],
			elapsedTime,
		)
		nextFeeRates = max(nextFeeRates, feesConfig.MinFeeRate[i])
		m.feeRates[i] = nextFeeRates
	}
	return nil
}

func nextFeeRate(currentFeeRate, coeff, parentBlkComplexity, targetComplexityRate, elapsedTime uint64) uint64 {
	// We update the fee rate with the formula:
	// feeRate_{t+1} = feeRate_t * exp{coeff * (parentComplexity - targetComplexity)/(targetComplexity) }
	// where [targetComplexity] is the complexity expected in the elapsed time.
	//
	// We simplify the exponential for integer math. Specifically we approximate 1/ln(2) with 1_442/1_000
	// so that exp { x } == 2^{ 1/ln(2) * x } ≈≈ 2^{1_442/1_000 * x}
	// Finally we round the exponent to a uint64

	// parent and child block may have the same timestamp. In this case targetComplexity will match targetComplexityRate
	elapsedTime = max(1, elapsedTime)
	targetComplexity, over := safemath.Mul64(targetComplexityRate, elapsedTime)
	if over != nil {
		targetComplexity = math.MaxUint64
	}

	switch {
	case parentBlkComplexity > targetComplexity:
		exp := 1442 * coeff * (parentBlkComplexity - targetComplexity) / targetComplexity / 1000
		exp = min(exp, 62) // we cap the exponent to avoid an overflow of uint64 type
		res, over := safemath.Mul64(currentFeeRate, 1<<exp)
		if over != nil {
			return math.MaxUint64
		}
		return res

	case parentBlkComplexity < targetComplexity:
		exp := 1442 * coeff * (targetComplexity - parentBlkComplexity) / targetComplexity / 1000
		exp = min(exp, 62) // we cap the exponent to avoid an overflow of uint64 type
		return currentFeeRate / (1 << exp)

	default:
		return currentFeeRate // unitsConsumed == target
	}
}

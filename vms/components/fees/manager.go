// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package fees

import (
	"fmt"
	"math"

	safemath "github.com/ava-labs/avalanchego/utils/math"
)

// the update fee algorithm has a UpdateCoefficient, normalized to [CoeffDenom]
const CoeffDenom = uint64(1_000)

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
			feesConfig.BlockMaxComplexity[i],
			elapsedTime,
		)
		nextFeeRates = max(nextFeeRates, feesConfig.MinFeeRate[i])
		m.feeRates[i] = nextFeeRates
	}
	return nil
}

func nextFeeRate(
	currentFeeRate,
	coeff,
	parentBlkComplexity,
	targetComplexityRate,
	maxComplexity,
	elapsedTime uint64,
) uint64 {
	// We update the fee rate with the formula:
	//     feeRate_{t+1} = feeRate_t * exp(delta)
	// where
	//     delta == K * (parentComplexity - targetComplexity)/(targetComplexity)
	// and [targetComplexity] is the median complexity expected in the elapsed time.
	//
	// We approximate the exponential as follows:
	// * For delta in the interval [0, 10], we use the following piecewise, linear function:
	//       x \in [a,b] --> exp(X) ≈≈ (exp(b)-exp(a))(b-a)*(X-a) + exp(a)
	// * For delta > 10 we approximate the exponential via a quadratic function
	//       exp(X) ≈≈ (X-10)^2 + exp(10)
	// The approximation is overall continuous, so it behaves well at the interval edges

	// parent and child block may have the same timestamp. In this case targetComplexity will match targetComplexityRate
	elapsedTime = max(1, elapsedTime)
	targetComplexity, over := safemath.Mul64(targetComplexityRate, elapsedTime)
	if over != nil {
		targetComplexity = math.MaxUint64
	}

	if parentBlkComplexity == targetComplexity {
		return currentFeeRate // complexity matches target, nothing to update
	}

	// [increase] tells if
	// [nextFeeRate] = [currentFeeRate] * [factor] or
	// [nextFeeRate] = [currentFeeRate] / [factor]
	var increase bool

	var num1 uint64
	if maxComplexity > targetComplexity {
		num1 = coeff * (maxComplexity - targetComplexity)
		increase = true
	} else {
		num1 = coeff * (targetComplexity - maxComplexity)
		increase = false
	}
	denom1 := CoeffDenom * targetComplexity

	switch {
	case num1 < 1*denom1:
		num1 = 2*num1 + denom1
	case num1 < 2*denom1:
		num1 = 5*num1 - 2*denom1
	case num1 < 3*denom1:
		num1 = 13*num1 - 18*denom1
	case num1 < 4*denom1:
		num1 = 35*num1 - 84*denom1
	case num1 < 5*denom1:
		num1 = 94*num1 - 321*denom1
	case num1 < 6*denom1:
		num1 = 256*num1 - 1131*denom1
	case num1 < 7*denom1:
		num1 = 694*num1 - 3760*denom1
	case num1 < 8*denom1:
		num1 = 1885*num1 - 12098*denom1
	case num1 < 9*denom1:
		num1 = 5123*num1 - 38003*denom1
	default:
		num1 = 13924*num1 - 117212*denom1
	}

	var (
		num2   uint64
		denom2 uint64
	)

	if maxComplexity > targetComplexity {
		num2 = parentBlkComplexity + maxComplexity - 2*targetComplexity
		denom2 = maxComplexity - targetComplexity
	} else {
		num2 = 2*targetComplexity - parentBlkComplexity - maxComplexity
		denom2 = targetComplexity - maxComplexity
	}

	if increase {
		return currentFeeRate * num1 * num2 / denom1 / denom2
	} else {
		return currentFeeRate * denom1 * num2 / num1 / denom2
	}

	////////////////////////////////////////////////////////////////////////////
	////////////////////////////////////////////////////////////////////////////
	////////////////////////////////////////////////////////////////////////////

	// // To control for numerical errors, we defer [coeff] normalization by [CoeffDenom]
	// // to the very end. This is possible since the approximation we use is piecewise linear.
	// if parentBlkComplexity > targetComplexity {
	// 	delta = coeff * (parentBlkComplexity - targetComplexity) / targetComplexity
	// 	increase = true
	// } else {
	// 	delta = coeff * (targetComplexity - parentBlkComplexity) / targetComplexity
	// 	increase = false
	// }

	// switch {
	// case delta < 1*CoeffDenom:
	// 	weight = 2*delta + 1*CoeffDenom
	// case delta < 2*CoeffDenom:
	// 	weight = 5*delta - 2*CoeffDenom
	// case delta < 3*CoeffDenom:
	// 	weight = 13*delta - 18*CoeffDenom
	// case delta < 4*CoeffDenom:
	// 	weight = 35*delta - 84*CoeffDenom
	// case delta < 5*CoeffDenom:
	// 	weight = 94*delta - 321*CoeffDenom
	// case delta < 6*CoeffDenom:
	// 	weight = 256*delta - 1131*CoeffDenom
	// case delta < 7*CoeffDenom:
	// 	weight = 694*delta - 3760*CoeffDenom
	// case delta < 8*CoeffDenom:
	// 	weight = 1885*delta - 12098*CoeffDenom
	// case delta < 9*CoeffDenom:
	// 	weight = 5123*delta - 38003*CoeffDenom
	// case delta < 10*CoeffDenom:
	// 	weight = 13924*delta - 117212*CoeffDenom
	// default:
	// 	weight = (delta/CoeffDenom - 10) ^ 2 + 22028
	// }

	// if increase {
	// 	return currentFeeRate * weight / CoeffDenom
	// }
	// return currentFeeRate * CoeffDenom / weight

	////////////////////////////////////////////////////////////////////////////
	////////////////////////////////////////////////////////////////////////////
	////////////////////////////////////////////////////////////////////////////
}

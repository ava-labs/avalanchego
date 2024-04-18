// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package fees

import (
	"fmt"
	"math"

	safemath "github.com/ava-labs/avalanchego/utils/math"
)

// the update fee algorithm has a UpdateCoefficient, normalized to  [CoeffDenom]
const CoeffDenom = uint64(10_000)

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

func nextFeeRate(currentFeeRate, coeff, parentBlkComplexity, targetComplexityRate, maxBlkComplexity, elapsedTime uint64) uint64 {
	// We update the fee rate with the formula:
	//     feeRate_{t+1} = feeRate_t * exp(delta)
	// where
	//     delta == K * (parentComplexity - targetComplexity)/(targetComplexity)
	// and [targetComplexity] is the median complexity expected in the elapsed time.
	//
	// We approximate the exponential as follows:
	// * For delta in the interval [0, MaxDelta], we use the following piecewise, linear function:
	//       x \in [a,b] --> exp(X) ≈≈ (exp(b)-exp(a))(b-a)*(X-a) + exp(a)
	// MaxDelta is given by the max block complexity (MaxBlkComplexity - targetComplexity)/(targetComplexity)
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

	var (
		scaledDelta uint64
		scaleCoeff  uint64

		// [increase] tells if
		// [nextFeeRate] = [currentFeeRate] * [factor] or
		// [nextFeeRate] = [currentFeeRate] / [factor]
		increase bool

		// [weight] is how much we increase or reduce the fee rate
		weight uint64
	)

	// To control for numerical errors, we defer [coeff] normalization by [CoeffDenom]
	// to the very end. This is possible since the approximation we use is piecewise linear.
	if parentBlkComplexity > targetComplexity {
		scaledDelta = 10 * (parentBlkComplexity - targetComplexity) / (maxBlkComplexity - targetComplexity)
		scaleCoeff = coeff * (maxBlkComplexity - targetComplexity) / targetComplexity / CoeffDenom / 10
		increase = true
	} else {
		scaledDelta = 10 * (targetComplexity - parentBlkComplexity) / (maxBlkComplexity - targetComplexity)
		scaleCoeff = coeff * (maxBlkComplexity - targetComplexity) / targetComplexity / CoeffDenom / 10
		increase = false
	}

	var (
		m, q      uint64
		qPositive bool
	)
	switch {
	case scaledDelta < 1:
		m = 2
		q = 1
		qPositive = true
	case scaledDelta < 2:
		m = 5
		q = 2
	case scaledDelta < 3:
		m = 13
		q = 18
	case scaledDelta < 4:
		m = 35
		q = 84
	case scaledDelta < 5:
		m = 94
		q = 321
	case scaledDelta < 6:
		m = 256
		q = 1131
	case scaledDelta < 7:
		m = 694
		q = 3760
	case scaledDelta < 8:
		m = 1885
		q = 12098
	case scaledDelta < 9:
		m = 5123
		q = 38003
	default:
		m = 13924
		q = 117212
	}

	if !qPositive {
		weight = scaleCoeff*(m*scaledDelta-q) + (scaleCoeff-1)*q
	} else {
		weight = scaleCoeff*(m*scaledDelta+q) - (scaleCoeff-1)*q
	}

	if increase {
		return currentFeeRate * weight / CoeffDenom
	}
	return currentFeeRate * CoeffDenom / weight
}

// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package fees

import (
	"fmt"
	"math"

	safemath "github.com/ava-labs/avalanchego/utils/math"
)

// the update fee algorithm has a UpdateCoefficient, normalized to [CoeffDenom]
const CoeffDenom = uint64(20)

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

// UpdateFeeRates calculates next fee rates.
// We update the fee rate with the formula:
//
//	feeRate_{t+1} = Max(feeRate_t * exp(k*delta), minFeeRate)
//
// where
//
//	delta == (parentComplexity - targetBlkComplexity)/targetBlkComplexity
//
// and [targetBlkComplexity] is the target complexity expected in the elapsed time.
// We update the fee rate trying to guarantee the following stability property:
//
//	feeRate(delta) * feeRate(-delta) = 1
//
// so that fee rates won't change much when block complexity wiggles around target complexity
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
		targetBlkComplexity := targetComplexity(
			feesConfig.BlockTargetComplexityRate[i],
			elapsedTime,
			feesConfig.BlockMaxComplexity[i],
		)

		factorNum, factorDenom := updateFactor(
			feesConfig.UpdateCoefficient[i],
			parentBlkComplexity[i],
			targetBlkComplexity,
		)
		nextFeeRates, over := safemath.Mul64(m.feeRates[i], factorNum)
		if over != nil {
			nextFeeRates = math.MaxUint64
		}
		nextFeeRates /= factorDenom

		nextFeeRates = max(nextFeeRates, feesConfig.MinFeeRate[i])
		m.feeRates[i] = nextFeeRates
	}
	return nil
}

func targetComplexity(targetComplexityRate, elapsedTime, maxBlockComplexity uint64) uint64 {
	// parent and child block may have the same timestamp. In this case targetComplexity will match targetComplexityRate
	elapsedTime = max(1, elapsedTime)
	targetComplexity, over := safemath.Mul64(targetComplexityRate, elapsedTime)
	if over != nil {
		targetComplexity = maxBlockComplexity
	}

	// regardless how low network load has been, we won't allow
	// blocks larger than max block complexity
	targetComplexity = min(targetComplexity, maxBlockComplexity)
	return targetComplexity
}

// updateFactor uses the following piece-wise approximation for the exponential function:
//
//	if B > T --> exp{k * (B-T)/T} ≈≈    Approx(k,B,T)
//	if B < T --> exp{k * (B-T)/T} ≈≈ 1/ Approx(k,B,T)
//
// Note that the approximation guarantees stability, since
//
//	factor(k, B=T+X, T)*factor(k, B=T-X, T) == 1
//
// We express the result with the pair (numerator, denominator)
// to increase precision with small deltas
func updateFactor(k, b, t uint64) (uint64, uint64) {
	if b == t {
		return 1, 1 // complexity matches target, nothing to update
	}

	var (
		increaseFee bool
		delta       uint64
	)

	if t < b {
		increaseFee = true
		delta = b - t
	} else {
		increaseFee = false
		delta = t - b
	}

	n, over := safemath.Mul64(k, delta)
	if over != nil {
		n = math.MaxUint64
	}
	d, over := safemath.Mul64(CoeffDenom, t)
	if over != nil {
		d = math.MaxUint64
	}

	p, q := expPiecewiseApproximation(n, d)
	if increaseFee {
		return p, q
	}
	return q, p
}

// piecewise approximation data. exp(x) ≈≈ m_i * x ± q_i in [i,i+1]
func expPiecewiseApproximation(a, b uint64) (uint64, uint64) { // exported to appease linter.
	var (
		m, q uint64
		sign bool
	)

	switch v := a / b; {
	case v < 1:
		m, q, sign = 2, 1, true
	case v < 2:
		m, q, sign = 5, 2, false
	case v < 3:
		m, q, sign = 13, 18, false
	case v < 4:
		m, q, sign = 35, 84, false
	case v < 5:
		m, q, sign = 94, 321, false
	case v < 6:
		m, q, sign = 256, 1131, false
	case v < 7:
		m, q, sign = 694, 3760, false
	case v < 8:
		m, q, sign = 1885, 12098, false
	case v < 9:
		m, q, sign = 5123, 38003, false
	default:
		m, q, sign = 13924, 117212, false
	}

	// m(A/B) - q == (m*A-q*B)/B
	n1, over := safemath.Mul64(m, a)
	if over != nil {
		return math.MaxUint64, b
	}
	n2, over := safemath.Mul64(q, b)
	if over != nil {
		return math.MaxUint64, b
	}

	var n uint64
	if !sign {
		n = n1 - n2
	} else {
		n, over = safemath.Add64(n1, n2)
		if over != nil {
			return math.MaxUint64, b
		}
	}
	return n, b
}

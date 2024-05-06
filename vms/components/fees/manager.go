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

	// We update the fee rate with the formula:
	//     feeRate_{t+1} = feeRate_t * exp(k*delta)
	// where
	//     delta == (parentComplexity - targetBlkComplexity)/targetBlkComplexity
	// and [targetBlkComplexity] is the target complexity expected in the elapsed time.
	// We update the fee rate trying to guarantee the following stability property:
	// 		feeRate(delta) * feeRate(-delta) = 1
	// so that fee rates won't change much when block complexity wiggles around target complexity

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

func updateFactor(
	coeff,
	parentBlkComplexity,
	targetBlkComplexity uint64,
) (uint64, uint64) {
	// We use the following piece-wise approximation for the exponential function:
	//
	//	if B > T --> exp{k * (B-T)/T} ≈≈    Approx(k,B,T)
	//  if B < T --> exp{k * (B-T)/T} ≈≈ 1/ Approx(k,B,T)
	//
	// Note that the approximation guarantees that factor(delta)*factor(-delta) == 1
	// We express the result with the pair (numerator, denominator)
	// to increase precision with small deltas

	if parentBlkComplexity == targetBlkComplexity {
		return 1, 1 // complexity matches target, nothing to update
	}

	var (
		increaseFee bool
		delta       uint64
	)

	if targetBlkComplexity < parentBlkComplexity {
		increaseFee = true
		delta = parentBlkComplexity - targetBlkComplexity
	} else {
		increaseFee = false
		delta = targetBlkComplexity - parentBlkComplexity
	}

	var n, d uint64
	a, over := safemath.Mul64(coeff, delta)
	if over != nil {
		a = math.MaxUint64
	}
	b, over := safemath.Mul64(CoeffDenom, targetBlkComplexity)
	if over != nil {
		b = math.MaxUint64
	}

	n, d = ExpPiecewiseApproximation(a, b)
	// n, d = expTaylorApproximation(a, b)

	if increaseFee {
		return n, d
	}
	return d, n
}

// piecewise approximation data. exp(x) ≈≈ m_i * x ± q_i in [i,i+1]
var (
	ms     = [...]uint64{2, 5, 13, 35, 94, 256, 694, 1885, 5123, 13924}
	qs     = [...]uint64{1, 2, 18, 84, 321, 1131, 3760, 12098, 38003, 117212}
	qSigns = [...]bool{true, false, false, false, false, false, false, false, false, false}
)

func ExpPiecewiseApproximation(a, b uint64) (uint64, uint64) { // exported to appease linter.
	idx := int(a / b)
	if idx >= len(ms) {
		idx = len(ms) - 1
	}

	m := ms[idx]
	q := qs[idx]
	sign := qSigns[idx]

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

func ExpTaylorApproximation(a, b uint64) (uint64, uint64) { // exported to appease linter.
	//	TAYLOR APPROXIMATION
	// exp{A/B} ≈≈ 1 + A/B + 1/2 * A^2/B^2 == (B^2 + (A+B)^2) / (2*B^2)
	var (
		n, d, b2 uint64
		over     error
	)

	b2, over = safemath.Mul64(b, b)
	if over != nil {
		b2 = math.MaxUint64
	}

	n, over = safemath.Add64(a, b)
	if over != nil {
		n = math.MaxUint64
	}
	n, over = safemath.Mul64(n, n)
	if over != nil {
		n = math.MaxUint64
	}
	n, over = safemath.Add64(b2, n)
	if over != nil {
		n = math.MaxUint64
	}
	d, over = safemath.Mul64(2, b2)
	if over != nil {
		n = math.MaxUint64
	}
	return n, d
}

func targetComplexity(
	targetComplexityRate,
	elapsedTime,
	maxBlockComplexity uint64,
) uint64 {
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

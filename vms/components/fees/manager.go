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
			feesConfig.BlockMaxComplexity[i],
			feesConfig.BlockTargetComplexityRate[i],
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
	maxBlockComplexity,
	targetComplexityRate,
	elapsedTime uint64,
) uint64 {
	// We update the fee rate with the formula:
	//     feeRate_{t+1} = feeRate_t * exp(delta)
	// where
	//     delta == K * (parentComplexity - targetComplexity)/(targetComplexity)
	// and [targetComplexity] is the median complexity expected in the elapsed time.
	//
	// We approximate the exponential as follows:
	//
	//                              1 + (k * delta)^2 if delta >= 0
	// feeRate_{t+1} = feeRate_t *
	//                              1 / (1 + (k * delta)^2) if delta < 0
	//
	// The approximation keeps two key properties of the exponential formula:
	// 1. It's strictly increasing with delta
	// 2. It's stable because feeRate(delta) * feeRate(-delta) = 1, meaning that
	//    if complexity increase and decrease by the same amount in two consecutive blocks
	//    the fee rate will go back to the original value.

	// parent and child block may have the same timestamp. In this case targetComplexity will match targetComplexityRate
	elapsedTime = max(1, elapsedTime)
	targetComplexity, over := safemath.Mul64(targetComplexityRate, elapsedTime)
	if over != nil {
		targetComplexity = maxBlockComplexity
	}

	// regardless how low network load has been, we won't allow
	// blocks larger than max block complexity
	targetComplexity = min(targetComplexity, maxBlockComplexity)

	if parentBlkComplexity == targetComplexity {
		return currentFeeRate // complexity matches target, nothing to update
	}

	var (
		increaseFee bool
		delta       uint64
	)

	if targetComplexity < parentBlkComplexity {
		increaseFee = true
		delta = parentBlkComplexity - targetComplexity
	} else {
		increaseFee = false
		delta = targetComplexity - parentBlkComplexity
	}

	num := coeff * coeff * delta * delta
	denom := targetComplexity * targetComplexity * CoeffDenom * CoeffDenom

	if increaseFee {
		res, over := safemath.Mul64(currentFeeRate, denom+num)
		if over != nil {
			res = math.MaxUint64
		}
		return res / denom
	}

	res, over := safemath.Mul64(currentFeeRate, denom)
	if over != nil {
		res = math.MaxUint64
	}
	return res / (denom + num)
}

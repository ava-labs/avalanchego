// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package fees

import (
	"errors"
	"fmt"
	"time"
)

var (
	errZeroLeakGasCoeff     = errors.New("zero leak gas coefficient")
	errZerUpdateDenominator = errors.New("update denominator cannot be zero")
)

type DynamicFeesConfig struct {
	// MinGasPrice contains the minimal gas price
	// enforced by the dynamic fees algo.
	MinGasPrice GasPrice `json:"minimal-gas-price"`

	// UpdateDenominator contains the
	// exponential normalization coefficient.
	UpdateDenominator Gas `json:"update-denominator"`

	// GasTargetRate contains the preferred gas consumed by a block.
	// The dynamic fee algo strives to converge to GasTargetRate per second.
	GasTargetRate Gas `json:"block-target-complexity-rate"`

	// weights to merge fees dimensions complexities into a single gas value
	FeeDimensionWeights Dimensions `json:"fee-dimension-weights"`

	MaxGasPerSecond Gas
	LeakGasCoeff    Gas // techically the unit of measure if sec^{-1}, but picking Gas reduces casts needed
}

func (c *DynamicFeesConfig) Validate() error {
	if c.UpdateDenominator == 0 {
		return errZerUpdateDenominator
	}

	if c.LeakGasCoeff == 0 {
		return errZeroLeakGasCoeff
	}

	return nil
}

// We cap the maximum gas consumed by time with a leaky bucket approach
// GasCap = min (GasCap + MaxGasPerSecond/LeakGasCoeff*ElapsedTime, MaxGasPerSecond)
func GasCap(cfg DynamicFeesConfig, currentGasCapacity Gas, parentBlkTime, childBlkTime time.Time) (Gas, error) {
	if !childBlkTime.After(parentBlkTime) {
		return ZeroGas, fmt.Errorf("unexpected block times, parentBlkTim %v, childBlkTime %v", parentBlkTime, childBlkTime)
	}

	elapsedTime := uint64(childBlkTime.Unix() - parentBlkTime.Unix())
	if elapsedTime > uint64(cfg.LeakGasCoeff) {
		return cfg.MaxGasPerSecond, nil
	}

	return min(cfg.MaxGasPerSecond, currentGasCapacity+cfg.MaxGasPerSecond*Gas(elapsedTime)/cfg.LeakGasCoeff), nil
}

func UpdateGasCap(currentGasCap, blkGas Gas) Gas {
	nextGasCap := Gas(0)
	if currentGasCap > blkGas {
		nextGasCap = currentGasCap - blkGas
	}
	return nextGasCap
}

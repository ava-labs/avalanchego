// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package fees

import "errors"

var errZeroLeakGasCoeff = errors.New("zero leak gas coefficient")

type DynamicFeesConfig struct {
	GasPrice GasPrice `json:"gas-price"`

	// weights to merge fees dimensions complexities into a single gas value
	FeeDimensionWeights Dimensions `json:"fee-dimension-weights"`

	MaxGasPerSecond Gas
	LeakGasCoeff    Gas // TODO ABENEGIA: not sure of the unit of measurement here
}

func (c *DynamicFeesConfig) Validate() error {
	if c.LeakGasCoeff == 0 {
		return errZeroLeakGasCoeff
	}

	return nil
}

// We cap the maximum gas consumed by time with a leaky bucket approach
// MaxGas = min (MaxGas + MaxGasPerSecond/LeakGasCoeff*ElapsedTime, MaxGasPerSecond)
func MaxGas(cfg DynamicFeesConfig, currentGasCapacity Gas, elapsedTime uint64) Gas {
	if elapsedTime > uint64(cfg.LeakGasCoeff) {
		return cfg.MaxGasPerSecond
	}

	return min(cfg.MaxGasPerSecond, currentGasCapacity+cfg.MaxGasPerSecond*Gas(elapsedTime)/cfg.LeakGasCoeff)
}

// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package fees

import "fmt"

type DynamicFeesConfig struct {
	// InitialFeeRate contains, per each fee dimension, the
	// fee rate, i.e. the fee per unit of complexity. Fee rates are
	// valid as soon as fork introducing dynamic fees activates.
	// Fee rates will be then updated by the dynamic fees algo.
	InitialFeeRate Dimensions `json:"initial-fee-rate"`

	// MinFeeRate contains, per each fee dimension, the
	// minimal fee rate, i.e. the fee per unit of complexity,
	// enforced by the dynamic fees algo.
	MinFeeRate Dimensions `json:"minimal-fee-rate"`

	// UpdateCoefficient contains, per each fee dimension, the
	// exponential update coefficient. Setting an entry to 0 makes
	// the corresponding fee rate constant.
	UpdateCoefficient Dimensions `json:"update-coefficient"`

	// BlockTargetComplexityRate contains, per each fee dimension, the
	// preferred block complexity that the dynamic fee algo
	// strive to converge to, per second.
	BlockTargetComplexityRate Dimensions `json:"block-target-complexity-rate"`

	// BlockMaxComplexity contains, per each fee dimension, the
	// maximal complexity a valid P-chain block can host.
	BlockMaxComplexity Dimensions `json:"block-max-complexity-rate"`
}

func (c *DynamicFeesConfig) Validate() error {
	for i := Dimension(0); i < FeeDimensions; i++ {
		// MinFeeRate can be zero, but that is a bit dangerous. If a fee rate ever becomes
		// zero, the update mechanism will keep them to zero.
		if c.InitialFeeRate[i] < c.MinFeeRate[i] {
			return fmt.Errorf("dimension %d, initial fee rate %d smaller than minimal fee rate %d",
				i,
				c.InitialFeeRate[i],
				c.MinFeeRate[i],
			)
		}

		if c.BlockTargetComplexityRate[i] > c.BlockMaxComplexity[i] {
			return fmt.Errorf("dimension %d, block target complexity rate %d larger than block max complexity rate %d",
				i,
				c.BlockTargetComplexityRate[i],
				c.BlockMaxComplexity[i],
			)
		}

		// The update algorithm normalizes complexity delta by [BlockTargetComplexityRate].
		// So we enforce [BlockTargetComplexityRate] to be non-zero.
		if c.BlockTargetComplexityRate[i] == 0 {
			return fmt.Errorf("dimension %d, block target complexity rate set to zero", i)
		}
	}

	return nil
}

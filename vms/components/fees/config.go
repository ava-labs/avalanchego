// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package fees

import "fmt"

type DynamicFeesConfig struct {
	// MinFeeRate contains, per each fee dimension, the
	// minimal fee rate, i.e. the fee per unit of complexity,
	// enforced by the dynamic fees algo.
	MinFeeRate Dimensions `json:"minimal-fee-rate"`

	// UpdateDenominators contains, per each fee dimension, the
	// exponential update coefficient. Setting an entry to 0 makes
	// the corresponding fee rate constant.
	UpdateDenominators Dimensions `json:"update-denominator"`

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
		if c.BlockTargetComplexityRate[i] > c.BlockMaxComplexity[i] {
			return fmt.Errorf("dimension %d, block target complexity rate %d larger than block max complexity rate %d",
				i,
				c.BlockTargetComplexityRate[i],
				c.BlockMaxComplexity[i],
			)
		}
	}

	return nil
}

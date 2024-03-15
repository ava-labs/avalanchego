// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package fees

import "fmt"

type DynamicFeesConfig struct {
	// InitialUnitFees contains, per each fee dimension, the
	// unit fees valid as soon as fork introducing dynamic fees
	// activates. Unit fees will be then updated by the dynamic fees algo.
	InitialUnitFees Dimensions `json:"initial-unit-fees"`

	// MinUnitFees contains, per each fee dimension, the
	// minimal unit fees enforced by the dynamic fees algo.
	MinUnitFees Dimensions `json:"min-unit-fees"`

	// UpdateCoefficient contains, per each fee dimension, the
	// exponential update coefficient. Setting an entry to 0 makes
	// the corresponding fee rate constant.
	UpdateCoefficient Dimensions `json:"update-coefficient"`

	// BlockUnitsCap contains, per each fee dimension, the
	// maximal complexity a valid P-chain block can host
	BlockUnitsCap Dimensions `json:"block-unit-caps"`

	// BlockUnitsTarget contains, per each fee dimension, the
	// preferred block complexity that the dynamic fee algo
	// strive to converge to
	BlockUnitsTarget Dimensions `json:"block-target-caps"`
}

func (c *DynamicFeesConfig) Validate() error {
	for i := Dimension(0); i < FeeDimensions; i++ {
		// MinUnitFees can be zero, but that is a bit dangerous. if a fee ever becomes
		// zero, the update mechanism will keep them to zero.

		if c.InitialUnitFees[i] < c.MinUnitFees[i] {
			return fmt.Errorf("dimension %d, initial unit fee %d smaller than minimal unit fee %d",
				i,
				c.InitialUnitFees[i],
				c.MinUnitFees[i],
			)
		}

		if c.BlockUnitsTarget[i] > c.BlockUnitsCap[i] {
			return fmt.Errorf("dimension %d, block target units %d larger than block units cap %d",
				i,
				c.BlockUnitsTarget[i],
				c.BlockUnitsCap[i],
			)
		}

		if c.BlockUnitsTarget[i] == 0 {
			return fmt.Errorf("dimension %d, block target units set to zero", i)
		}
	}

	return nil
}

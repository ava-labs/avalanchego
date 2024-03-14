// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package config

import (
	"fmt"

	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/units"

	commonfees "github.com/ava-labs/avalanchego/vms/components/fees"
)

func init() {
	if customDynamicFeesConfig != nil {
		if err := customDynamicFeesConfig.validate(); err != nil {
			panic(err)
		}
	}

	if err := eUpgradeDynamicFeesConfig.validate(); err != nil {
		panic(err)
	}
}

// eUpgradeDynamicFeesConfig to be tuned TODO ABENEGIA
var (
	eUpgradeDynamicFeesConfig = DynamicFeesConfig{
		InitialUnitFees: commonfees.Dimensions{
			1 * units.NanoAvax,
			2 * units.NanoAvax,
			3 * units.NanoAvax,
			4 * units.NanoAvax,
		},
		MinUnitFees: commonfees.Dimensions{},
		UpdateCoefficient: commonfees.Dimensions{
			1,
			1,
			1,
			1,
		},
		BlockUnitsCap:    commonfees.Max,
		BlockUnitsTarget: commonfees.Dimensions{1, 1, 1, 1},
	}

	// TODO ABENEGIA: decide if and how to validate preEUpgradeDynamicFeesConfig
	preEUpgradeDynamicFeesConfig = DynamicFeesConfig{
		InitialUnitFees: commonfees.Empty,
		BlockUnitsCap:   commonfees.Max,
	}

	customDynamicFeesConfig *DynamicFeesConfig
)

func GetDynamicFeesConfig(isEActive bool) DynamicFeesConfig {
	if !isEActive {
		return preEUpgradeDynamicFeesConfig
	}

	if customDynamicFeesConfig != nil {
		return *customDynamicFeesConfig
	}
	return eUpgradeDynamicFeesConfig
}

func ResetDynamicFeesConfig(ctx *snow.Context, customFeesConfig *DynamicFeesConfig) error {
	if customFeesConfig == nil {
		return nil // nothing to do
	}
	if ctx.NetworkID == constants.MainnetID || ctx.NetworkID == constants.FujiID {
		return fmt.Errorf("forbidden resetting dynamic unit fees config for network %s", constants.NetworkName(ctx.NetworkID))
	}

	customDynamicFeesConfig = customFeesConfig
	return nil
}

type DynamicFeesConfig struct {
	// InitialUnitFees contains, per each fee dimension, the
	// unit fees valid as soon as fork introducing dynamic fees
	// activates. Unit fees will be then updated by the dynamic fees algo.
	InitialUnitFees commonfees.Dimensions `json:"initial-unit-fees"`

	// MinUnitFees contains, per each fee dimension, the
	// minimal unit fees enforced by the dynamic fees algo.
	MinUnitFees commonfees.Dimensions `json:"min-unit-fees"`

	// UpdateCoefficient contains, per each fee dimension, the
	// exponential update coefficient. Setting an entry to 0 makes
	// the corresponding fee rate constant.
	UpdateCoefficient commonfees.Dimensions `json:"update-coefficient"`

	// BlockUnitsCap contains, per each fee dimension, the
	// maximal complexity a valid P-chain block can host
	BlockUnitsCap commonfees.Dimensions `json:"block-unit-caps"`

	// BlockUnitsTarget contains, per each fee dimension, the
	// preferred block complexity that the dynamic fee algo
	// strive to converge to
	BlockUnitsTarget commonfees.Dimensions `json:"block-target-caps"`
}

func (c *DynamicFeesConfig) validate() error {
	for i := commonfees.Dimension(0); i < commonfees.FeeDimensions; i++ {
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

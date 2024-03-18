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
	if err := eUpgradeDynamicFeesConfig.Validate(); err != nil {
		panic(err)
	}
}

// eUpgradeDynamicFeesConfig to be tuned TODO ABENEGIA
var (
	eUpgradeDynamicFeesConfig = commonfees.DynamicFeesConfig{
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
	preEUpgradeDynamicFeesConfig = commonfees.DynamicFeesConfig{
		InitialUnitFees: commonfees.Empty,
		BlockUnitsCap:   commonfees.Max,
	}

	customDynamicFeesConfig *commonfees.DynamicFeesConfig
)

func GetDynamicFeesConfig(isEActive bool) commonfees.DynamicFeesConfig {
	if !isEActive {
		return preEUpgradeDynamicFeesConfig
	}
	if customDynamicFeesConfig != nil {
		return *customDynamicFeesConfig
	}
	return eUpgradeDynamicFeesConfig
}

func ResetDynamicFeesConfig(ctx *snow.Context, customFeesConfig *commonfees.DynamicFeesConfig) error {
	if customFeesConfig == nil {
		return nil // nothing to do
	}
	if ctx.NetworkID == constants.MainnetID || ctx.NetworkID == constants.FujiID {
		return fmt.Errorf("forbidden resetting dynamic unit fees config for network %s", constants.NetworkName(ctx.NetworkID))
	}
	customDynamicFeesConfig = customFeesConfig
	return nil
}

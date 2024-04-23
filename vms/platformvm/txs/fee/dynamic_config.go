// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package fee

import (
	"fmt"

	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/units"

	commonfees "github.com/ava-labs/avalanchego/vms/components/fees"
)

func init() {
	if customDynamicFeesConfig != nil {
		if err := customDynamicFeesConfig.Validate(); err != nil {
			panic(err)
		}
	}

	if err := eUpgradeDynamicFeesConfig.Validate(); err != nil {
		panic(err)
	}
}

var (
	eUpgradeDynamicFeesConfig = commonfees.DynamicFeesConfig{
		InitialFeeRate: commonfees.Dimensions{
			80 * units.NanoAvax,
			10 * units.NanoAvax,
			15 * units.NanoAvax,
			50 * units.NanoAvax,
		},
		MinFeeRate: commonfees.Dimensions{ // 3/4 of InitialFees
			60 * units.NanoAvax,
			8 * units.NanoAvax,
			10 * units.NanoAvax,
			35 * units.NanoAvax,
		},
		UpdateCoefficient: commonfees.Dimensions{ // over fees.CoeffDenom
			3,
			2,
			2,
			3,
		},
		BlockMaxComplexity: commonfees.Dimensions{
			10_000,
			6_000,
			8_000,
			60_000,
		},
		BlockTargetComplexityRate: commonfees.Dimensions{
			200,
			60,
			80,
			600,
		},
	}

	// TODO ABENEGIA: decide if and how to validate preEUpgradeDynamicFeesConfig
	preEUpgradeDynamicFeesConfig = commonfees.DynamicFeesConfig{
		InitialFeeRate:     commonfees.Empty,
		BlockMaxComplexity: commonfees.Max,
	}

	customDynamicFeesConfig *commonfees.DynamicFeesConfig
)

func GetDynamicConfig(isEActive bool) commonfees.DynamicFeesConfig {
	if !isEActive {
		return preEUpgradeDynamicFeesConfig
	}

	if customDynamicFeesConfig != nil {
		return *customDynamicFeesConfig
	}
	return eUpgradeDynamicFeesConfig
}

func ResetDynamicConfig(ctx *snow.Context, customFeesConfig *commonfees.DynamicFeesConfig) error {
	if customFeesConfig == nil {
		return nil // nothing to do
	}
	if ctx.NetworkID == constants.MainnetID || ctx.NetworkID == constants.FujiID {
		return fmt.Errorf("forbidden resetting dynamic fee rates config for network %s", constants.NetworkName(ctx.NetworkID))
	}

	customDynamicFeesConfig = customFeesConfig
	return nil
}

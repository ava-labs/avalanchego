// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package fee

import (
	"errors"
	"fmt"

	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/units"

	commonfees "github.com/ava-labs/avalanchego/vms/components/fees"
)

var (
	errDynamicFeeConfigNotAvailable = errors.New("dynamic fee config not available")

	eUpgradeDynamicFeesConfig = commonfees.DynamicFeesConfig{
		FeeRate: commonfees.Dimensions{
			1 * units.NanoAvax,
			2 * units.NanoAvax,
			3 * units.NanoAvax,
			4 * units.NanoAvax,
		},

		BlockMaxComplexity: commonfees.Max,
	}

	customDynamicFeesConfig *commonfees.DynamicFeesConfig
)

func GetDynamicConfig(isEActive bool) (commonfees.DynamicFeesConfig, error) {
	if !isEActive {
		return commonfees.DynamicFeesConfig{}, errDynamicFeeConfigNotAvailable
	}

	if customDynamicFeesConfig != nil {
		return *customDynamicFeesConfig, nil
	}
	return eUpgradeDynamicFeesConfig, nil
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

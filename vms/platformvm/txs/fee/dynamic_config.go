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

const TempGasCap = commonfees.Gas(1_000_000) // TODO ABENEGIA: temp const to be replaced with API call

var (
	errDynamicFeeConfigNotAvailable = errors.New("dynamic fee config not available")

	eUpgradeDynamicFeesConfig = commonfees.DynamicFeesConfig{
		MinGasPrice:         commonfees.GasPrice(10 * units.NanoAvax),
		UpdateDenominator:   commonfees.Gas(50_000),
		GasTargetRate:       commonfees.Gas(250),
		FeeDimensionWeights: commonfees.Dimensions{1, 1, 1, 1},
		MaxGasPerSecond:     commonfees.Gas(1_000_000),
		LeakGasCoeff:        commonfees.Gas(1),
	}

	customDynamicFeesConfig *commonfees.DynamicFeesConfig
)

func init() {
	if err := eUpgradeDynamicFeesConfig.Validate(); err != nil {
		panic(err)
	}
}

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
	if err := customFeesConfig.Validate(); err != nil {
		return fmt.Errorf("invalid custom fee config: %w", err)
	}

	customDynamicFeesConfig = customFeesConfig
	return nil
}

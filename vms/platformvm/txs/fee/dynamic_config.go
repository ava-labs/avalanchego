// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package fee

import (
	"errors"
	"fmt"

	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/units"

	commonfee "github.com/ava-labs/avalanchego/vms/components/fee"
)

var (
	ErrDynamicFeeConfigNotAvailable = errors.New("dynamic fee config not available")

	eUpgradeDynamicFeesConfig = commonfee.DynamicFeesConfig{
		MinGasPrice:         commonfee.GasPrice(10 * units.NanoAvax),
		UpdateDenominator:   commonfee.Gas(100_000),
		GasTargetRate:       commonfee.Gas(2_500),
		FeeDimensionWeights: commonfee.Dimensions{6, 10, 10, 1},
		MaxGasPerSecond:     commonfee.Gas(500_000),
		LeakGasCoeff:        commonfee.Gas(5),
	}

	customDynamicFeesConfig *commonfee.DynamicFeesConfig
)

func init() {
	if err := eUpgradeDynamicFeesConfig.Validate(); err != nil {
		panic(err)
	}
}

func GetDynamicConfig(isEActive bool) (commonfee.DynamicFeesConfig, error) {
	if !isEActive {
		return commonfee.DynamicFeesConfig{}, ErrDynamicFeeConfigNotAvailable
	}

	if customDynamicFeesConfig != nil {
		return *customDynamicFeesConfig, nil
	}
	return eUpgradeDynamicFeesConfig, nil
}

func ResetDynamicConfig(ctx *snow.Context, customFeesConfig *commonfee.DynamicFeesConfig) error {
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

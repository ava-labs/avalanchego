// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"fmt"
	"time"

	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/utils/timer/mockable"
	"github.com/ava-labs/avalanchego/vms/avm/config"
	"github.com/ava-labs/avalanchego/vms/avm/txs/fee"

	commonfee "github.com/ava-labs/avalanchego/vms/components/fee"
)

func NextBlockTime(parentBlkTime time.Time, clk *mockable.Clock) time.Time {
	// [timestamp] =max(now, parentTime)
	timestamp := clk.Time()
	if parentBlkTime.After(timestamp) {
		timestamp = parentBlkTime
	}
	return timestamp
}

// PickFeeCalculator creates either a static or a dynamic fee calculator,
// depending on the active upgrade.
//
// PickFeeCalculator does not modify [state].
func PickFeeCalculator(cfg *config.Config, codec codec.Manager, state ReadOnlyChain, parentBlkTime time.Time) (fee.Calculator, error) {
	return pickFeeCalculator(cfg, codec, state, parentBlkTime, false)
}

func PickBuildingFeeCalculator(cfg *config.Config, codec codec.Manager, state ReadOnlyChain, parentBlkTime time.Time) (fee.Calculator, error) {
	return pickFeeCalculator(cfg, codec, state, parentBlkTime, true)
}

func pickFeeCalculator(cfg *config.Config, codec codec.Manager, state ReadOnlyChain, parentBlkTime time.Time, building bool) (fee.Calculator, error) {
	var (
		childBlkTime = state.GetTimestamp()
		isEActive    = cfg.IsEActivated(childBlkTime)
	)

	if !isEActive {
		return fee.NewStaticCalculator(cfg.StaticConfig), nil
	}

	feesCfg, err := fee.GetDynamicConfig(isEActive)
	if err != nil {
		return nil, fmt.Errorf("failed retrieving dynamic fees config: %w", err)
	}
	currentGasCap, err := state.GetCurrentGasCap()
	if err != nil {
		return nil, fmt.Errorf("failed retrieving gas cap: %w", err)
	}
	gasCap, err := commonfee.GasCap(feesCfg, currentGasCap, parentBlkTime, childBlkTime)
	if err != nil {
		return nil, fmt.Errorf("failed updating gas cap: %w", err)
	}

	commonFeeCalc := commonfee.NewCalculator(feesCfg.FeeDimensionWeights, feesCfg.GasPrice, gasCap)
	if building {
		return fee.NewBuildingDynamicCalculator(commonFeeCalc, codec), nil
	}
	return fee.NewDynamicCalculator(commonFeeCalc, codec), nil
}

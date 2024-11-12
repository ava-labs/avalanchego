// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"fmt"
	"time"

	"github.com/ava-labs/avalanchego/utils/iterator"
	"github.com/ava-labs/avalanchego/utils/timer/mockable"
	"github.com/ava-labs/avalanchego/vms/components/gas"
	"github.com/ava-labs/avalanchego/vms/platformvm/config"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs/fee"
)

func NextBlockTime(state Chain, clk *mockable.Clock) (time.Time, bool, error) {
	var (
		timestamp  = clk.Time()
		parentTime = state.GetTimestamp()
	)
	if parentTime.After(timestamp) {
		timestamp = parentTime
	}
	// [timestamp] = max(now, parentTime)

	// If the NextStakerChangeTime is after timestamp, then we shouldn't return
	// that the time was capped.
	nextStakerChangeTimeCap := timestamp.Add(time.Second)
	nextStakerChangeTime, err := GetNextStakerChangeTime(state, nextStakerChangeTimeCap)
	if err != nil {
		return time.Time{}, false, fmt.Errorf("failed getting next staker change time: %w", err)
	}

	// timeWasCapped means that [timestamp] was reduced to [nextStakerChangeTime]
	timeWasCapped := !timestamp.Before(nextStakerChangeTime)
	if timeWasCapped {
		timestamp = nextStakerChangeTime
	}
	// [timestamp] = min(max(now, parentTime), nextStakerChangeTime)
	return timestamp, timeWasCapped, nil
}

// GetNextStakerChangeTime returns the next time a staker will be either added
// or removed to/from the current validator set. If the next staker change time
// is further in the future than [defaultTime], then [defaultTime] is returned.
func GetNextStakerChangeTime(state Chain, defaultTime time.Time) (time.Time, error) {
	currentIterator, err := state.GetCurrentStakerIterator()
	if err != nil {
		return time.Time{}, err
	}
	defer currentIterator.Release()

	pendingIterator, err := state.GetPendingStakerIterator()
	if err != nil {
		return time.Time{}, err
	}
	defer pendingIterator.Release()

	for _, it := range []iterator.Iterator[*Staker]{currentIterator, pendingIterator} {
		// If the iterator is empty, skip it
		if !it.Next() {
			continue
		}

		time := it.Value().NextTime
		if time.Before(defaultTime) {
			defaultTime = time
		}
	}
	return defaultTime, nil
}

// PickFeeCalculator creates either a static or a dynamic fee calculator,
// depending on the active upgrade.
//
// PickFeeCalculator does not modify [state].
func PickFeeCalculator(cfg *config.Config, state Chain) fee.Calculator {
	timestamp := state.GetTimestamp()
	if !cfg.UpgradeConfig.IsEtnaActivated(timestamp) {
		return NewStaticFeeCalculator(cfg, timestamp)
	}

	feeState := state.GetFeeState()
	gasPrice := gas.CalculatePrice(
		cfg.DynamicFeeConfig.MinPrice,
		feeState.Excess,
		cfg.DynamicFeeConfig.ExcessConversionConstant,
	)
	return fee.NewDynamicCalculator(
		cfg.DynamicFeeConfig.Weights,
		gasPrice,
	)
}

// NewStaticFeeCalculator creates a static fee calculator, with the config set
// to either the pre-AP3 or post-AP3 config.
func NewStaticFeeCalculator(cfg *config.Config, timestamp time.Time) fee.Calculator {
	config := cfg.StaticFeeConfig
	if !cfg.UpgradeConfig.IsApricotPhase3Activated(timestamp) {
		config.CreateSubnetTxFee = cfg.CreateAssetTxFee
		config.CreateBlockchainTxFee = cfg.CreateAssetTxFee
	}
	return fee.NewStaticCalculator(config)
}

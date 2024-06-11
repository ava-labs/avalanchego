// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"fmt"
	"time"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/utils/timer/mockable"
	"github.com/ava-labs/avalanchego/vms/platformvm/config"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs/fee"

	commonfees "github.com/ava-labs/avalanchego/vms/components/fees"
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

	nextStakerChangeTime, err := GetNextStakerChangeTime(state)
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
// or removed to/from the current validator set.
func GetNextStakerChangeTime(state Chain) (time.Time, error) {
	currentStakerIterator, err := state.GetCurrentStakerIterator()
	if err != nil {
		return time.Time{}, err
	}
	defer currentStakerIterator.Release()

	pendingStakerIterator, err := state.GetPendingStakerIterator()
	if err != nil {
		return time.Time{}, err
	}
	defer pendingStakerIterator.Release()

	hasCurrentStaker := currentStakerIterator.Next()
	hasPendingStaker := pendingStakerIterator.Next()
	switch {
	case hasCurrentStaker && hasPendingStaker:
		nextCurrentTime := currentStakerIterator.Value().NextTime
		nextPendingTime := pendingStakerIterator.Value().NextTime
		if nextCurrentTime.Before(nextPendingTime) {
			return nextCurrentTime, nil
		}
		return nextPendingTime, nil
	case hasCurrentStaker:
		return currentStakerIterator.Value().NextTime, nil
	case hasPendingStaker:
		return pendingStakerIterator.Value().NextTime, nil
	default:
		return time.Time{}, database.ErrNotFound
	}
}

// [PickFeeCalculator] creates either a static or a dynamic fee calculator, depending on the active upgrade
// [PickFeeCalculator] does not mnodify [state]
func PickFeeCalculator(cfg *config.Config, state Chain, parentBlkTime time.Time) (*fee.Calculator, error) {
	var (
		childBlkTime  = state.GetTimestamp()
		isEActive     = cfg.UpgradeConfig.IsEActivated(childBlkTime)
		staticFeeCfg  = cfg.StaticFeeConfig
		feeCalculator *fee.Calculator
	)

	if !isEActive {
		feeCalculator = fee.NewStaticCalculator(staticFeeCfg, cfg.UpgradeConfig, childBlkTime)
	} else {
		feesCfg := fee.GetDynamicConfig(isEActive)
		feesMan, err := updatedFeeManager(isEActive, state, parentBlkTime)
		if err != nil {
			return nil, fmt.Errorf("failed updating fee manager: %w", err)
		}
		feeCalculator = fee.NewDynamicCalculator(staticFeeCfg, feesMan, feesCfg.BlockMaxComplexity)
	}
	return feeCalculator, nil
}

func updatedFeeManager(isEActive bool, state Chain, parentBlkTime time.Time) (*commonfees.Manager, error) {
	var feeManager *commonfees.Manager
	if isEActive {
		excessComplexity, err := state.GetExcessComplexity()
		if err != nil {
			return nil, fmt.Errorf("failed retrieving excess complexity: %w", err)
		}
		childBlkTime := state.GetTimestamp()
		feeCfg := fee.GetDynamicConfig(isEActive)

		feeManager, err = commonfees.NewUpdatedManager(
			feeCfg,
			excessComplexity,
			parentBlkTime.Unix(),
			childBlkTime.Unix(),
		)
		if err != nil {
			return nil, fmt.Errorf("failed updating fee rates, %w", err)
		}
	} else {
		feeManager = commonfees.NewManager(commonfees.Empty)
	}
	return feeManager, nil
}

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
		childBlkTime = state.GetTimestamp()
		isEActive    = cfg.UpgradeConfig.IsEActivated(childBlkTime)
	)

	if !isEActive {
		return fee.NewStaticCalculator(cfg.StaticFeeConfig, cfg.UpgradeConfig, childBlkTime), nil
	}

	feesCfg, err := fee.GetDynamicConfig(isEActive)
	if err != nil {
		return nil, fmt.Errorf("failed retrieving dynamic fees config: %w", err)
	}

	feesMan, err := updatedFeeManager(feesCfg, state, parentBlkTime)
	if err != nil {
		return nil, fmt.Errorf("failed updating fee manager: %w", err)
	}

	currentGasCap, err := state.GetCurrentGasCap()
	if err != nil {
		return nil, fmt.Errorf("failed retrieving gas cap: %w", err)
	}

	elapsedTime := childBlkTime.Unix() - parentBlkTime.Unix()
	maxGas := commonfees.MaxGas(feesCfg, currentGasCap, uint64(elapsedTime))
	feeCalculator := fee.NewDynamicCalculator(feesMan, maxGas)
	return feeCalculator, nil
}

func updatedFeeManager(feesCfg commonfees.DynamicFeesConfig, state Chain, parentBlkTime time.Time) (*commonfees.Manager, error) {
	excessComplexity, err := state.GetExcessComplexity()
	if err != nil {
		return nil, fmt.Errorf("failed retrieving excess complexity: %w", err)
	}
	childBlkTime := state.GetTimestamp()
	feeManager, err := commonfees.NewUpdatedManager(
		feesCfg,
		excessComplexity,
		parentBlkTime.Unix(),
		childBlkTime.Unix(),
	)
	if err != nil {
		return nil, fmt.Errorf("failed updating fee rates, %w", err)
	}
	return feeManager, nil
}

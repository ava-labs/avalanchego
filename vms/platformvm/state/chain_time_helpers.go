// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"fmt"
	"time"

	"github.com/ava-labs/avalanchego/utils/iterator"
	"github.com/ava-labs/avalanchego/utils/math"
	"github.com/ava-labs/avalanchego/utils/timer/mockable"
	"github.com/ava-labs/avalanchego/vms/components/gas"
	"github.com/ava-labs/avalanchego/vms/platformvm/config"

	txfee "github.com/ava-labs/avalanchego/vms/platformvm/txs/fee"
	validatorfee "github.com/ava-labs/avalanchego/vms/platformvm/validators/fee"
)

func NextBlockTime(
	config validatorfee.Config,
	state Chain,
	clk *mockable.Clock,
) (time.Time, bool, error) {
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
	nextStakerChangeTime, err := GetNextStakerChangeTime(config, state, nextStakerChangeTimeCap)
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
func GetNextStakerChangeTime(
	config validatorfee.Config,
	state Chain,
	defaultTime time.Time,
) (time.Time, error) {
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

	sovIterator, err := state.GetActiveSubnetOnlyValidatorsIterator()
	if err != nil {
		return time.Time{}, err
	}
	defer sovIterator.Release()

	// If there are no SoVs, return
	if !sovIterator.Next() {
		return defaultTime, nil
	}

	var (
		currentTime = state.GetTimestamp()
		maxSeconds  = uint64(defaultTime.Sub(currentTime) / time.Second)
		sov         = sovIterator.Value()
		accruedFees = state.GetAccruedFees()
	)
	remainingFunds, err := math.Sub(sov.EndAccumulatedFee, accruedFees)
	if err != nil {
		return time.Time{}, err
	}

	feeState := validatorfee.State{
		Current: gas.Gas(state.NumActiveSubnetOnlyValidators()),
		Excess:  state.GetSoVExcess(),
	}
	remainingSeconds := feeState.SecondsRemaining(
		config,
		maxSeconds,
		remainingFunds,
	)

	deactivationTime := currentTime.Add(time.Duration(remainingSeconds) * time.Second)
	if deactivationTime.Before(defaultTime) {
		defaultTime = deactivationTime
	}
	return defaultTime, nil
}

// PickFeeCalculator creates either a static or a dynamic fee calculator,
// depending on the active upgrade.
//
// PickFeeCalculator does not modify [state].
func PickFeeCalculator(config *config.Config, state Chain) txfee.Calculator {
	timestamp := state.GetTimestamp()
	if !config.UpgradeConfig.IsEtnaActivated(timestamp) {
		return NewStaticFeeCalculator(config, timestamp)
	}

	feeState := state.GetFeeState()
	gasPrice := gas.CalculatePrice(
		config.DynamicFeeConfig.MinPrice,
		feeState.Excess,
		config.DynamicFeeConfig.ExcessConversionConstant,
	)
	return txfee.NewDynamicCalculator(
		config.DynamicFeeConfig.Weights,
		gasPrice,
	)
}

// NewStaticFeeCalculator creates a static fee calculator, with the config set
// to either the pre-AP3 or post-AP3 config.
func NewStaticFeeCalculator(config *config.Config, timestamp time.Time) txfee.Calculator {
	feeConfig := config.StaticFeeConfig
	if !config.UpgradeConfig.IsApricotPhase3Activated(timestamp) {
		feeConfig.CreateSubnetTxFee = config.CreateAssetTxFee
		feeConfig.CreateBlockchainTxFee = config.CreateAssetTxFee
	}
	return txfee.NewStaticCalculator(feeConfig)
}

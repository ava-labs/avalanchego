// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
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
// to or removed from the validator set. If the next staker change time is
// further in the future than [nextTime], then [nextTime] is returned.
func GetNextStakerChangeTime(
	config validatorfee.Config,
	state Chain,
	nextTime time.Time,
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
		if time.Before(nextTime) {
			nextTime = time
		}
	}

	return getNextL1ValidatorEvictionTime(config, state, nextTime)
}

func getNextL1ValidatorEvictionTime(
	config validatorfee.Config,
	state Chain,
	nextTime time.Time,
) (time.Time, error) {
	l1ValidatorIterator, err := state.GetActiveL1ValidatorsIterator()
	if err != nil {
		return time.Time{}, fmt.Errorf("could not iterate over active L1 validators: %w", err)
	}
	defer l1ValidatorIterator.Release()

	// If there are no L1 validators, return
	if !l1ValidatorIterator.Next() {
		return nextTime, nil
	}

	// Calculate the remaining funds that the next validator to evict has.
	var (
		// GetActiveL1ValidatorsIterator iterates in order of increasing
		// EndAccumulatedFee, so the first L1 validator is the next L1 validator
		// to evict.
		l1Validator = l1ValidatorIterator.Value()
		accruedFees = state.GetAccruedFees()
	)
	remainingFunds, err := math.Sub(l1Validator.EndAccumulatedFee, accruedFees)
	if err != nil {
		return time.Time{}, fmt.Errorf("could not calculate remaining funds: %w", err)
	}

	// Calculate how many seconds the remaining funds can last for.
	var (
		currentTime = state.GetTimestamp()
		maxSeconds  = uint64(nextTime.Sub(currentTime) / time.Second)
	)
	feeState := validatorfee.State{
		Current: gas.Gas(state.NumActiveL1Validators()),
		Excess:  state.GetL1ValidatorExcess(),
	}
	remainingSeconds := feeState.SecondsRemaining(
		config,
		maxSeconds,
		remainingFunds,
	)

	deactivationTime := currentTime.Add(time.Duration(remainingSeconds) * time.Second)
	if deactivationTime.Before(nextTime) {
		nextTime = deactivationTime
	}
	return nextTime, nil
}

// PickFeeCalculator creates either a simple or a dynamic fee calculator,
// depending on the active upgrade.
//
// PickFeeCalculator does not modify [state].
func PickFeeCalculator(config *config.Internal, state Chain) txfee.Calculator {
	timestamp := state.GetTimestamp()
	if !config.UpgradeConfig.IsEtnaActivated(timestamp) {
		return txfee.NewSimpleCalculator(0)
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

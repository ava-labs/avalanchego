// Copyright (C) 2023, Chain4Travel AG. All rights reserved.
// See the file LICENSE for licensing terms.

package executor

import (
	"time"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
)

// GetNextChainEventTime returns the next chain event time
// For example: stakers set changed, deposit expired, proposal expired
func GetNextChainEventTime(state state.Chain, stakerChangeTime time.Time) (time.Time, error) {
	earliestTime := stakerChangeTime
	nextDeferredStakerEndTime, err := getNextDeferredStakerEndTime(state)
	if err != nil && err != database.ErrNotFound {
		return time.Time{}, err
	}
	if err != database.ErrNotFound && nextDeferredStakerEndTime.Before(earliestTime) {
		earliestTime = nextDeferredStakerEndTime
	}

	depositUnlockTime, err := state.GetNextToUnlockDepositTime(nil)
	if err != nil && err != database.ErrNotFound {
		return time.Time{}, err
	}
	if err != database.ErrNotFound && depositUnlockTime.Before(earliestTime) {
		earliestTime = depositUnlockTime
	}

	proposalExpirationTime, err := state.GetNextProposalExpirationTime(nil)
	if err != nil && err != database.ErrNotFound {
		return time.Time{}, err
	}
	if err != database.ErrNotFound && proposalExpirationTime.Before(earliestTime) {
		earliestTime = proposalExpirationTime
	}

	finishedProposalIDs, err := state.GetProposalIDsToFinish()
	if err != nil {
		return time.Time{}, err
	}
	if len(finishedProposalIDs) > 0 {
		currentChainTime := state.GetTimestamp()
		if currentChainTime.Before(earliestTime) {
			earliestTime = currentChainTime
		}
	}

	return earliestTime, nil
}

func getNextDeferredStakerEndTime(state state.Chain) (time.Time, error) {
	deferredStakerIterator, err := state.GetDeferredStakerIterator()
	if err != nil {
		return time.Time{}, err
	}
	defer deferredStakerIterator.Release()
	if deferredStakerIterator.Next() {
		return deferredStakerIterator.Value().NextTime, nil
	}
	return time.Time{}, database.ErrNotFound
}

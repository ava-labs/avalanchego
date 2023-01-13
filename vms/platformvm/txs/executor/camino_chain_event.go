// Copyright (C) 2023, Chain4Travel AG. All rights reserved.
// See the file LICENSE for licensing terms.

package executor

import (
	"time"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
)

// GetNextChainEventTime returns the next chain event time
// For example: stakers set changed, deposit expired
func GetNextChainEventTime(state state.Chain, stakerChangeTime time.Time) (time.Time, error) {
	// return stakerChangeTime, nil
	nextDeferredStakerEndTime, err := getNextDeferredStakerEndTime(state)
	if err == database.ErrNotFound {
		return stakerChangeTime, nil
	} else if err != nil {
		return time.Time{}, err
	}

	if nextDeferredStakerEndTime.Before(stakerChangeTime) {
		return nextDeferredStakerEndTime, nil
	}

	return stakerChangeTime, nil
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

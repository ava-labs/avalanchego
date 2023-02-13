// Copyright (C) 2022-2023, Chain4Travel AG. All rights reserved.
// See the file LICENSE for licensing terms.

package builder

import (
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
)

func getNextPendingStakerToRemove(
	chainTimestamp time.Time,
	shouldRewardNextCurrentStaker bool,
	nextCurrentStaker *state.Staker,
	preferredState state.Chain,
) (ids.ID, bool, error) {
	pendingStakerIterator, err := preferredState.GetPendingStakerIterator()
	if err != nil {
		return ids.Empty, false, err
	}
	defer pendingStakerIterator.Release()

	if pendingStakerIterator.Next() {
		pendingStaker := pendingStakerIterator.Value()
		if shouldRewardNextCurrentStaker && !nextCurrentStaker.EndTime.After(pendingStaker.EndTime) {
			return nextCurrentStaker.TxID, shouldRewardNextCurrentStaker, nil
		}
		return pendingStaker.TxID, chainTimestamp.Equal(pendingStaker.EndTime), nil
	}

	return nextCurrentStaker.TxID, shouldRewardNextCurrentStaker, nil
}

// Copyright (C) 2022, Chain4Travel AG. All rights reserved.
//
// This file is a derived work, based on ava-labs code whose
// original notices appear below.
//
// It is distributed under the same license conditions as the
// original code from which it is derived.
//
// Much love to the original authors for their work.
// **********************************************************

// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
	"time"

	"github.com/chain4travel/caminogo/utils/timer/mockable"
)

type ValidatorState interface {
	CurrentStakerChainState() currentStakerChainState
	PendingStakerChainState() pendingStakerChainState
}

// getNextStakerChangeTime returns the next time that a staker set change should
// occur.
func getNextStakerChangeTime(vs ValidatorState) (time.Time, error) {
	earliest := mockable.MaxTime
	currentStakers := vs.CurrentStakerChainState()
	if currentStakers := currentStakers.Stakers(); len(currentStakers) > 0 {
		nextStakerToRemove := currentStakers[0]
		staker, ok := nextStakerToRemove.UnsignedTx.(TimedTx)
		if !ok {
			return time.Time{}, errWrongTxType
		}
		endTime := staker.EndTime()
		if endTime.Before(earliest) {
			earliest = endTime
		}
	}
	pendingStakers := vs.PendingStakerChainState()
	if pendingStakers := pendingStakers.Stakers(); len(pendingStakers) > 0 {
		nextStakerToAdd := pendingStakers[0]
		staker, ok := nextStakerToAdd.UnsignedTx.(TimedTx)
		if !ok {
			return time.Time{}, errWrongTxType
		}
		startTime := staker.StartTime()
		if startTime.Before(earliest) {
			earliest = startTime
		}
	}
	return earliest, nil
}

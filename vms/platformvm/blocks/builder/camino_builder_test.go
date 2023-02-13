// Copyright (C) 2022-2023, Chain4Travel AG. All rights reserved.
// See the file LICENSE for licensing terms.

package builder

import (
	"testing"
	"time"

	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
)

func TestGetNextStakerToRewardWithTwoIterations(t *testing.T) {
	type test struct {
		name                       string
		timestamp                  time.Time
		stateF                     func(*gomock.Controller) state.Chain
		firstExpectedTxID          ids.ID
		secondExpectedTxID         ids.ID
		firstExpectedShouldReward  bool
		secondExpectedShouldReward bool
		expectedErr                error
	}

	var (
		now         = time.Now()
		currentTxID = ids.GenerateTestID()
		pendingTxID = ids.GenerateTestID()
	)
	tests := []test{
		{
			name:      "End time reached for both next current and pending - first reward current and then pending",
			timestamp: now,
			stateF: func(ctrl *gomock.Controller) state.Chain {
				currentStakerIter := state.NewMockStakerIterator(ctrl)
				pendingStakerIter := state.NewMockStakerIterator(ctrl)

				currentStakerIter.EXPECT().Next().Return(true).AnyTimes()
				pendingStakerIter.EXPECT().Next().Return(true).AnyTimes()
				firstCurrentStakerIter := currentStakerIter.EXPECT().Value().Return(&state.Staker{
					TxID:     currentTxID,
					Priority: txs.PrimaryNetworkValidatorCurrentPriority,
					EndTime:  now,
				})
				secondCurrentStakerIter := currentStakerIter.EXPECT().Value().Return(&state.Staker{
					TxID:     currentTxID,
					Priority: txs.PrimaryNetworkValidatorCurrentPriority,
					EndTime:  now.Add(1 * time.Hour),
				})
				pendingStakerIter.EXPECT().Value().Return(&state.Staker{
					TxID:     pendingTxID,
					Priority: txs.PrimaryNetworkValidatorCurrentPriority,
					EndTime:  now,
				}).AnyTimes()
				gomock.InOrder(
					firstCurrentStakerIter,
					secondCurrentStakerIter,
				)
				currentStakerIter.EXPECT().Release().AnyTimes()
				pendingStakerIter.EXPECT().Release().AnyTimes()

				s := state.NewMockChain(ctrl)
				s.EXPECT().GetCurrentStakerIterator().Return(currentStakerIter, nil).AnyTimes()
				s.EXPECT().GetPendingStakerIterator().Return(pendingStakerIter, nil).AnyTimes()

				return s
			},
			firstExpectedTxID:          currentTxID,
			secondExpectedTxID:         pendingTxID,
			firstExpectedShouldReward:  true,
			secondExpectedShouldReward: true,
		},
		{
			name:      "End time reached only for pending - reward pending and then nothing",
			timestamp: now,
			stateF: func(ctrl *gomock.Controller) state.Chain {
				currentStakerIter := state.NewMockStakerIterator(ctrl)
				pendingStakerIter := state.NewMockStakerIterator(ctrl)

				currentStakerIter.EXPECT().Next().Return(true).AnyTimes()
				pendingStakerIter.EXPECT().Next().Return(true).AnyTimes()
				currentStakerIter.EXPECT().Value().Return(&state.Staker{
					TxID:     currentTxID,
					Priority: txs.PrimaryNetworkValidatorCurrentPriority,
					EndTime:  now.Add(1 * time.Hour),
				}).AnyTimes()
				firstPendingStakerIter := pendingStakerIter.EXPECT().Value().Return(&state.Staker{
					TxID:     pendingTxID,
					Priority: txs.PrimaryNetworkValidatorCurrentPriority,
					EndTime:  now,
				})
				secondPendingStakerIter := pendingStakerIter.EXPECT().Value().Return(&state.Staker{
					TxID:     pendingTxID,
					Priority: txs.PrimaryNetworkValidatorCurrentPriority,
					EndTime:  now.Add(1 * time.Hour),
				})
				gomock.InOrder(
					firstPendingStakerIter,
					secondPendingStakerIter,
				)
				currentStakerIter.EXPECT().Release().AnyTimes()
				pendingStakerIter.EXPECT().Release().AnyTimes()

				s := state.NewMockChain(ctrl)
				s.EXPECT().GetCurrentStakerIterator().Return(currentStakerIter, nil).AnyTimes()
				s.EXPECT().GetPendingStakerIterator().Return(pendingStakerIter, nil).AnyTimes()

				return s
			},
			firstExpectedTxID:          pendingTxID,
			secondExpectedTxID:         pendingTxID,
			firstExpectedShouldReward:  true,
			secondExpectedShouldReward: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()

			state := tt.stateF(ctrl)
			txID, shouldReward, err := getNextStakerToReward(tt.timestamp, state)
			if tt.expectedErr != nil {
				require.Equal(tt.expectedErr, err)
				return
			}
			require.NoError(err)
			require.Equal(tt.firstExpectedTxID, txID)
			require.Equal(tt.firstExpectedShouldReward, shouldReward)

			txID, shouldReward, err = getNextStakerToReward(tt.timestamp, state)
			if tt.expectedErr != nil {
				require.ErrorIs(err, tt.expectedErr)
				return
			}
			require.NoError(err)
			require.Equal(tt.secondExpectedTxID, txID)
			require.Equal(tt.secondExpectedShouldReward, shouldReward)
		})
	}
}

// Copyright (C) 2022-2023, Chain4Travel AG. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"errors"
	"testing"
	"time"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/vms/platformvm/deposit"
	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/require"
)

func TestDiffGetDeposit(t *testing.T) {
	parentStateID := ids.GenerateTestID()
	depositTxID := ids.GenerateTestID()
	deposit1 := &deposit.Deposit{Duration: 101}
	deposit2 := &deposit.Deposit{Duration: 102}
	testErr := errors.New("test err")

	tests := map[string]struct {
		diff            func(*gomock.Controller) *diff
		depositTxID     ids.ID
		expectedDeposit *deposit.Deposit
		expectedErr     error
	}{
		"Fail: deposit in removed": {
			diff: func(c *gomock.Controller) *diff {
				return &diff{
					stateVersions: NewMockVersions(c),
					caminoDiff: &caminoDiff{
						removedDeposits: map[ids.ID]*deposit.Deposit{
							depositTxID: deposit1,
						},
						modifiedDeposits: map[ids.ID]*deposit.Deposit{
							depositTxID: deposit2,
						},
					},
				}
			},
			depositTxID: depositTxID,
			expectedErr: database.ErrNotFound,
		},
		"OK: deposit in modified": {
			diff: func(c *gomock.Controller) *diff {
				return &diff{
					stateVersions: NewMockVersions(c),
					caminoDiff: &caminoDiff{
						modifiedDeposits: map[ids.ID]*deposit.Deposit{
							depositTxID: deposit1,
						},
					},
				}
			},
			depositTxID:     depositTxID,
			expectedDeposit: deposit1,
		},
		"OK: deposit in parent state": {
			diff: func(c *gomock.Controller) *diff {
				parentState := NewMockChain(c)
				parentState.EXPECT().GetDeposit(depositTxID).Return(deposit1, nil)
				return &diff{
					stateVersions: newMockStateVersions(c, parentStateID, parentState),
					parentID:      parentStateID,
					caminoDiff:    &caminoDiff{},
				}
			},
			depositTxID:     depositTxID,
			expectedDeposit: deposit1,
		},
		"Fail: no deposit in parent state": {
			diff: func(c *gomock.Controller) *diff {
				parentState := NewMockChain(c)
				parentState.EXPECT().GetDeposit(depositTxID).Return(nil, testErr)
				return &diff{
					stateVersions: newMockStateVersions(c, parentStateID, parentState),
					parentID:      parentStateID,
					caminoDiff:    &caminoDiff{},
				}
			},
			depositTxID: depositTxID,
			expectedErr: testErr,
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()
			actualDeposit, err := tt.diff(ctrl).GetDeposit(depositTxID)
			require.ErrorIs(t, err, tt.expectedErr)
			require.Equal(t, tt.expectedDeposit, actualDeposit)
		})
	}
}

func TestDiffSetDeposit(t *testing.T) {
	depositTxID := ids.GenerateTestID()
	deposit1 := &deposit.Deposit{Duration: 101, Amount: 1}

	tests := map[string]struct {
		diff         *diff
		depositTxID  ids.ID
		deposit      *deposit.Deposit
		expectedDiff *diff
	}{
		"OK": {
			diff: &diff{caminoDiff: &caminoDiff{
				modifiedDeposits: map[ids.ID]*deposit.Deposit{},
			}},
			depositTxID: depositTxID,
			deposit:     deposit1,
			expectedDiff: &diff{caminoDiff: &caminoDiff{
				modifiedDeposits: map[ids.ID]*deposit.Deposit{
					depositTxID: deposit1,
				},
			}},
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			tt.diff.SetDeposit(tt.depositTxID, tt.deposit)
			require.Equal(t, tt.expectedDiff, tt.diff)
		})
	}
}

func TestDiffRemoveDeposit(t *testing.T) {
	depositTxID := ids.GenerateTestID()
	deposit1 := &deposit.Deposit{Duration: 101, Amount: 1}

	tests := map[string]struct {
		diff         *diff
		depositTxID  ids.ID
		deposit      *deposit.Deposit
		expectedDiff *diff
	}{
		"OK": {
			diff: &diff{caminoDiff: &caminoDiff{
				removedDeposits: map[ids.ID]*deposit.Deposit{},
			}},
			depositTxID: depositTxID,
			deposit:     deposit1,
			expectedDiff: &diff{caminoDiff: &caminoDiff{
				removedDeposits: map[ids.ID]*deposit.Deposit{
					depositTxID: deposit1,
				},
			}},
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			tt.diff.RemoveDeposit(tt.depositTxID, tt.deposit)
			require.Equal(t, tt.expectedDiff, tt.diff)
		})
	}
}

func TestDiffGetNextToUnlockDepositTime(t *testing.T) {
	parentStateID := ids.GenerateTestID()
	earlyDepositTxID1 := ids.GenerateTestID()
	earlyDepositTxID2 := ids.GenerateTestID()
	midDepositTxID1 := ids.GenerateTestID()
	midDepositTxID2 := ids.GenerateTestID()
	laterDepositTxID1 := ids.GenerateTestID()
	laterDepositTxID2 := ids.GenerateTestID()
	earlyDeposit1 := &deposit.Deposit{Duration: 101, Amount: 1}
	earlyDeposit2 := &deposit.Deposit{Duration: 101, Amount: 2}
	midDeposit1 := &deposit.Deposit{Duration: 102, Amount: 1}
	midDeposit2 := &deposit.Deposit{Duration: 102, Amount: 2}
	laterDeposit1 := &deposit.Deposit{Duration: 103, Amount: 1}
	laterDeposit2 := &deposit.Deposit{Duration: 103, Amount: 2}

	tests := map[string]struct {
		diff                   func(*gomock.Controller) *diff
		expectedNextUnlockTime time.Time
		expectedErr            error
	}{
		"Fail: no deposits": {
			diff: func(c *gomock.Controller) *diff {
				parentState := NewMockChain(c)
				parentState.EXPECT().GetNextToUnlockDepositIDsAndTime().Return(nil, time.Time{}, database.ErrNotFound)
				return &diff{
					stateVersions: newMockStateVersions(c, parentStateID, parentState),
					parentID:      parentStateID,
					caminoDiff:    &caminoDiff{},
				}
			},
			expectedNextUnlockTime: time.Time{},
			expectedErr:            database.ErrNotFound,
		},
		"Fail: deposits in parent state only, but all removed": {
			diff: func(c *gomock.Controller) *diff {
				parentState := NewMockChain(c)
				parentState.EXPECT().GetNextToUnlockDepositIDsAndTime().Return(
					[]ids.ID{earlyDepositTxID2, laterDepositTxID2},
					earlyDeposit2.EndTime(),
					nil,
				)
				return &diff{
					stateVersions: newMockStateVersions(c, parentStateID, parentState),
					parentID:      parentStateID,
					caminoDiff: &caminoDiff{
						removedDeposits: map[ids.ID]*deposit.Deposit{
							earlyDepositTxID2: earlyDeposit2,
							laterDepositTxID2: laterDeposit2,
						},
					},
				}
			},
			expectedNextUnlockTime: time.Time{},
			expectedErr:            database.ErrNotFound,
		},
		"OK: deposits in modified only": {
			diff: func(c *gomock.Controller) *diff {
				parentState := NewMockChain(c)
				parentState.EXPECT().GetNextToUnlockDepositIDsAndTime().Return(nil, time.Time{}, database.ErrNotFound)
				return &diff{
					stateVersions: newMockStateVersions(c, parentStateID, parentState),
					parentID:      parentStateID,
					caminoDiff: &caminoDiff{
						modifiedDeposits: map[ids.ID]*deposit.Deposit{
							earlyDepositTxID1: earlyDeposit1,
							laterDepositTxID1: laterDeposit1,
						},
					},
				}
			},
			expectedNextUnlockTime: earlyDeposit1.EndTime(),
		},
		"OK: deposits in parent state only": {
			diff: func(c *gomock.Controller) *diff {
				parentState := NewMockChain(c)
				parentState.EXPECT().GetNextToUnlockDepositIDsAndTime().Return(
					[]ids.ID{earlyDepositTxID2, laterDepositTxID2},
					earlyDeposit2.EndTime(),
					nil,
				)
				return &diff{
					stateVersions: newMockStateVersions(c, parentStateID, parentState),
					parentID:      parentStateID,
					caminoDiff:    &caminoDiff{},
				}
			},
			expectedNextUnlockTime: earlyDeposit2.EndTime(),
		},
		"OK: deposits in modified (late) and parent state (mid, early)": {
			diff: func(c *gomock.Controller) *diff {
				parentState := NewMockChain(c)
				parentState.EXPECT().GetNextToUnlockDepositIDsAndTime().Return(
					[]ids.ID{midDepositTxID2, earlyDepositTxID2},
					earlyDeposit2.EndTime(),
					nil,
				)
				return &diff{
					stateVersions: newMockStateVersions(c, parentStateID, parentState),
					parentID:      parentStateID,
					caminoDiff: &caminoDiff{
						modifiedDeposits: map[ids.ID]*deposit.Deposit{
							laterDepositTxID1: laterDeposit1,
						},
					},
				}
			},
			expectedNextUnlockTime: earlyDeposit2.EndTime(),
		},
		"OK: deposits in modified (early, mid) and parent state (late)": {
			diff: func(c *gomock.Controller) *diff {
				parentState := NewMockChain(c)
				parentState.EXPECT().GetNextToUnlockDepositIDsAndTime().Return(
					[]ids.ID{laterDepositTxID2},
					laterDeposit2.EndTime(),
					nil,
				)
				return &diff{
					stateVersions: newMockStateVersions(c, parentStateID, parentState),
					parentID:      parentStateID,
					caminoDiff: &caminoDiff{
						modifiedDeposits: map[ids.ID]*deposit.Deposit{
							earlyDepositTxID1: earlyDeposit1,
							midDepositTxID1:   midDeposit1,
						},
					},
				}
			},
			expectedNextUnlockTime: earlyDeposit1.EndTime(),
		},
		"OK: deposits in modified (late) and parent state (early), but all removed": {
			diff: func(c *gomock.Controller) *diff {
				parentState := NewMockChain(c)
				parentState.EXPECT().GetNextToUnlockDepositIDsAndTime().Return(
					[]ids.ID{earlyDepositTxID2},
					earlyDeposit2.EndTime(),
					nil,
				)
				return &diff{
					stateVersions: newMockStateVersions(c, parentStateID, parentState),
					parentID:      parentStateID,
					caminoDiff: &caminoDiff{
						modifiedDeposits: map[ids.ID]*deposit.Deposit{
							laterDepositTxID1: laterDeposit1,
						},
						removedDeposits: map[ids.ID]*deposit.Deposit{
							earlyDepositTxID2: earlyDeposit2,
						},
					},
				}
			},
			expectedNextUnlockTime: laterDeposit1.EndTime(),
		},
		"OK: deposits in modified (late) and parent state (mid, early), but all removed": {
			diff: func(c *gomock.Controller) *diff {
				parentState := NewMockChain(c)
				parentState.EXPECT().GetNextToUnlockDepositIDsAndTime().Return(
					[]ids.ID{earlyDepositTxID2},
					earlyDeposit2.EndTime(),
					nil,
				)
				return &diff{
					stateVersions: newMockStateVersions(c, parentStateID, parentState),
					parentID:      parentStateID,
					caminoDiff: &caminoDiff{
						modifiedDeposits: map[ids.ID]*deposit.Deposit{
							laterDepositTxID1: laterDeposit1,
						},
						removedDeposits: map[ids.ID]*deposit.Deposit{
							earlyDepositTxID2: earlyDeposit2,
						},
					},
				}
			},
			expectedNextUnlockTime: laterDeposit1.EndTime(),
		},
		"OK: deposits in modified (early) and parent state (late), but all removed": {
			diff: func(c *gomock.Controller) *diff {
				parentState := NewMockChain(c)
				parentState.EXPECT().GetNextToUnlockDepositIDsAndTime().Return(
					[]ids.ID{laterDepositTxID2},
					laterDeposit2.EndTime(),
					nil,
				)
				return &diff{
					stateVersions: newMockStateVersions(c, parentStateID, parentState),
					parentID:      parentStateID,
					caminoDiff: &caminoDiff{
						modifiedDeposits: map[ids.ID]*deposit.Deposit{
							earlyDepositTxID1: earlyDeposit1,
						},
						removedDeposits: map[ids.ID]*deposit.Deposit{
							laterDepositTxID2: laterDeposit2,
						},
					},
				}
			},
			expectedNextUnlockTime: earlyDeposit1.EndTime(),
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()
			nextUnlockTime, err := tt.diff(ctrl).GetNextToUnlockDepositTime()
			require.ErrorIs(t, err, tt.expectedErr)
			require.Equal(t, tt.expectedNextUnlockTime, nextUnlockTime)
		})
	}
}

// func TestGetNextToUnlockDepositIDsAndTime(t *testing.T) {
// 	depositTxID1 := ids.GenerateTestID()
// 	depositTxID2 := ids.GenerateTestID()
// 	time1 := time.Unix(100, 0)

// 	tests := map[string]struct {
// 		caminoState            *caminoState
// 		expectedNextUnlockTime time.Time
// 		expectedNextUnlockIDs  []ids.ID
// 		expectedErr            error
// 	}{
// 		"Fail: no deposits": {
// 			caminoState:            &caminoState{},
// 			expectedNextUnlockTime: time.Time{},
// 			expectedErr:            database.ErrNotFound,
// 		},
// 		"OK": {
// 			caminoState: &caminoState{
// 				depositsNextToUnlockTime: &time1,
// 				depositsNextToUnlockIDs:  []ids.ID{depositTxID1, depositTxID2},
// 			},
// 			expectedNextUnlockTime: time1,
// 			expectedNextUnlockIDs:  []ids.ID{depositTxID1, depositTxID2},
// 		},
// 	}
// 	for name, tt := range tests {
// 		t.Run(name, func(t *testing.T) {
// 			nextUnlockIDs, nextUnlockTime, err := tt.caminoState.GetNextToUnlockDepositIDsAndTime()
// 			require.ErrorIs(t, err, tt.expectedErr)
// 			require.Equal(t, tt.expectedNextUnlockTime, nextUnlockTime)
// 			require.Equal(t, tt.expectedNextUnlockIDs, nextUnlockIDs)

// 		})
// 	}
// }

// TODO@ GetNextToUnlockDepositIDsAndTime

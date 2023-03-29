// Copyright (C) 2023, Chain4Travel AG. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"errors"
	"testing"
	"time"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/utils/timer/mockable"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/platformvm/deposit"
	"github.com/ava-labs/avalanchego/vms/platformvm/locked"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/require"
)

func TestDiffGetDeposit(t *testing.T) {
	parentStateID := ids.GenerateTestID()
	depositTxID := ids.GenerateTestID()
	deposit1 := &deposit.Deposit{Duration: 101}
	testErr := errors.New("test err")

	tests := map[string]struct {
		diff            func(*gomock.Controller) *diff
		depositTxID     ids.ID
		expectedDeposit *deposit.Deposit
		expectedErr     error
	}{
		"Fail: deposit removed": {
			diff: func(c *gomock.Controller) *diff {
				return &diff{
					stateVersions: NewMockVersions(c),
					caminoDiff: &caminoDiff{
						modifiedDeposits: map[ids.ID]*depositDiff{
							depositTxID: {Deposit: deposit1, removed: true},
						},
					},
				}
			},
			depositTxID: depositTxID,
			expectedErr: database.ErrNotFound,
		},
		"OK: deposit modified": {
			diff: func(c *gomock.Controller) *diff {
				return &diff{
					stateVersions: NewMockVersions(c),
					caminoDiff: &caminoDiff{
						modifiedDeposits: map[ids.ID]*depositDiff{
							depositTxID: {Deposit: deposit1},
						},
					},
				}
			},
			depositTxID:     depositTxID,
			expectedDeposit: deposit1,
		},
		"OK: deposit added": {
			diff: func(c *gomock.Controller) *diff {
				return &diff{
					stateVersions: NewMockVersions(c),
					caminoDiff: &caminoDiff{
						modifiedDeposits: map[ids.ID]*depositDiff{
							depositTxID: {Deposit: deposit1, added: true},
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

func TestDiffAddDeposit(t *testing.T) {
	depositTxID := ids.GenerateTestID()
	deposit1 := &deposit.Deposit{Duration: 101}

	tests := map[string]struct {
		diff         *diff
		depositTxID  ids.ID
		deposit      *deposit.Deposit
		expectedDiff *diff
	}{
		"OK": {
			diff: &diff{caminoDiff: &caminoDiff{
				modifiedDeposits: map[ids.ID]*depositDiff{},
			}},
			depositTxID: depositTxID,
			deposit:     deposit1,
			expectedDiff: &diff{caminoDiff: &caminoDiff{
				modifiedDeposits: map[ids.ID]*depositDiff{
					depositTxID: {Deposit: deposit1, added: true},
				},
			}},
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			tt.diff.AddDeposit(tt.depositTxID, tt.deposit)
			require.Equal(t, tt.expectedDiff, tt.diff)
		})
	}
}

func TestDiffModifyDeposit(t *testing.T) {
	depositTxID := ids.GenerateTestID()
	deposit1 := &deposit.Deposit{Duration: 101}

	tests := map[string]struct {
		diff         *diff
		depositTxID  ids.ID
		deposit      *deposit.Deposit
		expectedDiff *diff
	}{
		"OK": {
			diff: &diff{caminoDiff: &caminoDiff{
				modifiedDeposits: map[ids.ID]*depositDiff{},
			}},
			depositTxID: depositTxID,
			deposit:     deposit1,
			expectedDiff: &diff{caminoDiff: &caminoDiff{
				modifiedDeposits: map[ids.ID]*depositDiff{
					depositTxID: {Deposit: deposit1},
				},
			}},
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			tt.diff.ModifyDeposit(tt.depositTxID, tt.deposit)
			require.Equal(t, tt.expectedDiff, tt.diff)
		})
	}
}

func TestDiffRemoveDeposit(t *testing.T) {
	depositTxID := ids.GenerateTestID()
	deposit1 := &deposit.Deposit{Duration: 101}

	tests := map[string]struct {
		diff         *diff
		depositTxID  ids.ID
		deposit      *deposit.Deposit
		expectedDiff *diff
	}{
		"OK": {
			diff: &diff{caminoDiff: &caminoDiff{
				modifiedDeposits: map[ids.ID]*depositDiff{},
			}},
			depositTxID: depositTxID,
			deposit:     deposit1,
			expectedDiff: &diff{caminoDiff: &caminoDiff{
				modifiedDeposits: map[ids.ID]*depositDiff{
					depositTxID: {Deposit: deposit1, removed: true},
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
	earlyDepositTxID1 := ids.ID{1}
	earlyDepositTxID2 := ids.ID{2}
	midDepositTxID := ids.ID{11}
	lateDepositTxID1 := ids.ID{101}
	lateDepositTxID2 := ids.ID{102}
	earlyDeposit := &deposit.Deposit{Duration: 101}
	midDeposit := &deposit.Deposit{Duration: 102}
	lateDeposit := &deposit.Deposit{Duration: 103}

	tests := map[string]struct {
		diff                   func(*gomock.Controller, set.Set[ids.ID]) *diff
		removedDepositIDs      set.Set[ids.ID]
		expectedNextUnlockTime time.Time
		expectedErr            error
	}{
		"Fail: no deposits": {
			diff: func(c *gomock.Controller, removedDepositIDs set.Set[ids.ID]) *diff {
				parentState := NewMockChain(c)
				parentState.EXPECT().GetNextToUnlockDepositTime(removedDepositIDs).
					Return(mockable.MaxTime, database.ErrNotFound)
				return &diff{
					stateVersions: newMockStateVersions(c, parentStateID, parentState),
					parentID:      parentStateID,
					caminoDiff:    &caminoDiff{},
				}
			},
			expectedNextUnlockTime: mockable.MaxTime,
			expectedErr:            database.ErrNotFound,
		},
		"Fail: deposits in parent state only, but all removed": {
			diff: func(c *gomock.Controller, removedDepositIDs set.Set[ids.ID]) *diff {
				removedDepositIDs.Add(earlyDepositTxID1, earlyDepositTxID2)
				parentState := NewMockChain(c)
				parentState.EXPECT().GetNextToUnlockDepositTime(removedDepositIDs).
					Return(mockable.MaxTime, database.ErrNotFound)
				return &diff{
					stateVersions: newMockStateVersions(c, parentStateID, parentState),
					parentID:      parentStateID,
					caminoDiff: &caminoDiff{
						modifiedDeposits: map[ids.ID]*depositDiff{
							earlyDepositTxID1: {Deposit: earlyDeposit, removed: true},
							earlyDepositTxID2: {Deposit: earlyDeposit, removed: true},
						},
					},
				}
			},
			expectedNextUnlockTime: mockable.MaxTime,
			expectedErr:            database.ErrNotFound,
		},
		"OK: deposits in parent state only, but one removed": {
			diff: func(c *gomock.Controller, removedDepositIDs set.Set[ids.ID]) *diff {
				removedDepositIDs.Add(earlyDepositTxID1)
				parentState := NewMockChain(c)
				parentState.EXPECT().GetNextToUnlockDepositTime(removedDepositIDs).
					Return(earlyDeposit.EndTime(), nil)
				return &diff{
					stateVersions: newMockStateVersions(c, parentStateID, parentState),
					parentID:      parentStateID,
					caminoDiff: &caminoDiff{
						modifiedDeposits: map[ids.ID]*depositDiff{
							earlyDepositTxID1: {Deposit: earlyDeposit, removed: true},
						},
					},
				}
			},
			expectedNextUnlockTime: earlyDeposit.EndTime(),
		},
		"OK: deposits in added (late) and parent state (early), but all parent removed": {
			diff: func(c *gomock.Controller, removedDepositIDs set.Set[ids.ID]) *diff {
				removedDepositIDs.Add(earlyDepositTxID1)
				parentState := NewMockChain(c)
				parentState.EXPECT().GetNextToUnlockDepositTime(removedDepositIDs).
					Return(mockable.MaxTime, nil)
				return &diff{
					stateVersions: newMockStateVersions(c, parentStateID, parentState),
					parentID:      parentStateID,
					caminoDiff: &caminoDiff{
						modifiedDeposits: map[ids.ID]*depositDiff{
							lateDepositTxID1:  {Deposit: lateDeposit, added: true},
							earlyDepositTxID1: {Deposit: earlyDeposit, removed: true},
						},
					},
				}
			},
			expectedNextUnlockTime: lateDeposit.EndTime(),
		},
		"OK: deposits in added (late) and parent state (early), but one parent removed": {
			diff: func(c *gomock.Controller, removedDepositIDs set.Set[ids.ID]) *diff {
				removedDepositIDs.Add(earlyDepositTxID1)
				parentState := NewMockChain(c)
				parentState.EXPECT().GetNextToUnlockDepositTime(removedDepositIDs).
					Return(earlyDeposit.EndTime(), nil)
				return &diff{
					stateVersions: newMockStateVersions(c, parentStateID, parentState),
					parentID:      parentStateID,
					caminoDiff: &caminoDiff{
						modifiedDeposits: map[ids.ID]*depositDiff{
							lateDepositTxID1:  {Deposit: lateDeposit, added: true},
							earlyDepositTxID1: {Deposit: earlyDeposit, removed: true},
						},
					},
				}
			},
			expectedNextUnlockTime: earlyDeposit.EndTime(),
		},
		"OK: deposits in added (early) and parent state (late), but all parent removed": {
			diff: func(c *gomock.Controller, removedDepositIDs set.Set[ids.ID]) *diff {
				removedDepositIDs.Add(lateDepositTxID1)
				parentState := NewMockChain(c)
				parentState.EXPECT().GetNextToUnlockDepositTime(removedDepositIDs).
					Return(mockable.MaxTime, nil)
				return &diff{
					stateVersions: newMockStateVersions(c, parentStateID, parentState),
					parentID:      parentStateID,
					caminoDiff: &caminoDiff{
						modifiedDeposits: map[ids.ID]*depositDiff{
							earlyDepositTxID1: {Deposit: earlyDeposit, added: true},
							lateDepositTxID1:  {Deposit: lateDeposit, removed: true},
						},
					},
				}
			},
			expectedNextUnlockTime: earlyDeposit.EndTime(),
		},
		"OK: deposits in added (early) and parent state (late), but one removed": {
			diff: func(c *gomock.Controller, removedDepositIDs set.Set[ids.ID]) *diff {
				removedDepositIDs.Add(lateDepositTxID1)
				parentState := NewMockChain(c)
				parentState.EXPECT().GetNextToUnlockDepositTime(removedDepositIDs).
					Return(lateDeposit.EndTime(), nil)
				return &diff{
					stateVersions: newMockStateVersions(c, parentStateID, parentState),
					parentID:      parentStateID,
					caminoDiff: &caminoDiff{
						modifiedDeposits: map[ids.ID]*depositDiff{
							earlyDepositTxID1: {Deposit: earlyDeposit, added: true},
							lateDepositTxID1:  {Deposit: lateDeposit, removed: true},
						},
					},
				}
			},
			expectedNextUnlockTime: earlyDeposit.EndTime(),
		},
		"OK: deposits in added only": {
			diff: func(c *gomock.Controller, removedDepositIDs set.Set[ids.ID]) *diff {
				parentState := NewMockChain(c)
				parentState.EXPECT().GetNextToUnlockDepositTime(removedDepositIDs).
					Return(mockable.MaxTime, database.ErrNotFound)
				return &diff{
					stateVersions: newMockStateVersions(c, parentStateID, parentState),
					parentID:      parentStateID,
					caminoDiff: &caminoDiff{
						modifiedDeposits: map[ids.ID]*depositDiff{
							earlyDepositTxID1: {Deposit: earlyDeposit, added: true},
							lateDepositTxID1:  {Deposit: lateDeposit, added: true},
						},
					},
				}
			},
			expectedNextUnlockTime: earlyDeposit.EndTime(),
		},
		"OK: deposits in parent state only": {
			diff: func(c *gomock.Controller, removedDepositIDs set.Set[ids.ID]) *diff {
				parentState := NewMockChain(c)
				parentState.EXPECT().GetNextToUnlockDepositTime(removedDepositIDs).
					Return(earlyDeposit.EndTime(), nil)
				return &diff{
					stateVersions: newMockStateVersions(c, parentStateID, parentState),
					parentID:      parentStateID,
					caminoDiff:    &caminoDiff{},
				}
			},
			expectedNextUnlockTime: earlyDeposit.EndTime(),
		},
		"OK: deposits in added (late) and parent state (early)": {
			diff: func(c *gomock.Controller, removedDepositIDs set.Set[ids.ID]) *diff {
				parentState := NewMockChain(c)
				parentState.EXPECT().GetNextToUnlockDepositTime(removedDepositIDs).
					Return(earlyDeposit.EndTime(), nil)
				return &diff{
					stateVersions: newMockStateVersions(c, parentStateID, parentState),
					parentID:      parentStateID,
					caminoDiff: &caminoDiff{
						modifiedDeposits: map[ids.ID]*depositDiff{
							lateDepositTxID1: {Deposit: lateDeposit, added: true},
						},
					},
				}
			},
			expectedNextUnlockTime: earlyDeposit.EndTime(),
		},
		"OK: deposits in added (early, mid) and parent state (late)": {
			diff: func(c *gomock.Controller, removedDepositIDs set.Set[ids.ID]) *diff {
				parentState := NewMockChain(c)
				parentState.EXPECT().GetNextToUnlockDepositTime(removedDepositIDs).
					Return(lateDeposit.EndTime(), nil)
				return &diff{
					stateVersions: newMockStateVersions(c, parentStateID, parentState),
					parentID:      parentStateID,
					caminoDiff: &caminoDiff{
						modifiedDeposits: map[ids.ID]*depositDiff{
							earlyDepositTxID1: {Deposit: earlyDeposit, added: true},
							midDepositTxID:    {Deposit: midDeposit, added: true},
						},
					},
				}
			},
			expectedNextUnlockTime: earlyDeposit.EndTime(),
		},
		"Fail: deposits in parent state only, but all removed in arg": {
			diff: func(c *gomock.Controller, removedDepositIDs set.Set[ids.ID]) *diff {
				parentState := NewMockChain(c)
				parentState.EXPECT().GetNextToUnlockDepositTime(removedDepositIDs).
					Return(mockable.MaxTime, database.ErrNotFound)
				return &diff{
					stateVersions: newMockStateVersions(c, parentStateID, parentState),
					parentID:      parentStateID,
					caminoDiff:    &caminoDiff{},
				}
			},
			removedDepositIDs: set.Set[ids.ID]{
				earlyDepositTxID1: struct{}{},
			},
			expectedNextUnlockTime: mockable.MaxTime,
			expectedErr:            database.ErrNotFound,
		},
		"Fail: deposits in parent state only, but all removed (one in arg, one in diff)": {
			diff: func(c *gomock.Controller, removedDepositIDs set.Set[ids.ID]) *diff {
				removedDepositIDs.Add(earlyDepositTxID1)
				parentState := NewMockChain(c)
				parentState.EXPECT().GetNextToUnlockDepositTime(removedDepositIDs).
					Return(mockable.MaxTime, database.ErrNotFound)
				return &diff{
					stateVersions: newMockStateVersions(c, parentStateID, parentState),
					parentID:      parentStateID,
					caminoDiff: &caminoDiff{
						modifiedDeposits: map[ids.ID]*depositDiff{
							earlyDepositTxID1: {Deposit: earlyDeposit, removed: true},
						},
					},
				}
			},
			removedDepositIDs: set.Set[ids.ID]{
				earlyDepositTxID2: struct{}{},
			},
			expectedNextUnlockTime: mockable.MaxTime,
			expectedErr:            database.ErrNotFound,
		},
		"OK: deposits in added (late) and parent state (early), but all parent removed (one in arg, one in diff)": {
			diff: func(c *gomock.Controller, removedDepositIDs set.Set[ids.ID]) *diff {
				removedDepositIDs.Add(earlyDepositTxID1)
				parentState := NewMockChain(c)
				parentState.EXPECT().GetNextToUnlockDepositTime(removedDepositIDs).
					Return(mockable.MaxTime, nil)
				return &diff{
					stateVersions: newMockStateVersions(c, parentStateID, parentState),
					parentID:      parentStateID,
					caminoDiff: &caminoDiff{
						modifiedDeposits: map[ids.ID]*depositDiff{
							lateDepositTxID1:  {Deposit: lateDeposit, added: true},
							earlyDepositTxID1: {Deposit: earlyDeposit, removed: true},
						},
					},
				}
			},
			removedDepositIDs: set.Set[ids.ID]{
				earlyDepositTxID2: struct{}{},
			},
			expectedNextUnlockTime: lateDeposit.EndTime(),
		},
		"OK: deposits in added (late) and parent state (early), but all parent removed in arg": {
			diff: func(c *gomock.Controller, removedDepositIDs set.Set[ids.ID]) *diff {
				parentState := NewMockChain(c)
				parentState.EXPECT().GetNextToUnlockDepositTime(removedDepositIDs).
					Return(mockable.MaxTime, nil)
				return &diff{
					stateVersions: newMockStateVersions(c, parentStateID, parentState),
					parentID:      parentStateID,
					caminoDiff: &caminoDiff{
						modifiedDeposits: map[ids.ID]*depositDiff{
							lateDepositTxID1: {Deposit: lateDeposit, added: true},
						},
					},
				}
			},
			removedDepositIDs: set.Set[ids.ID]{
				earlyDepositTxID1: struct{}{},
			},
			expectedNextUnlockTime: lateDeposit.EndTime(),
		},
		"OK: deposits in added (late) and parent state (early), but some parent removed (one in arg, one in diff)": {
			diff: func(c *gomock.Controller, removedDepositIDs set.Set[ids.ID]) *diff {
				removedDepositIDs.Add(earlyDepositTxID1)
				parentState := NewMockChain(c)
				parentState.EXPECT().GetNextToUnlockDepositTime(removedDepositIDs).
					Return(earlyDeposit.EndTime(), nil)
				return &diff{
					stateVersions: newMockStateVersions(c, parentStateID, parentState),
					parentID:      parentStateID,
					caminoDiff: &caminoDiff{
						modifiedDeposits: map[ids.ID]*depositDiff{
							lateDepositTxID1:  {Deposit: lateDeposit, added: true},
							earlyDepositTxID1: {Deposit: earlyDeposit, removed: true},
						},
					},
				}
			},
			removedDepositIDs: set.Set[ids.ID]{
				earlyDepositTxID2: struct{}{},
			},
			expectedNextUnlockTime: earlyDeposit.EndTime(),
		},
		"OK: deposits in added (late) and parent state (early), but some parent removed in arg": {
			diff: func(c *gomock.Controller, removedDepositIDs set.Set[ids.ID]) *diff {
				parentState := NewMockChain(c)
				parentState.EXPECT().GetNextToUnlockDepositTime(removedDepositIDs).
					Return(earlyDeposit.EndTime(), nil)
				return &diff{
					stateVersions: newMockStateVersions(c, parentStateID, parentState),
					parentID:      parentStateID,
					caminoDiff: &caminoDiff{
						modifiedDeposits: map[ids.ID]*depositDiff{
							lateDepositTxID1: {Deposit: lateDeposit, added: true},
						},
					},
				}
			},
			removedDepositIDs: set.Set[ids.ID]{
				earlyDepositTxID1: struct{}{},
			},
			expectedNextUnlockTime: earlyDeposit.EndTime(),
		},
		"OK: deposits in added (early) and parent state (late), but all parent removed (one in arg, one in diff)": {
			diff: func(c *gomock.Controller, removedDepositIDs set.Set[ids.ID]) *diff {
				removedDepositIDs.Add(lateDepositTxID1)
				parentState := NewMockChain(c)
				parentState.EXPECT().GetNextToUnlockDepositTime(removedDepositIDs).
					Return(mockable.MaxTime, nil)
				return &diff{
					stateVersions: newMockStateVersions(c, parentStateID, parentState),
					parentID:      parentStateID,
					caminoDiff: &caminoDiff{
						modifiedDeposits: map[ids.ID]*depositDiff{
							earlyDepositTxID1: {Deposit: earlyDeposit, added: true},
							lateDepositTxID1:  {Deposit: lateDeposit, removed: true},
						},
					},
				}
			},
			removedDepositIDs: set.Set[ids.ID]{
				lateDepositTxID2: struct{}{},
			},
			expectedNextUnlockTime: earlyDeposit.EndTime(),
		},
		"OK: deposits in added (early) and parent state (late), but all parent removed in arg": {
			diff: func(c *gomock.Controller, removedDepositIDs set.Set[ids.ID]) *diff {
				parentState := NewMockChain(c)
				parentState.EXPECT().GetNextToUnlockDepositTime(removedDepositIDs).
					Return(mockable.MaxTime, nil)
				return &diff{
					stateVersions: newMockStateVersions(c, parentStateID, parentState),
					parentID:      parentStateID,
					caminoDiff: &caminoDiff{
						modifiedDeposits: map[ids.ID]*depositDiff{
							earlyDepositTxID1: {Deposit: earlyDeposit, added: true},
						},
					},
				}
			},
			removedDepositIDs: set.Set[ids.ID]{
				lateDepositTxID1: struct{}{},
			},
			expectedNextUnlockTime: earlyDeposit.EndTime(),
		},
		"OK: deposits in added (early) and parent state (late), but some removed (one in arg, one in diff)": {
			diff: func(c *gomock.Controller, removedDepositIDs set.Set[ids.ID]) *diff {
				removedDepositIDs.Add(lateDepositTxID1)
				parentState := NewMockChain(c)
				parentState.EXPECT().GetNextToUnlockDepositTime(removedDepositIDs).
					Return(lateDeposit.EndTime(), nil)
				return &diff{
					stateVersions: newMockStateVersions(c, parentStateID, parentState),
					parentID:      parentStateID,
					caminoDiff: &caminoDiff{
						modifiedDeposits: map[ids.ID]*depositDiff{
							earlyDepositTxID1: {Deposit: earlyDeposit, added: true},
							lateDepositTxID1:  {Deposit: lateDeposit, removed: true},
						},
					},
				}
			},
			removedDepositIDs: set.Set[ids.ID]{
				lateDepositTxID2: struct{}{},
			},
			expectedNextUnlockTime: earlyDeposit.EndTime(),
		},
		"OK: deposits in added (early) and parent state (late), but some removed in arg": {
			diff: func(c *gomock.Controller, removedDepositIDs set.Set[ids.ID]) *diff {
				removedDepositIDs.Add(lateDepositTxID1)
				parentState := NewMockChain(c)
				parentState.EXPECT().GetNextToUnlockDepositTime(removedDepositIDs).
					Return(lateDeposit.EndTime(), nil)
				return &diff{
					stateVersions: newMockStateVersions(c, parentStateID, parentState),
					parentID:      parentStateID,
					caminoDiff: &caminoDiff{
						modifiedDeposits: map[ids.ID]*depositDiff{
							earlyDepositTxID1: {Deposit: earlyDeposit, added: true},
						},
					},
				}
			},
			removedDepositIDs: set.Set[ids.ID]{
				lateDepositTxID1: struct{}{},
			},
			expectedNextUnlockTime: earlyDeposit.EndTime(),
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()
			nextUnlockTime, err := tt.diff(ctrl, tt.removedDepositIDs).GetNextToUnlockDepositTime(tt.removedDepositIDs)
			require.ErrorIs(t, err, tt.expectedErr)
			require.Equal(t, tt.expectedNextUnlockTime, nextUnlockTime)
		})
	}
}

func TestDiffGetNextToUnlockDepositIDsAndTime(t *testing.T) {
	parentStateID := ids.GenerateTestID()
	earlyDepositTxID1 := ids.ID{1}
	earlyDepositTxID2 := ids.ID{2}
	earlyDepositTxID3 := ids.ID{3}
	midDepositTxID := ids.ID{10}
	lateDepositTxID1 := ids.ID{101}
	lateDepositTxID2 := ids.ID{102}
	lateDepositTxID3 := ids.ID{103}
	earlyDeposit := &deposit.Deposit{Duration: 101}
	midDeposit := &deposit.Deposit{Duration: 102}
	lateDeposit := &deposit.Deposit{Duration: 103}

	tests := map[string]struct {
		diff                   func(*gomock.Controller, set.Set[ids.ID]) *diff
		removedDepositIDs      set.Set[ids.ID]
		expectedNextUnlockIDs  []ids.ID
		expectedNextUnlockTime time.Time
		expectedErr            error
	}{
		"Fail: no deposits": {
			diff: func(c *gomock.Controller, removedDepositIDs set.Set[ids.ID]) *diff {
				parentState := NewMockChain(c)
				parentState.EXPECT().GetNextToUnlockDepositIDsAndTime(removedDepositIDs).
					Return(nil, mockable.MaxTime, database.ErrNotFound)
				return &diff{
					stateVersions: newMockStateVersions(c, parentStateID, parentState),
					parentID:      parentStateID,
					caminoDiff:    &caminoDiff{},
				}
			},
			expectedNextUnlockTime: mockable.MaxTime,
			expectedErr:            database.ErrNotFound,
		},
		"Fail: deposits in parent state only, but all removed": {
			diff: func(c *gomock.Controller, removedDepositIDs set.Set[ids.ID]) *diff {
				removedDepositIDs.Add(earlyDepositTxID1, earlyDepositTxID2)
				parentState := NewMockChain(c)
				parentState.EXPECT().GetNextToUnlockDepositIDsAndTime(removedDepositIDs).
					Return(nil, mockable.MaxTime, database.ErrNotFound)
				return &diff{
					stateVersions: newMockStateVersions(c, parentStateID, parentState),
					parentID:      parentStateID,
					caminoDiff: &caminoDiff{
						modifiedDeposits: map[ids.ID]*depositDiff{
							earlyDepositTxID1: {Deposit: earlyDeposit, removed: true},
							earlyDepositTxID2: {Deposit: earlyDeposit, removed: true},
						},
					},
				}
			},
			expectedNextUnlockTime: mockable.MaxTime,
			expectedErr:            database.ErrNotFound,
		},
		"OK: deposits in parent state only, but one removed": {
			diff: func(c *gomock.Controller, removedDepositIDs set.Set[ids.ID]) *diff {
				removedDepositIDs.Add(earlyDepositTxID1)
				parentState := NewMockChain(c)
				parentState.EXPECT().GetNextToUnlockDepositIDsAndTime(removedDepositIDs).
					Return([]ids.ID{earlyDepositTxID2}, earlyDeposit.EndTime(), nil)
				return &diff{
					stateVersions: newMockStateVersions(c, parentStateID, parentState),
					parentID:      parentStateID,
					caminoDiff: &caminoDiff{
						modifiedDeposits: map[ids.ID]*depositDiff{
							earlyDepositTxID1: {Deposit: earlyDeposit, removed: true},
						},
					},
				}
			},
			expectedNextUnlockTime: earlyDeposit.EndTime(),
			expectedNextUnlockIDs:  []ids.ID{earlyDepositTxID2},
		},
		"OK: deposits in added (late) and parent state (early), but all parent removed": {
			diff: func(c *gomock.Controller, removedDepositIDs set.Set[ids.ID]) *diff {
				removedDepositIDs.Add(earlyDepositTxID1, earlyDepositTxID2)
				parentState := NewMockChain(c)
				parentState.EXPECT().GetNextToUnlockDepositIDsAndTime(removedDepositIDs).
					Return(nil, mockable.MaxTime, database.ErrNotFound)
				return &diff{
					stateVersions: newMockStateVersions(c, parentStateID, parentState),
					parentID:      parentStateID,
					caminoDiff: &caminoDiff{
						modifiedDeposits: map[ids.ID]*depositDiff{
							lateDepositTxID1:  {Deposit: lateDeposit, added: true},
							earlyDepositTxID1: {Deposit: earlyDeposit, removed: true},
							earlyDepositTxID2: {Deposit: earlyDeposit, removed: true},
						},
					},
				}
			},
			expectedNextUnlockIDs:  []ids.ID{lateDepositTxID1},
			expectedNextUnlockTime: lateDeposit.EndTime(),
		},
		"OK: deposits in added (late) and parent state (early), but one parent removed": {
			diff: func(c *gomock.Controller, removedDepositIDs set.Set[ids.ID]) *diff {
				removedDepositIDs.Add(earlyDepositTxID1)
				parentState := NewMockChain(c)
				parentState.EXPECT().GetNextToUnlockDepositIDsAndTime(removedDepositIDs).
					Return([]ids.ID{earlyDepositTxID2}, earlyDeposit.EndTime(), nil)
				return &diff{
					stateVersions: newMockStateVersions(c, parentStateID, parentState),
					parentID:      parentStateID,
					caminoDiff: &caminoDiff{
						modifiedDeposits: map[ids.ID]*depositDiff{
							lateDepositTxID1:  {Deposit: lateDeposit, added: true},
							earlyDepositTxID1: {Deposit: earlyDeposit, removed: true},
						},
					},
				}
			},
			expectedNextUnlockIDs:  []ids.ID{earlyDepositTxID2},
			expectedNextUnlockTime: earlyDeposit.EndTime(),
		},
		"OK: deposits in added (early) and parent state (late), but all parent removed": {
			diff: func(c *gomock.Controller, removedDepositIDs set.Set[ids.ID]) *diff {
				removedDepositIDs.Add(lateDepositTxID1, lateDepositTxID2)
				parentState := NewMockChain(c)
				parentState.EXPECT().GetNextToUnlockDepositIDsAndTime(removedDepositIDs).
					Return(nil, mockable.MaxTime, nil)
				return &diff{
					stateVersions: newMockStateVersions(c, parentStateID, parentState),
					parentID:      parentStateID,
					caminoDiff: &caminoDiff{
						modifiedDeposits: map[ids.ID]*depositDiff{
							earlyDepositTxID1: {Deposit: earlyDeposit, added: true},
							lateDepositTxID1:  {Deposit: lateDeposit, removed: true},
							lateDepositTxID2:  {Deposit: lateDeposit, removed: true},
						},
					},
				}
			},
			expectedNextUnlockIDs:  []ids.ID{earlyDepositTxID1},
			expectedNextUnlockTime: earlyDeposit.EndTime(),
		},
		"OK: deposits in added (early) and parent state (late), but one parent removed": {
			diff: func(c *gomock.Controller, removedDepositIDs set.Set[ids.ID]) *diff {
				removedDepositIDs.Add(lateDepositTxID1)
				parentState := NewMockChain(c)
				parentState.EXPECT().GetNextToUnlockDepositIDsAndTime(removedDepositIDs).
					Return([]ids.ID{lateDepositTxID2}, lateDeposit.EndTime(), nil)
				return &diff{
					stateVersions: newMockStateVersions(c, parentStateID, parentState),
					parentID:      parentStateID,
					caminoDiff: &caminoDiff{
						modifiedDeposits: map[ids.ID]*depositDiff{
							earlyDepositTxID1: {Deposit: earlyDeposit, added: true},
							lateDepositTxID1:  {Deposit: lateDeposit, removed: true},
						},
					},
				}
			},
			expectedNextUnlockIDs:  []ids.ID{earlyDepositTxID1},
			expectedNextUnlockTime: earlyDeposit.EndTime(),
		},
		"OK: deposits in added only (early, late)": {
			diff: func(c *gomock.Controller, removedDepositIDs set.Set[ids.ID]) *diff {
				parentState := NewMockChain(c)
				parentState.EXPECT().GetNextToUnlockDepositIDsAndTime(removedDepositIDs).Return(nil, mockable.MaxTime, database.ErrNotFound)
				return &diff{
					stateVersions: newMockStateVersions(c, parentStateID, parentState),
					parentID:      parentStateID,
					caminoDiff: &caminoDiff{
						modifiedDeposits: map[ids.ID]*depositDiff{
							earlyDepositTxID1: {Deposit: earlyDeposit, added: true},
							lateDepositTxID1:  {Deposit: lateDeposit, added: true},
						},
					},
				}
			},
			expectedNextUnlockIDs:  []ids.ID{earlyDepositTxID1},
			expectedNextUnlockTime: earlyDeposit.EndTime(),
		},
		"OK: deposits in parent state only": {
			diff: func(c *gomock.Controller, removedDepositIDs set.Set[ids.ID]) *diff {
				parentState := NewMockChain(c)
				parentState.EXPECT().GetNextToUnlockDepositIDsAndTime(removedDepositIDs).Return(
					[]ids.ID{earlyDepositTxID1, earlyDepositTxID2},
					earlyDeposit.EndTime(),
					nil,
				)
				return &diff{
					stateVersions: newMockStateVersions(c, parentStateID, parentState),
					parentID:      parentStateID,
					caminoDiff:    &caminoDiff{},
				}
			},
			expectedNextUnlockIDs:  []ids.ID{earlyDepositTxID1, earlyDepositTxID2},
			expectedNextUnlockTime: earlyDeposit.EndTime(),
		},
		"OK: deposits in added (late) and parent state (early)": {
			diff: func(c *gomock.Controller, removedDepositIDs set.Set[ids.ID]) *diff {
				parentState := NewMockChain(c)
				parentState.EXPECT().GetNextToUnlockDepositIDsAndTime(removedDepositIDs).Return(
					[]ids.ID{earlyDepositTxID1},
					earlyDeposit.EndTime(),
					nil,
				)
				return &diff{
					stateVersions: newMockStateVersions(c, parentStateID, parentState),
					parentID:      parentStateID,
					caminoDiff: &caminoDiff{
						modifiedDeposits: map[ids.ID]*depositDiff{
							lateDepositTxID1: {Deposit: lateDeposit, added: true},
						},
					},
				}
			},
			expectedNextUnlockIDs:  []ids.ID{earlyDepositTxID1},
			expectedNextUnlockTime: earlyDeposit.EndTime(),
		},
		"OK: deposits in added (early, mid) and parent state (late)": {
			diff: func(c *gomock.Controller, removedDepositIDs set.Set[ids.ID]) *diff {
				parentState := NewMockChain(c)
				parentState.EXPECT().GetNextToUnlockDepositIDsAndTime(removedDepositIDs).Return(
					[]ids.ID{lateDepositTxID1},
					lateDeposit.EndTime(),
					nil,
				)
				return &diff{
					stateVersions: newMockStateVersions(c, parentStateID, parentState),
					parentID:      parentStateID,
					caminoDiff: &caminoDiff{
						modifiedDeposits: map[ids.ID]*depositDiff{
							earlyDepositTxID1: {Deposit: earlyDeposit, added: true},
							midDepositTxID:    {Deposit: midDeposit, added: true},
						},
					},
				}
			},
			expectedNextUnlockIDs:  []ids.ID{earlyDepositTxID1},
			expectedNextUnlockTime: earlyDeposit.EndTime(),
		},
		"OK: deposits in added (early1, late) and parent state (early2)": {
			diff: func(c *gomock.Controller, removedDepositIDs set.Set[ids.ID]) *diff {
				parentState := NewMockChain(c)
				parentState.EXPECT().GetNextToUnlockDepositIDsAndTime(removedDepositIDs).Return(
					[]ids.ID{earlyDepositTxID2},
					earlyDeposit.EndTime(),
					nil,
				)
				return &diff{
					stateVersions: newMockStateVersions(c, parentStateID, parentState),
					parentID:      parentStateID,
					caminoDiff: &caminoDiff{
						modifiedDeposits: map[ids.ID]*depositDiff{
							earlyDepositTxID1: {Deposit: earlyDeposit, added: true},
							lateDepositTxID1:  {Deposit: lateDeposit, added: true},
						},
					},
				}
			},
			expectedNextUnlockIDs:  []ids.ID{earlyDepositTxID1, earlyDepositTxID2},
			expectedNextUnlockTime: earlyDeposit.EndTime(),
		},
		"Fail: deposits in parent state only, but all removed in arg": {
			diff: func(c *gomock.Controller, removedDepositIDs set.Set[ids.ID]) *diff {
				parentState := NewMockChain(c)
				parentState.EXPECT().GetNextToUnlockDepositIDsAndTime(removedDepositIDs).
					Return(nil, mockable.MaxTime, database.ErrNotFound)
				return &diff{
					stateVersions: newMockStateVersions(c, parentStateID, parentState),
					parentID:      parentStateID,
					caminoDiff:    &caminoDiff{},
				}
			},
			removedDepositIDs: set.Set[ids.ID]{
				earlyDepositTxID1: struct{}{},
			},
			expectedNextUnlockTime: mockable.MaxTime,
			expectedErr:            database.ErrNotFound,
		},
		"OK: deposits in parent state only, but one removed in arg": {
			diff: func(c *gomock.Controller, removedDepositIDs set.Set[ids.ID]) *diff {
				parentState := NewMockChain(c)
				parentState.EXPECT().GetNextToUnlockDepositIDsAndTime(removedDepositIDs).
					Return([]ids.ID{earlyDepositTxID2}, earlyDeposit.EndTime(), nil)
				return &diff{
					stateVersions: newMockStateVersions(c, parentStateID, parentState),
					parentID:      parentStateID,
					caminoDiff:    &caminoDiff{},
				}
			},
			removedDepositIDs: set.Set[ids.ID]{
				earlyDepositTxID1: struct{}{},
			},
			expectedNextUnlockTime: earlyDeposit.EndTime(),
			expectedNextUnlockIDs:  []ids.ID{earlyDepositTxID2},
		},
		"OK: deposits in added (late) and parent state (early), but all parent removed in arg": {
			diff: func(c *gomock.Controller, removedDepositIDs set.Set[ids.ID]) *diff {
				parentState := NewMockChain(c)
				parentState.EXPECT().GetNextToUnlockDepositIDsAndTime(removedDepositIDs).
					Return(nil, mockable.MaxTime, database.ErrNotFound)
				return &diff{
					stateVersions: newMockStateVersions(c, parentStateID, parentState),
					parentID:      parentStateID,
					caminoDiff: &caminoDiff{
						modifiedDeposits: map[ids.ID]*depositDiff{
							lateDepositTxID1: {Deposit: lateDeposit, added: true},
						},
					},
				}
			},
			removedDepositIDs: set.Set[ids.ID]{
				earlyDepositTxID1: struct{}{},
			},
			expectedNextUnlockIDs:  []ids.ID{lateDepositTxID1},
			expectedNextUnlockTime: lateDeposit.EndTime(),
		},
		"OK: deposits in added (late) and parent state (early), but one parent removed in arg": {
			diff: func(c *gomock.Controller, removedDepositIDs set.Set[ids.ID]) *diff {
				parentState := NewMockChain(c)
				parentState.EXPECT().GetNextToUnlockDepositIDsAndTime(removedDepositIDs).
					Return([]ids.ID{earlyDepositTxID2}, earlyDeposit.EndTime(), nil)
				return &diff{
					stateVersions: newMockStateVersions(c, parentStateID, parentState),
					parentID:      parentStateID,
					caminoDiff: &caminoDiff{
						modifiedDeposits: map[ids.ID]*depositDiff{
							lateDepositTxID1: {Deposit: lateDeposit, added: true},
						},
					},
				}
			},
			removedDepositIDs: set.Set[ids.ID]{
				earlyDepositTxID1: struct{}{},
			},
			expectedNextUnlockIDs:  []ids.ID{earlyDepositTxID2},
			expectedNextUnlockTime: earlyDeposit.EndTime(),
		},
		"OK: deposits in added (early) and parent state (late), but all parent removed in arg": {
			diff: func(c *gomock.Controller, removedDepositIDs set.Set[ids.ID]) *diff {
				parentState := NewMockChain(c)
				parentState.EXPECT().GetNextToUnlockDepositIDsAndTime(removedDepositIDs).
					Return(nil, mockable.MaxTime, nil)
				return &diff{
					stateVersions: newMockStateVersions(c, parentStateID, parentState),
					parentID:      parentStateID,
					caminoDiff: &caminoDiff{
						modifiedDeposits: map[ids.ID]*depositDiff{
							earlyDepositTxID1: {Deposit: earlyDeposit, added: true},
						},
					},
				}
			},
			removedDepositIDs: set.Set[ids.ID]{
				lateDepositTxID1: struct{}{},
			},
			expectedNextUnlockIDs:  []ids.ID{earlyDepositTxID1},
			expectedNextUnlockTime: earlyDeposit.EndTime(),
		},
		"OK: deposits in added (early) and parent state (late), but one parent removed in arg": {
			diff: func(c *gomock.Controller, removedDepositIDs set.Set[ids.ID]) *diff {
				parentState := NewMockChain(c)
				parentState.EXPECT().GetNextToUnlockDepositIDsAndTime(removedDepositIDs).
					Return([]ids.ID{lateDepositTxID2}, lateDeposit.EndTime(), nil)
				return &diff{
					stateVersions: newMockStateVersions(c, parentStateID, parentState),
					parentID:      parentStateID,
					caminoDiff: &caminoDiff{
						modifiedDeposits: map[ids.ID]*depositDiff{
							earlyDepositTxID1: {Deposit: earlyDeposit, added: true},
						},
					},
				}
			},
			removedDepositIDs: set.Set[ids.ID]{
				lateDepositTxID1: struct{}{},
			},
			expectedNextUnlockIDs:  []ids.ID{earlyDepositTxID1},
			expectedNextUnlockTime: earlyDeposit.EndTime(),
		},

		"Fail: deposits in parent state only, but all removed (one in arg, one in diff)": {
			diff: func(c *gomock.Controller, removedDepositIDs set.Set[ids.ID]) *diff {
				removedDepositIDs.Add(earlyDepositTxID1)
				parentState := NewMockChain(c)
				parentState.EXPECT().GetNextToUnlockDepositIDsAndTime(removedDepositIDs).
					Return(nil, mockable.MaxTime, database.ErrNotFound)
				return &diff{
					stateVersions: newMockStateVersions(c, parentStateID, parentState),
					parentID:      parentStateID,
					caminoDiff: &caminoDiff{
						modifiedDeposits: map[ids.ID]*depositDiff{
							earlyDepositTxID1: {Deposit: earlyDeposit, removed: true},
						},
					},
				}
			},
			removedDepositIDs: set.Set[ids.ID]{
				earlyDepositTxID2: struct{}{},
			},
			expectedNextUnlockTime: mockable.MaxTime,
			expectedErr:            database.ErrNotFound,
		},
		"OK: deposits in parent state only, but some removed (one in arg, one in diff)": {
			diff: func(c *gomock.Controller, removedDepositIDs set.Set[ids.ID]) *diff {
				removedDepositIDs.Add(earlyDepositTxID1)
				parentState := NewMockChain(c)
				parentState.EXPECT().GetNextToUnlockDepositIDsAndTime(removedDepositIDs).
					Return([]ids.ID{earlyDepositTxID3}, earlyDeposit.EndTime(), nil)
				return &diff{
					stateVersions: newMockStateVersions(c, parentStateID, parentState),
					parentID:      parentStateID,
					caminoDiff: &caminoDiff{
						modifiedDeposits: map[ids.ID]*depositDiff{
							earlyDepositTxID1: {Deposit: earlyDeposit, removed: true},
						},
					},
				}
			},
			removedDepositIDs: set.Set[ids.ID]{
				earlyDepositTxID2: struct{}{},
			},
			expectedNextUnlockTime: earlyDeposit.EndTime(),
			expectedNextUnlockIDs:  []ids.ID{earlyDepositTxID3},
		},
		"OK: deposits in added (late) and parent state (early), but all parent removed (one in arg, one in diff)": {
			diff: func(c *gomock.Controller, removedDepositIDs set.Set[ids.ID]) *diff {
				removedDepositIDs.Add(earlyDepositTxID1)
				parentState := NewMockChain(c)
				parentState.EXPECT().GetNextToUnlockDepositIDsAndTime(removedDepositIDs).
					Return(nil, mockable.MaxTime, database.ErrNotFound)
				return &diff{
					stateVersions: newMockStateVersions(c, parentStateID, parentState),
					parentID:      parentStateID,
					caminoDiff: &caminoDiff{
						modifiedDeposits: map[ids.ID]*depositDiff{
							lateDepositTxID1:  {Deposit: lateDeposit, added: true},
							earlyDepositTxID1: {Deposit: lateDeposit, removed: true},
						},
					},
				}
			},
			removedDepositIDs: set.Set[ids.ID]{
				earlyDepositTxID2: struct{}{},
			},
			expectedNextUnlockIDs:  []ids.ID{lateDepositTxID1},
			expectedNextUnlockTime: lateDeposit.EndTime(),
		},
		"OK: deposits in added (late) and parent state (early), but some parent removed (one in arg, one in diff)": {
			diff: func(c *gomock.Controller, removedDepositIDs set.Set[ids.ID]) *diff {
				removedDepositIDs.Add(earlyDepositTxID1)
				parentState := NewMockChain(c)
				parentState.EXPECT().GetNextToUnlockDepositIDsAndTime(removedDepositIDs).
					Return([]ids.ID{earlyDepositTxID3}, earlyDeposit.EndTime(), nil)
				return &diff{
					stateVersions: newMockStateVersions(c, parentStateID, parentState),
					parentID:      parentStateID,
					caminoDiff: &caminoDiff{
						modifiedDeposits: map[ids.ID]*depositDiff{
							lateDepositTxID1:  {Deposit: lateDeposit, added: true},
							earlyDepositTxID1: {Deposit: lateDeposit, removed: true},
						},
					},
				}
			},
			removedDepositIDs: set.Set[ids.ID]{
				earlyDepositTxID2: struct{}{},
			},
			expectedNextUnlockIDs:  []ids.ID{earlyDepositTxID3},
			expectedNextUnlockTime: earlyDeposit.EndTime(),
		},
		"OK: deposits in added (early) and parent state (late), but all parent removed (one in arg, one in diff)": {
			diff: func(c *gomock.Controller, removedDepositIDs set.Set[ids.ID]) *diff {
				removedDepositIDs.Add(lateDepositTxID1)
				parentState := NewMockChain(c)
				parentState.EXPECT().GetNextToUnlockDepositIDsAndTime(removedDepositIDs).
					Return(nil, mockable.MaxTime, nil)
				return &diff{
					stateVersions: newMockStateVersions(c, parentStateID, parentState),
					parentID:      parentStateID,
					caminoDiff: &caminoDiff{
						modifiedDeposits: map[ids.ID]*depositDiff{
							earlyDepositTxID1: {Deposit: earlyDeposit, added: true},
							lateDepositTxID1:  {Deposit: lateDeposit, removed: true},
						},
					},
				}
			},
			removedDepositIDs: set.Set[ids.ID]{
				lateDepositTxID2: struct{}{},
			},
			expectedNextUnlockIDs:  []ids.ID{earlyDepositTxID1},
			expectedNextUnlockTime: earlyDeposit.EndTime(),
		},
		"OK: deposits in added (early) and parent state (late), but some parent removed (one in arg, one in diff)": {
			diff: func(c *gomock.Controller, removedDepositIDs set.Set[ids.ID]) *diff {
				removedDepositIDs.Add(lateDepositTxID1)
				parentState := NewMockChain(c)
				parentState.EXPECT().GetNextToUnlockDepositIDsAndTime(removedDepositIDs).
					Return([]ids.ID{lateDepositTxID3}, lateDeposit.EndTime(), nil)
				return &diff{
					stateVersions: newMockStateVersions(c, parentStateID, parentState),
					parentID:      parentStateID,
					caminoDiff: &caminoDiff{
						modifiedDeposits: map[ids.ID]*depositDiff{
							earlyDepositTxID1: {Deposit: earlyDeposit, added: true},
							lateDepositTxID1:  {Deposit: lateDeposit, removed: true},
						},
					},
				}
			},
			removedDepositIDs: set.Set[ids.ID]{
				lateDepositTxID2: struct{}{},
			},
			expectedNextUnlockIDs:  []ids.ID{earlyDepositTxID1},
			expectedNextUnlockTime: earlyDeposit.EndTime(),
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()
			nextUnlockIDs, nextUnlockTime, err := tt.diff(ctrl, tt.removedDepositIDs).GetNextToUnlockDepositIDsAndTime(tt.removedDepositIDs)
			require.ErrorIs(t, err, tt.expectedErr)
			require.Equal(t, tt.expectedNextUnlockTime, nextUnlockTime)
			require.Equal(t, tt.expectedNextUnlockIDs, nextUnlockIDs)
		})
	}
}

func TestDiffLockedUTXOs(t *testing.T) {
	parentStateID := ids.GenerateTestID()
	bondTxID := ids.ID{0, 1}
	owner := secp256k1fx.OutputOwners{Threshold: 1, Addrs: []ids.ShortID{{1}}}
	assetID := ids.ID{}
	lockTxIDs := set.Set[ids.ID]{bondTxID: struct{}{}}
	addresses := set.Set[ids.ShortID]{owner.Addrs[0]: struct{}{}}
	lockState := locked.StateBonded

	parentUTXO1 := generateTestUTXO(ids.ID{1}, assetID, 1, owner, ids.Empty, bondTxID)
	parentUTXO2 := generateTestUTXO(ids.ID{2}, assetID, 1, owner, ids.Empty, bondTxID)
	parentUTXO3 := generateTestUTXO(ids.ID{3}, assetID, 1, owner, ids.Empty, bondTxID)
	parentUTXO4 := generateTestUTXO(ids.ID{4}, assetID, 1, owner, ids.Empty, bondTxID)
	parentUTXO5 := generateTestUTXO(ids.ID{5}, assetID, 1, owner, ids.Empty, bondTxID)
	addedUTXO1 := generateTestUTXO(ids.ID{6}, assetID, 1, owner, ids.Empty, bondTxID)
	addedUTXO2 := generateTestUTXO(ids.ID{7}, assetID, 1, owner, ids.Empty, bondTxID)
	addedUTXO3 := generateTestUTXO(ids.ID{8}, assetID, 1, owner, ids.Empty, ids.Empty)
	addedUTXO4 := generateTestUTXO(ids.ID{9}, assetID, 1, owner, ids.Empty, ids.Empty)
	removedUTXO1 := generateTestUTXO(ids.ID{10}, assetID, 1, owner, ids.Empty, ids.Empty)
	removedUTXO2 := generateTestUTXO(ids.ID{11}, assetID, 1, owner, ids.Empty, ids.Empty)
	parentUTXOs := []*avax.UTXO{parentUTXO1, parentUTXO2, parentUTXO3, parentUTXO4, parentUTXO5}

	tests := map[string]struct {
		diff          func(*testing.T, *gomock.Controller) *diff
		expectedUTXOs []*avax.UTXO
		expectedErr   error
	}{
		"OK": {
			diff: func(t *testing.T, c *gomock.Controller) *diff {
				parentState := NewMockChain(c)
				parentState.EXPECT().LockedUTXOs(lockTxIDs, addresses, lockState).Return(parentUTXOs, nil)
				return &diff{
					stateVersions: newMockStateVersions(c, parentStateID, parentState),
					parentID:      parentStateID,
					modifiedUTXOs: map[ids.ID]*utxoModification{
						addedUTXO3.InputID():   {utxoID: addedUTXO3.InputID(), utxo: addedUTXO3},
						addedUTXO4.InputID():   {utxoID: addedUTXO4.InputID(), utxo: addedUTXO4},
						removedUTXO1.InputID(): {utxoID: removedUTXO1.InputID()},
						removedUTXO2.InputID(): {utxoID: removedUTXO2.InputID()},
					},
				}
			},
			expectedUTXOs: parentUTXOs,
		},
		"OK: some utxos removed, some modified, some added": {
			diff: func(t *testing.T, c *gomock.Controller) *diff {
				parentState := NewMockChain(c)
				parentState.EXPECT().LockedUTXOs(lockTxIDs, addresses, lockState).Return(parentUTXOs, nil)
				return &diff{
					stateVersions: newMockStateVersions(c, parentStateID, parentState),
					parentID:      parentStateID,
					modifiedUTXOs: map[ids.ID]*utxoModification{
						parentUTXO1.InputID(): {utxoID: parentUTXO1.InputID()},
						parentUTXO2.InputID(): {utxoID: parentUTXO2.InputID()},
						parentUTXO3.InputID(): {utxoID: parentUTXO3.InputID(), utxo: &avax.UTXO{UTXOID: parentUTXO3.UTXOID}},
						parentUTXO4.InputID(): {utxoID: parentUTXO4.InputID(), utxo: &avax.UTXO{UTXOID: parentUTXO4.UTXOID}},
						addedUTXO1.InputID():  {utxoID: addedUTXO1.InputID(), utxo: addedUTXO1},
						addedUTXO2.InputID():  {utxoID: addedUTXO2.InputID(), utxo: addedUTXO2},
					},
				}
			},
			expectedUTXOs: []*avax.UTXO{
				{UTXOID: parentUTXO3.UTXOID},
				{UTXOID: parentUTXO4.UTXOID},
				parentUTXOs[4],
				addedUTXO1, addedUTXO2,
			},
		},
		"OK: all utxos removed": {
			diff: func(t *testing.T, c *gomock.Controller) *diff {
				modifiedUTXOs := map[ids.ID]*utxoModification{}
				for i := 0; i < len(parentUTXOs); i++ {
					modifiedUTXOs[parentUTXOs[i].InputID()] = &utxoModification{utxoID: parentUTXOs[i].InputID()}
				}
				parentState := NewMockChain(c)
				parentState.EXPECT().LockedUTXOs(lockTxIDs, addresses, lockState).Return(parentUTXOs, nil)
				return &diff{
					stateVersions: newMockStateVersions(c, parentStateID, parentState),
					parentID:      parentStateID,
					modifiedUTXOs: modifiedUTXOs,
				}
			},
			expectedUTXOs: []*avax.UTXO{},
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()
			utxos, err := tt.diff(t, ctrl).LockedUTXOs(lockTxIDs, addresses, locked.StateBonded)
			require.ErrorIs(t, err, tt.expectedErr)
			require.ElementsMatch(t, tt.expectedUTXOs, utxos)
		})
	}
}

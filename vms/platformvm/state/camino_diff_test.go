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
	"github.com/ava-labs/avalanchego/vms/components/multisig"
	"github.com/ava-labs/avalanchego/vms/platformvm/config"
	"github.com/ava-labs/avalanchego/vms/platformvm/dac"
	"github.com/ava-labs/avalanchego/vms/platformvm/deposit"
	"github.com/ava-labs/avalanchego/vms/platformvm/locked"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
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
		expectedDiff    func(*diff) *diff
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
			expectedDiff: func(actualDiff *diff) *diff {
				return &diff{
					stateVersions: actualDiff.stateVersions,
					caminoDiff: &caminoDiff{
						modifiedDeposits: map[ids.ID]*depositDiff{
							depositTxID: {Deposit: deposit1, removed: true},
						},
					},
				}
			},
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
			depositTxID: depositTxID,
			expectedDiff: func(actualDiff *diff) *diff {
				return &diff{
					stateVersions: actualDiff.stateVersions,
					caminoDiff: &caminoDiff{
						modifiedDeposits: map[ids.ID]*depositDiff{
							depositTxID: {Deposit: deposit1},
						},
					},
				}
			},
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
			depositTxID: depositTxID,
			expectedDiff: func(actualDiff *diff) *diff {
				return &diff{
					stateVersions: actualDiff.stateVersions,
					caminoDiff: &caminoDiff{
						modifiedDeposits: map[ids.ID]*depositDiff{
							depositTxID: {Deposit: deposit1, added: true},
						},
					},
				}
			},
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
			depositTxID: depositTxID,
			expectedDiff: func(actualDiff *diff) *diff {
				return &diff{
					stateVersions: actualDiff.stateVersions,
					parentID:      actualDiff.parentID,
					caminoDiff:    &caminoDiff{},
				}
			},
			expectedDeposit: deposit1,
		},
		"Fail: parent errored": {
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
			expectedDiff: func(actualDiff *diff) *diff {
				return &diff{
					stateVersions: actualDiff.stateVersions,
					parentID:      actualDiff.parentID,
					caminoDiff:    &caminoDiff{},
				}
			},
			expectedErr: testErr,
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()
			actualDiff := tt.diff(ctrl)
			actualDeposit, err := actualDiff.GetDeposit(depositTxID)
			require.ErrorIs(t, err, tt.expectedErr)
			require.Equal(t, tt.expectedDeposit, actualDeposit)
			require.Equal(t, tt.expectedDiff(actualDiff), actualDiff)
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
	testErr := errors.New("test err")

	tests := map[string]struct {
		diff                   func(*gomock.Controller, set.Set[ids.ID]) *diff
		removedDepositIDs      set.Set[ids.ID]
		expectedNextUnlockTime time.Time
		expectedDiff           func(*diff) *diff
		expectedErr            error
	}{
		"Fail: parent errored": {
			diff: func(c *gomock.Controller, removedDepositIDs set.Set[ids.ID]) *diff {
				parentState := NewMockChain(c)
				parentState.EXPECT().GetNextToUnlockDepositTime(nil).Return(time.Time{}, testErr)
				return &diff{
					stateVersions: newMockStateVersions(c, parentStateID, parentState),
					parentID:      parentStateID,
					caminoDiff:    &caminoDiff{},
				}
			},
			expectedDiff: func(actualDiff *diff) *diff {
				return &diff{
					stateVersions: actualDiff.stateVersions,
					parentID:      actualDiff.parentID,
					caminoDiff:    &caminoDiff{},
				}
			},
			expectedNextUnlockTime: time.Time{},
			expectedErr:            testErr,
		},
		"Fail: no deposits": {
			diff: func(c *gomock.Controller, removedDepositIDs set.Set[ids.ID]) *diff {
				parentState := NewMockChain(c)
				parentState.EXPECT().GetNextToUnlockDepositTime(nil).Return(mockable.MaxTime, database.ErrNotFound)
				return &diff{
					stateVersions: newMockStateVersions(c, parentStateID, parentState),
					parentID:      parentStateID,
					caminoDiff:    &caminoDiff{},
				}
			},
			expectedDiff: func(actualDiff *diff) *diff {
				return &diff{
					stateVersions: actualDiff.stateVersions,
					parentID:      actualDiff.parentID,
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
			expectedDiff: func(actualDiff *diff) *diff {
				return &diff{
					stateVersions: actualDiff.stateVersions,
					parentID:      actualDiff.parentID,
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
			expectedDiff: func(actualDiff *diff) *diff {
				return &diff{
					stateVersions: actualDiff.stateVersions,
					parentID:      actualDiff.parentID,
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
			expectedDiff: func(actualDiff *diff) *diff {
				return &diff{
					stateVersions: actualDiff.stateVersions,
					parentID:      actualDiff.parentID,
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
			expectedDiff: func(actualDiff *diff) *diff {
				return &diff{
					stateVersions: actualDiff.stateVersions,
					parentID:      actualDiff.parentID,
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
			expectedDiff: func(actualDiff *diff) *diff {
				return &diff{
					stateVersions: actualDiff.stateVersions,
					parentID:      actualDiff.parentID,
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
			expectedDiff: func(actualDiff *diff) *diff {
				return &diff{
					stateVersions: actualDiff.stateVersions,
					parentID:      actualDiff.parentID,
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
			expectedDiff: func(actualDiff *diff) *diff {
				return &diff{
					stateVersions: actualDiff.stateVersions,
					parentID:      actualDiff.parentID,
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
			expectedDiff: func(actualDiff *diff) *diff {
				return &diff{
					stateVersions: actualDiff.stateVersions,
					parentID:      actualDiff.parentID,
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
			expectedDiff: func(actualDiff *diff) *diff {
				return &diff{
					stateVersions: actualDiff.stateVersions,
					parentID:      actualDiff.parentID,
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
			expectedDiff: func(actualDiff *diff) *diff {
				return &diff{
					stateVersions: actualDiff.stateVersions,
					parentID:      actualDiff.parentID,
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
			expectedDiff: func(actualDiff *diff) *diff {
				return &diff{
					stateVersions: actualDiff.stateVersions,
					parentID:      actualDiff.parentID,
					caminoDiff:    &caminoDiff{},
				}
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
			expectedDiff: func(actualDiff *diff) *diff {
				return &diff{
					stateVersions: actualDiff.stateVersions,
					parentID:      actualDiff.parentID,
					caminoDiff: &caminoDiff{
						modifiedDeposits: map[ids.ID]*depositDiff{
							earlyDepositTxID1: {Deposit: earlyDeposit, removed: true},
						},
					},
				}
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
			expectedDiff: func(actualDiff *diff) *diff {
				return &diff{
					stateVersions: actualDiff.stateVersions,
					parentID:      actualDiff.parentID,
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
			expectedDiff: func(actualDiff *diff) *diff {
				return &diff{
					stateVersions: actualDiff.stateVersions,
					parentID:      actualDiff.parentID,
					caminoDiff: &caminoDiff{
						modifiedDeposits: map[ids.ID]*depositDiff{
							lateDepositTxID1: {Deposit: lateDeposit, added: true},
						},
					},
				}
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
			expectedDiff: func(actualDiff *diff) *diff {
				return &diff{
					stateVersions: actualDiff.stateVersions,
					parentID:      actualDiff.parentID,
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
			expectedDiff: func(actualDiff *diff) *diff {
				return &diff{
					stateVersions: actualDiff.stateVersions,
					parentID:      actualDiff.parentID,
					caminoDiff: &caminoDiff{
						modifiedDeposits: map[ids.ID]*depositDiff{
							lateDepositTxID1: {Deposit: lateDeposit, added: true},
						},
					},
				}
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
			expectedDiff: func(actualDiff *diff) *diff {
				return &diff{
					stateVersions: actualDiff.stateVersions,
					parentID:      actualDiff.parentID,
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
			expectedDiff: func(actualDiff *diff) *diff {
				return &diff{
					stateVersions: actualDiff.stateVersions,
					parentID:      actualDiff.parentID,
					caminoDiff: &caminoDiff{
						modifiedDeposits: map[ids.ID]*depositDiff{
							earlyDepositTxID1: {Deposit: earlyDeposit, added: true},
						},
					},
				}
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
			expectedDiff: func(actualDiff *diff) *diff {
				return &diff{
					stateVersions: actualDiff.stateVersions,
					parentID:      actualDiff.parentID,
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
			expectedDiff: func(actualDiff *diff) *diff {
				return &diff{
					stateVersions: actualDiff.stateVersions,
					parentID:      actualDiff.parentID,
					caminoDiff: &caminoDiff{
						modifiedDeposits: map[ids.ID]*depositDiff{
							earlyDepositTxID1: {Deposit: earlyDeposit, added: true},
						},
					},
				}
			},
			expectedNextUnlockTime: earlyDeposit.EndTime(),
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()
			actualDiff := tt.diff(ctrl, tt.removedDepositIDs)
			nextUnlockTime, err := actualDiff.GetNextToUnlockDepositTime(tt.removedDepositIDs)
			require.ErrorIs(t, err, tt.expectedErr)
			require.Equal(t, tt.expectedNextUnlockTime, nextUnlockTime)
			require.Equal(t, tt.expectedDiff(actualDiff), actualDiff)
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
	testErr := errors.New("test err")

	tests := map[string]struct {
		diff                   func(*gomock.Controller, set.Set[ids.ID]) *diff
		removedDepositIDs      set.Set[ids.ID]
		expectedDiff           func(*diff) *diff
		expectedNextUnlockIDs  []ids.ID
		expectedNextUnlockTime time.Time
		expectedErr            error
	}{
		"Fail: parent errored": {
			diff: func(c *gomock.Controller, removedDepositIDs set.Set[ids.ID]) *diff {
				parentState := NewMockChain(c)
				parentState.EXPECT().GetNextToUnlockDepositIDsAndTime(nil).Return(nil, time.Time{}, testErr)
				return &diff{
					stateVersions: newMockStateVersions(c, parentStateID, parentState),
					parentID:      parentStateID,
					caminoDiff:    &caminoDiff{},
				}
			},
			expectedDiff: func(actualDiff *diff) *diff {
				return &diff{
					stateVersions: actualDiff.stateVersions,
					parentID:      actualDiff.parentID,
					caminoDiff:    &caminoDiff{},
				}
			},
			expectedNextUnlockTime: time.Time{},
			expectedErr:            testErr,
		},
		"Fail: no deposits": {
			diff: func(c *gomock.Controller, removedDepositIDs set.Set[ids.ID]) *diff {
				parentState := NewMockChain(c)
				parentState.EXPECT().GetNextToUnlockDepositIDsAndTime(nil).
					Return(nil, mockable.MaxTime, database.ErrNotFound)
				return &diff{
					stateVersions: newMockStateVersions(c, parentStateID, parentState),
					parentID:      parentStateID,
					caminoDiff:    &caminoDiff{},
				}
			},
			expectedDiff: func(actualDiff *diff) *diff {
				return &diff{
					stateVersions: actualDiff.stateVersions,
					parentID:      actualDiff.parentID,
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
			expectedDiff: func(actualDiff *diff) *diff {
				return &diff{
					stateVersions: actualDiff.stateVersions,
					parentID:      actualDiff.parentID,
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
			expectedDiff: func(actualDiff *diff) *diff {
				return &diff{
					stateVersions: actualDiff.stateVersions,
					parentID:      actualDiff.parentID,
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
			expectedNextUnlockIDs: []ids.ID{lateDepositTxID1},
			expectedDiff: func(actualDiff *diff) *diff {
				return &diff{
					stateVersions: actualDiff.stateVersions,
					parentID:      actualDiff.parentID,
					caminoDiff: &caminoDiff{
						modifiedDeposits: map[ids.ID]*depositDiff{
							lateDepositTxID1:  {Deposit: lateDeposit, added: true},
							earlyDepositTxID1: {Deposit: earlyDeposit, removed: true},
							earlyDepositTxID2: {Deposit: earlyDeposit, removed: true},
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
			expectedNextUnlockIDs: []ids.ID{earlyDepositTxID2},
			expectedDiff: func(actualDiff *diff) *diff {
				return &diff{
					stateVersions: actualDiff.stateVersions,
					parentID:      actualDiff.parentID,
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
			expectedNextUnlockIDs: []ids.ID{earlyDepositTxID1},
			expectedDiff: func(actualDiff *diff) *diff {
				return &diff{
					stateVersions: actualDiff.stateVersions,
					parentID:      actualDiff.parentID,
					caminoDiff: &caminoDiff{
						modifiedDeposits: map[ids.ID]*depositDiff{
							earlyDepositTxID1: {Deposit: earlyDeposit, added: true},
							lateDepositTxID1:  {Deposit: lateDeposit, removed: true},
							lateDepositTxID2:  {Deposit: lateDeposit, removed: true},
						},
					},
				}
			},
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
			expectedNextUnlockIDs: []ids.ID{earlyDepositTxID1},
			expectedDiff: func(actualDiff *diff) *diff {
				return &diff{
					stateVersions: actualDiff.stateVersions,
					parentID:      actualDiff.parentID,
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
			expectedNextUnlockIDs: []ids.ID{earlyDepositTxID1},
			expectedDiff: func(actualDiff *diff) *diff {
				return &diff{
					stateVersions: actualDiff.stateVersions,
					parentID:      actualDiff.parentID,
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
			expectedNextUnlockIDs: []ids.ID{earlyDepositTxID1, earlyDepositTxID2},
			expectedDiff: func(actualDiff *diff) *diff {
				return &diff{
					stateVersions: actualDiff.stateVersions,
					parentID:      actualDiff.parentID,
					caminoDiff:    &caminoDiff{},
				}
			},
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
			expectedNextUnlockIDs: []ids.ID{earlyDepositTxID1},
			expectedDiff: func(actualDiff *diff) *diff {
				return &diff{
					stateVersions: actualDiff.stateVersions,
					parentID:      actualDiff.parentID,
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
			expectedNextUnlockIDs: []ids.ID{earlyDepositTxID1},
			expectedDiff: func(actualDiff *diff) *diff {
				return &diff{
					stateVersions: actualDiff.stateVersions,
					parentID:      actualDiff.parentID,
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
			expectedNextUnlockIDs: []ids.ID{earlyDepositTxID1, earlyDepositTxID2},
			expectedDiff: func(actualDiff *diff) *diff {
				return &diff{
					stateVersions: actualDiff.stateVersions,
					parentID:      actualDiff.parentID,
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
			expectedDiff: func(actualDiff *diff) *diff {
				return &diff{
					stateVersions: actualDiff.stateVersions,
					parentID:      actualDiff.parentID,
					caminoDiff:    &caminoDiff{},
				}
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
			expectedDiff: func(actualDiff *diff) *diff {
				return &diff{
					stateVersions: actualDiff.stateVersions,
					parentID:      actualDiff.parentID,
					caminoDiff:    &caminoDiff{},
				}
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
			expectedNextUnlockIDs: []ids.ID{lateDepositTxID1},
			expectedDiff: func(actualDiff *diff) *diff {
				return &diff{
					stateVersions: actualDiff.stateVersions,
					parentID:      actualDiff.parentID,
					caminoDiff: &caminoDiff{
						modifiedDeposits: map[ids.ID]*depositDiff{
							lateDepositTxID1: {Deposit: lateDeposit, added: true},
						},
					},
				}
			},
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
			expectedNextUnlockIDs: []ids.ID{earlyDepositTxID2},
			expectedDiff: func(actualDiff *diff) *diff {
				return &diff{
					stateVersions: actualDiff.stateVersions,
					parentID:      actualDiff.parentID,
					caminoDiff: &caminoDiff{
						modifiedDeposits: map[ids.ID]*depositDiff{
							lateDepositTxID1: {Deposit: lateDeposit, added: true},
						},
					},
				}
			},
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
			expectedNextUnlockIDs: []ids.ID{earlyDepositTxID1},
			expectedDiff: func(actualDiff *diff) *diff {
				return &diff{
					stateVersions: actualDiff.stateVersions,
					parentID:      actualDiff.parentID,
					caminoDiff: &caminoDiff{
						modifiedDeposits: map[ids.ID]*depositDiff{
							earlyDepositTxID1: {Deposit: earlyDeposit, added: true},
						},
					},
				}
			},
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
			expectedNextUnlockIDs: []ids.ID{earlyDepositTxID1},
			expectedDiff: func(actualDiff *diff) *diff {
				return &diff{
					stateVersions: actualDiff.stateVersions,
					parentID:      actualDiff.parentID,
					caminoDiff: &caminoDiff{
						modifiedDeposits: map[ids.ID]*depositDiff{
							earlyDepositTxID1: {Deposit: earlyDeposit, added: true},
						},
					},
				}
			},
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
			expectedDiff: func(actualDiff *diff) *diff {
				return &diff{
					stateVersions: actualDiff.stateVersions,
					parentID:      actualDiff.parentID,
					caminoDiff: &caminoDiff{
						modifiedDeposits: map[ids.ID]*depositDiff{
							earlyDepositTxID1: {Deposit: earlyDeposit, removed: true},
						},
					},
				}
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
			expectedDiff: func(actualDiff *diff) *diff {
				return &diff{
					stateVersions: actualDiff.stateVersions,
					parentID:      actualDiff.parentID,
					caminoDiff: &caminoDiff{
						modifiedDeposits: map[ids.ID]*depositDiff{
							earlyDepositTxID1: {Deposit: earlyDeposit, removed: true},
						},
					},
				}
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
			expectedNextUnlockIDs: []ids.ID{lateDepositTxID1},
			expectedDiff: func(actualDiff *diff) *diff {
				return &diff{
					stateVersions: actualDiff.stateVersions,
					parentID:      actualDiff.parentID,
					caminoDiff: &caminoDiff{
						modifiedDeposits: map[ids.ID]*depositDiff{
							lateDepositTxID1:  {Deposit: lateDeposit, added: true},
							earlyDepositTxID1: {Deposit: lateDeposit, removed: true},
						},
					},
				}
			},
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
			expectedNextUnlockIDs: []ids.ID{earlyDepositTxID3},
			expectedDiff: func(actualDiff *diff) *diff {
				return &diff{
					stateVersions: actualDiff.stateVersions,
					parentID:      actualDiff.parentID,
					caminoDiff: &caminoDiff{
						modifiedDeposits: map[ids.ID]*depositDiff{
							lateDepositTxID1:  {Deposit: lateDeposit, added: true},
							earlyDepositTxID1: {Deposit: lateDeposit, removed: true},
						},
					},
				}
			},
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
			expectedNextUnlockIDs: []ids.ID{earlyDepositTxID1},
			expectedDiff: func(actualDiff *diff) *diff {
				return &diff{
					stateVersions: actualDiff.stateVersions,
					parentID:      actualDiff.parentID,
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
			expectedNextUnlockIDs: []ids.ID{earlyDepositTxID1},
			expectedDiff: func(actualDiff *diff) *diff {
				return &diff{
					stateVersions: actualDiff.stateVersions,
					parentID:      actualDiff.parentID,
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
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()
			actualDiff := tt.diff(ctrl, tt.removedDepositIDs)
			nextUnlockIDs, nextUnlockTime, err := actualDiff.GetNextToUnlockDepositIDsAndTime(tt.removedDepositIDs)
			require.ErrorIs(t, err, tt.expectedErr)
			require.Equal(t, tt.expectedNextUnlockTime, nextUnlockTime)
			require.Equal(t, tt.expectedNextUnlockIDs, nextUnlockIDs)
			require.Equal(t, tt.expectedDiff(actualDiff), actualDiff)
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
	testErr := errors.New("test err")

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
		expectedDiff  func(*diff) *diff
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
					modifiedUTXOs: map[ids.ID]*avax.UTXO{
						addedUTXO3.InputID():   addedUTXO3,
						addedUTXO4.InputID():   addedUTXO4,
						removedUTXO1.InputID(): nil,
						removedUTXO2.InputID(): nil,
					},
				}
			},
			expectedDiff: func(actualDiff *diff) *diff {
				return &diff{
					stateVersions: actualDiff.stateVersions,
					parentID:      actualDiff.parentID,
					modifiedUTXOs: map[ids.ID]*avax.UTXO{
						addedUTXO3.InputID():   addedUTXO3,
						addedUTXO4.InputID():   addedUTXO4,
						removedUTXO1.InputID(): nil,
						removedUTXO2.InputID(): nil,
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
					modifiedUTXOs: map[ids.ID]*avax.UTXO{
						parentUTXO1.InputID(): nil,
						parentUTXO2.InputID(): nil,
						parentUTXO3.InputID(): {UTXOID: parentUTXO3.UTXOID},
						parentUTXO4.InputID(): {UTXOID: parentUTXO4.UTXOID},
						addedUTXO1.InputID():  addedUTXO1,
						addedUTXO2.InputID():  addedUTXO2,
					},
				}
			},
			expectedDiff: func(actualDiff *diff) *diff {
				return &diff{
					stateVersions: actualDiff.stateVersions,
					parentID:      actualDiff.parentID,
					modifiedUTXOs: map[ids.ID]*avax.UTXO{
						parentUTXO1.InputID(): nil,
						parentUTXO2.InputID(): nil,
						parentUTXO3.InputID(): {UTXOID: parentUTXO3.UTXOID},
						parentUTXO4.InputID(): {UTXOID: parentUTXO4.UTXOID},
						addedUTXO1.InputID():  addedUTXO1,
						addedUTXO2.InputID():  addedUTXO2,
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
				modifiedUTXOs := map[ids.ID]*avax.UTXO{}
				for i := 0; i < len(parentUTXOs); i++ {
					modifiedUTXOs[parentUTXOs[i].InputID()] = nil
				}
				parentState := NewMockChain(c)
				parentState.EXPECT().LockedUTXOs(lockTxIDs, addresses, lockState).Return(parentUTXOs, nil)
				return &diff{
					stateVersions: newMockStateVersions(c, parentStateID, parentState),
					parentID:      parentStateID,
					modifiedUTXOs: modifiedUTXOs,
				}
			},
			expectedDiff: func(actualDiff *diff) *diff {
				modifiedUTXOs := map[ids.ID]*avax.UTXO{}
				for i := 0; i < len(parentUTXOs); i++ {
					modifiedUTXOs[parentUTXOs[i].InputID()] = nil
				}
				return &diff{
					stateVersions: actualDiff.stateVersions,
					parentID:      actualDiff.parentID,
					modifiedUTXOs: modifiedUTXOs,
				}
			},
			expectedUTXOs: []*avax.UTXO{},
		},
		"Fail: parent errored": {
			diff: func(t *testing.T, c *gomock.Controller) *diff {
				parentState := NewMockChain(c)
				parentState.EXPECT().LockedUTXOs(lockTxIDs, addresses, lockState).Return(nil, testErr)
				return &diff{
					stateVersions: newMockStateVersions(c, parentStateID, parentState),
					parentID:      parentStateID,
				}
			},
			expectedDiff: func(actualDiff *diff) *diff {
				return &diff{
					stateVersions: actualDiff.stateVersions,
					parentID:      actualDiff.parentID,
				}
			},
			expectedErr: testErr,
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()
			actualDiff := tt.diff(t, ctrl)
			utxos, err := actualDiff.LockedUTXOs(lockTxIDs, addresses, locked.StateBonded)
			require.ErrorIs(t, err, tt.expectedErr)
			require.ElementsMatch(t, tt.expectedUTXOs, utxos)
			require.Equal(t, tt.expectedDiff(actualDiff), actualDiff)
		})
	}
}

func TestDiffConfig(t *testing.T) {
	parentStateID := ids.GenerateTestID()
	testErr := errors.New("test err")

	tests := map[string]struct {
		diff           func(*gomock.Controller) *diff
		expectedDiff   func(*diff) *diff
		expectedConfig *config.Config
		expectedErr    error
	}{
		"OK": {
			diff: func(c *gomock.Controller) *diff {
				parentState := NewMockChain(c)
				parentState.EXPECT().Config().Return(&config.Config{TxFee: 111}, nil)
				return &diff{
					stateVersions: newMockStateVersions(c, parentStateID, parentState),
					parentID:      parentStateID,
				}
			},
			expectedDiff: func(actualDiff *diff) *diff {
				return &diff{
					stateVersions: actualDiff.stateVersions,
					parentID:      actualDiff.parentID,
				}
			},
			expectedConfig: &config.Config{TxFee: 111},
		},
		"Fail: parent errored": {
			diff: func(c *gomock.Controller) *diff {
				parentState := NewMockChain(c)
				parentState.EXPECT().Config().Return(nil, testErr)
				return &diff{
					stateVersions: newMockStateVersions(c, parentStateID, parentState),
					parentID:      parentStateID,
				}
			},
			expectedDiff: func(actualDiff *diff) *diff {
				return &diff{
					stateVersions: actualDiff.stateVersions,
					parentID:      actualDiff.parentID,
				}
			},
			expectedErr: testErr,
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()
			actualDiff := tt.diff(ctrl)
			config, err := actualDiff.Config()
			require.ErrorIs(t, err, tt.expectedErr)
			require.Equal(t, tt.expectedConfig, config)
			require.Equal(t, tt.expectedDiff(actualDiff), actualDiff)
		})
	}
}

func TestDiffCaminoConfig(t *testing.T) {
	parentStateID := ids.GenerateTestID()
	testErr := errors.New("test err")

	tests := map[string]struct {
		diff                 func(*gomock.Controller) *diff
		expectedDiff         func(*diff) *diff
		expectedCaminoConfig *CaminoConfig
		expectedErr          error
	}{
		"OK": {
			diff: func(c *gomock.Controller) *diff {
				parentState := NewMockChain(c)
				parentState.EXPECT().CaminoConfig().Return(&CaminoConfig{VerifyNodeSignature: true}, nil)
				return &diff{
					stateVersions: newMockStateVersions(c, parentStateID, parentState),
					parentID:      parentStateID,
				}
			},
			expectedDiff: func(actualDiff *diff) *diff {
				return &diff{
					stateVersions: actualDiff.stateVersions,
					parentID:      actualDiff.parentID,
				}
			},
			expectedCaminoConfig: &CaminoConfig{VerifyNodeSignature: true},
		},
		"Fail: parent errored": {
			diff: func(c *gomock.Controller) *diff {
				parentState := NewMockChain(c)
				parentState.EXPECT().CaminoConfig().Return(nil, testErr)
				return &diff{
					stateVersions: newMockStateVersions(c, parentStateID, parentState),
					parentID:      parentStateID,
				}
			},
			expectedDiff: func(actualDiff *diff) *diff {
				return &diff{
					stateVersions: actualDiff.stateVersions,
					parentID:      actualDiff.parentID,
				}
			},
			expectedErr: testErr,
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()
			actualDiff := tt.diff(ctrl)
			caminoConfig, err := actualDiff.CaminoConfig()
			require.ErrorIs(t, err, tt.expectedErr)
			require.Equal(t, tt.expectedCaminoConfig, caminoConfig)
			require.Equal(t, tt.expectedDiff(actualDiff), actualDiff)
		})
	}
}

func TestDiffSetAddressStates(t *testing.T) {
	addr1 := ids.ShortID{1}

	tests := map[string]struct {
		diff         *diff
		address      ids.ShortID
		states       txs.AddressState
		expectedDiff *diff
	}{
		"OK": {
			diff:    &diff{caminoDiff: &caminoDiff{modifiedAddressStates: map[ids.ShortID]txs.AddressState{}}},
			address: addr1,
			states:  111,
			expectedDiff: &diff{caminoDiff: &caminoDiff{
				modifiedAddressStates: map[ids.ShortID]txs.AddressState{
					addr1: 111,
				},
			}},
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			tt.diff.SetAddressStates(tt.address, tt.states)
			require.Equal(t, tt.expectedDiff, tt.diff)
		})
	}
}

func TestDiffGetAddressStates(t *testing.T) {
	parentStateID := ids.ID{123}
	addr1 := ids.ShortID{1}
	testErr := errors.New("test err")

	tests := map[string]struct {
		diff                 func(*gomock.Controller) *diff
		address              ids.ShortID
		expectedDiff         func(actualDiff *diff) *diff
		expectedAddresStates txs.AddressState
		expectedErr          error
	}{
		"OK: modified": {
			diff: func(c *gomock.Controller) *diff {
				return &diff{caminoDiff: &caminoDiff{
					modifiedAddressStates: map[ids.ShortID]txs.AddressState{
						addr1: 111,
					},
				}}
			},
			address: addr1,
			expectedDiff: func(actualDiff *diff) *diff {
				return &diff{caminoDiff: &caminoDiff{
					modifiedAddressStates: map[ids.ShortID]txs.AddressState{
						addr1: 111,
					},
				}}
			},
			expectedAddresStates: 111,
		},
		"OK: removed": {
			diff: func(c *gomock.Controller) *diff {
				return &diff{caminoDiff: &caminoDiff{
					modifiedAddressStates: map[ids.ShortID]txs.AddressState{
						addr1: 0,
					},
				}}
			},
			address: addr1,
			expectedDiff: func(actualDiff *diff) *diff {
				return &diff{caminoDiff: &caminoDiff{
					modifiedAddressStates: map[ids.ShortID]txs.AddressState{
						addr1: 0,
					},
				}}
			},
			expectedAddresStates: 0,
		},
		"OK: in parent": {
			diff: func(c *gomock.Controller) *diff {
				parentState := NewMockChain(c)
				parentState.EXPECT().GetAddressStates(addr1).Return(txs.AddressState(111), nil)
				return &diff{
					stateVersions: newMockStateVersions(c, parentStateID, parentState),
					parentID:      parentStateID,
					caminoDiff:    &caminoDiff{},
				}
			},
			address: addr1,
			expectedDiff: func(actualDiff *diff) *diff {
				return &diff{
					caminoDiff:    &caminoDiff{},
					parentID:      parentStateID,
					stateVersions: actualDiff.stateVersions,
				}
			},
			expectedAddresStates: 111,
		},
		"Fail: parent errored": {
			diff: func(c *gomock.Controller) *diff {
				parentState := NewMockChain(c)
				parentState.EXPECT().GetAddressStates(addr1).Return(txs.AddressStateEmpty, testErr)
				return &diff{
					stateVersions: newMockStateVersions(c, parentStateID, parentState),
					parentID:      parentStateID,
					caminoDiff:    &caminoDiff{},
				}
			},
			address: addr1,
			expectedDiff: func(actualDiff *diff) *diff {
				return &diff{
					stateVersions: actualDiff.stateVersions,
					parentID:      actualDiff.parentID,
					caminoDiff:    &caminoDiff{},
				}
			},
			expectedErr: testErr,
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()
			actualDiff := tt.diff(ctrl)
			addressStates, err := actualDiff.GetAddressStates(tt.address)
			require.ErrorIs(t, err, tt.expectedErr)
			require.Equal(t, tt.expectedAddresStates, addressStates)
			require.Equal(t, tt.expectedDiff(actualDiff), actualDiff)
		})
	}
}

func TestDiffSetDepositOffer(t *testing.T) {
	offer1 := &deposit.Offer{ID: ids.ID{12}}

	tests := map[string]struct {
		diff         *diff
		offer        *deposit.Offer
		states       uint64
		expectedDiff *diff
	}{
		"OK": {
			diff:  &diff{caminoDiff: &caminoDiff{modifiedDepositOffers: map[ids.ID]*deposit.Offer{}}},
			offer: offer1,
			expectedDiff: &diff{caminoDiff: &caminoDiff{
				modifiedDepositOffers: map[ids.ID]*deposit.Offer{
					offer1.ID: offer1,
				},
			}},
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			tt.diff.SetDepositOffer(tt.offer)
			require.Equal(t, tt.expectedDiff, tt.diff)
		})
	}
}

func TestDiffGetDepositOffer(t *testing.T) {
	parentStateID := ids.ID{123}
	offer1 := &deposit.Offer{ID: ids.ID{12}}
	testErr := errors.New("test err")

	tests := map[string]struct {
		diff          func(*gomock.Controller) *diff
		offerID       ids.ID
		expectedDiff  func(actualDiff *diff) *diff
		expectedOffer *deposit.Offer
		expectedErr   error
	}{
		"OK: modified": {
			diff: func(c *gomock.Controller) *diff {
				return &diff{caminoDiff: &caminoDiff{
					modifiedDepositOffers: map[ids.ID]*deposit.Offer{
						offer1.ID: offer1,
					},
				}}
			},
			offerID: offer1.ID,
			expectedDiff: func(actualDiff *diff) *diff {
				return &diff{caminoDiff: &caminoDiff{
					modifiedDepositOffers: map[ids.ID]*deposit.Offer{
						offer1.ID: offer1,
					},
				}}
			},
			expectedOffer: offer1,
		},
		"OK: in parent": {
			diff: func(c *gomock.Controller) *diff {
				parentState := NewMockChain(c)
				parentState.EXPECT().GetDepositOffer(offer1.ID).Return(offer1, nil)
				return &diff{
					stateVersions: newMockStateVersions(c, parentStateID, parentState),
					parentID:      parentStateID,
					caminoDiff:    &caminoDiff{},
				}
			},
			offerID: offer1.ID,
			expectedDiff: func(actualDiff *diff) *diff {
				return &diff{
					caminoDiff:    &caminoDiff{},
					parentID:      parentStateID,
					stateVersions: actualDiff.stateVersions,
				}
			},
			expectedOffer: offer1,
		},
		"Fail: removed": {
			diff: func(c *gomock.Controller) *diff {
				return &diff{caminoDiff: &caminoDiff{
					modifiedDepositOffers: map[ids.ID]*deposit.Offer{
						offer1.ID: nil,
					},
				}}
			},
			offerID: offer1.ID,
			expectedDiff: func(actualDiff *diff) *diff {
				return &diff{caminoDiff: &caminoDiff{
					modifiedDepositOffers: map[ids.ID]*deposit.Offer{
						offer1.ID: nil,
					},
				}}
			},
			expectedErr: database.ErrNotFound,
		},
		"Fail: parent errored": {
			diff: func(c *gomock.Controller) *diff {
				parentState := NewMockChain(c)
				parentState.EXPECT().GetDepositOffer(offer1.ID).Return(nil, testErr)
				return &diff{
					stateVersions: newMockStateVersions(c, parentStateID, parentState),
					parentID:      parentStateID,
					caminoDiff:    &caminoDiff{},
				}
			},
			offerID: offer1.ID,
			expectedDiff: func(actualDiff *diff) *diff {
				return &diff{
					stateVersions: actualDiff.stateVersions,
					parentID:      actualDiff.parentID,
					caminoDiff:    &caminoDiff{},
				}
			},
			expectedErr: testErr,
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()
			actualDiff := tt.diff(ctrl)
			offer, err := actualDiff.GetDepositOffer(tt.offerID)
			require.ErrorIs(t, err, tt.expectedErr)
			require.Equal(t, tt.expectedOffer, offer)
			require.Equal(t, tt.expectedDiff(actualDiff), actualDiff)
		})
	}
}

func TestDiffGetAllDepositOffers(t *testing.T) {
	parentStateID := ids.ID{123}
	offer1 := &deposit.Offer{ID: ids.ID{11}}
	offer2 := &deposit.Offer{ID: ids.ID{12}}
	offer2modified := &deposit.Offer{ID: ids.ID{12}, MinAmount: 1}
	offer3 := &deposit.Offer{ID: ids.ID{13}}
	offer4 := &deposit.Offer{ID: ids.ID{14}}
	testErr := errors.New("test err")

	tests := map[string]struct {
		diff           func(*gomock.Controller) *diff
		expectedDiff   func(actualDiff *diff) *diff
		expectedOffers []*deposit.Offer
		expectedErr    error
	}{
		"OK: in parent": {
			diff: func(c *gomock.Controller) *diff {
				parentState := NewMockChain(c)
				parentState.EXPECT().GetAllDepositOffers().Return([]*deposit.Offer{offer1, offer2, offer3}, nil)
				return &diff{
					stateVersions: newMockStateVersions(c, parentStateID, parentState),
					parentID:      parentStateID,
					caminoDiff: &caminoDiff{modifiedDepositOffers: map[ids.ID]*deposit.Offer{
						offer2.ID: offer2modified,
						offer3.ID: nil,
						offer4.ID: offer4,
					}},
				}
			},
			expectedDiff: func(actualDiff *diff) *diff {
				return &diff{
					stateVersions: actualDiff.stateVersions,
					parentID:      parentStateID,
					caminoDiff: &caminoDiff{modifiedDepositOffers: map[ids.ID]*deposit.Offer{
						offer2.ID: offer2modified,
						offer3.ID: nil,
						offer4.ID: offer4,
					}},
				}
			},
			expectedOffers: []*deposit.Offer{
				offer1, offer2modified, offer4,
			},
		},
		"OK: no offers": {
			diff: func(c *gomock.Controller) *diff {
				parentState := NewMockChain(c)
				parentState.EXPECT().GetAllDepositOffers().Return([]*deposit.Offer{}, nil)
				return &diff{
					stateVersions: newMockStateVersions(c, parentStateID, parentState),
					parentID:      parentStateID,
					caminoDiff:    &caminoDiff{},
				}
			},
			expectedDiff: func(actualDiff *diff) *diff {
				return &diff{
					stateVersions: actualDiff.stateVersions,
					parentID:      parentStateID,
					caminoDiff:    &caminoDiff{},
				}
			},
			expectedOffers: []*deposit.Offer{},
		},
		"Fail: parent errored": {
			diff: func(c *gomock.Controller) *diff {
				parentState := NewMockChain(c)
				parentState.EXPECT().GetAllDepositOffers().Return(nil, testErr)
				return &diff{
					stateVersions: newMockStateVersions(c, parentStateID, parentState),
					parentID:      parentStateID,
				}
			},
			expectedDiff: func(actualDiff *diff) *diff {
				return &diff{
					stateVersions: actualDiff.stateVersions,
					parentID:      parentStateID,
				}
			},
			expectedErr: testErr,
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()
			actualDiff := tt.diff(ctrl)
			offers, err := actualDiff.GetAllDepositOffers()
			require.ErrorIs(t, err, tt.expectedErr)
			require.ElementsMatch(t, tt.expectedOffers, offers)
			require.Equal(t, tt.expectedDiff(actualDiff), actualDiff)
		})
	}
}

func TestDiffSetMultisigAlias(t *testing.T) {
	alias1 := &multisig.AliasWithNonce{Alias: multisig.Alias{ID: ids.ShortID{12}}}

	tests := map[string]struct {
		diff         *diff
		alias        *multisig.AliasWithNonce
		expectedDiff *diff
	}{
		"OK": {
			diff:  &diff{caminoDiff: &caminoDiff{modifiedMultisigAliases: map[ids.ShortID]*multisig.AliasWithNonce{}}},
			alias: alias1,
			expectedDiff: &diff{caminoDiff: &caminoDiff{
				modifiedMultisigAliases: map[ids.ShortID]*multisig.AliasWithNonce{
					alias1.ID: alias1,
				},
			}},
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			tt.diff.SetMultisigAlias(tt.alias)
			require.Equal(t, tt.expectedDiff, tt.diff)
		})
	}
}

func TestDiffGetMultisigAlias(t *testing.T) {
	parentStateID := ids.ID{123}
	alias1 := &multisig.AliasWithNonce{Alias: multisig.Alias{ID: ids.ShortID{12}}}
	testErr := errors.New("test err")

	tests := map[string]struct {
		diff          func(*gomock.Controller) *diff
		aliasID       ids.ShortID
		expectedDiff  func(actualDiff *diff) *diff
		expectedAlias *multisig.AliasWithNonce
		expectedErr   error
	}{
		"OK: modified": {
			diff: func(c *gomock.Controller) *diff {
				return &diff{caminoDiff: &caminoDiff{
					modifiedMultisigAliases: map[ids.ShortID]*multisig.AliasWithNonce{
						alias1.ID: alias1,
					},
				}}
			},
			aliasID: alias1.ID,
			expectedDiff: func(actualDiff *diff) *diff {
				return &diff{caminoDiff: &caminoDiff{
					modifiedMultisigAliases: map[ids.ShortID]*multisig.AliasWithNonce{
						alias1.ID: alias1,
					},
				}}
			},
			expectedAlias: alias1,
		},
		"OK: in parent": {
			diff: func(c *gomock.Controller) *diff {
				parentState := NewMockChain(c)
				parentState.EXPECT().GetMultisigAlias(alias1.ID).Return(alias1, nil)
				return &diff{
					stateVersions: newMockStateVersions(c, parentStateID, parentState),
					parentID:      parentStateID,
					caminoDiff:    &caminoDiff{},
				}
			},
			aliasID: alias1.ID,
			expectedDiff: func(actualDiff *diff) *diff {
				return &diff{
					caminoDiff:    &caminoDiff{},
					parentID:      parentStateID,
					stateVersions: actualDiff.stateVersions,
				}
			},
			expectedAlias: alias1,
		},
		"Fail: removed": {
			diff: func(c *gomock.Controller) *diff {
				return &diff{caminoDiff: &caminoDiff{
					modifiedMultisigAliases: map[ids.ShortID]*multisig.AliasWithNonce{
						alias1.ID: nil,
					},
				}}
			},
			aliasID: alias1.ID,
			expectedDiff: func(actualDiff *diff) *diff {
				return &diff{caminoDiff: &caminoDiff{
					modifiedMultisigAliases: map[ids.ShortID]*multisig.AliasWithNonce{
						alias1.ID: nil,
					},
				}}
			},
			expectedErr: database.ErrNotFound,
		},
		"Fail: parent errored": {
			diff: func(c *gomock.Controller) *diff {
				parentState := NewMockChain(c)
				parentState.EXPECT().GetMultisigAlias(alias1.ID).Return(nil, testErr)
				return &diff{
					stateVersions: newMockStateVersions(c, parentStateID, parentState),
					parentID:      parentStateID,
					caminoDiff:    &caminoDiff{},
				}
			},
			aliasID: alias1.ID,
			expectedDiff: func(actualDiff *diff) *diff {
				return &diff{
					stateVersions: actualDiff.stateVersions,
					parentID:      actualDiff.parentID,
					caminoDiff:    &caminoDiff{},
				}
			},
			expectedErr: testErr,
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()
			actualDiff := tt.diff(ctrl)
			alias, err := actualDiff.GetMultisigAlias(tt.aliasID)
			require.ErrorIs(t, err, tt.expectedErr)
			require.Equal(t, tt.expectedAlias, alias)
			require.Equal(t, tt.expectedDiff(actualDiff), actualDiff)
		})
	}
}

func TestDiffSetShortIDLink(t *testing.T) {
	id1 := ids.ShortID{1}
	linkedID1 := ids.ShortID{12}

	tests := map[string]struct {
		diff         *diff
		id           ids.ShortID
		key          ShortLinkKey
		link         *ids.ShortID
		expectedDiff *diff
	}{
		"OK: nil": {
			diff: &diff{caminoDiff: &caminoDiff{modifiedShortLinks: map[ids.ID]*ids.ShortID{}}},
			id:   id1,
			key:  ShortLinkKeyRegisterNode,
			expectedDiff: &diff{caminoDiff: &caminoDiff{
				modifiedShortLinks: map[ids.ID]*ids.ShortID{
					toShortLinkKey(id1, ShortLinkKeyRegisterNode): nil,
				},
			}},
		},
		"OK": {
			diff: &diff{caminoDiff: &caminoDiff{modifiedShortLinks: map[ids.ID]*ids.ShortID{}}},
			id:   id1,
			key:  ShortLinkKeyRegisterNode,
			link: &linkedID1,
			expectedDiff: &diff{caminoDiff: &caminoDiff{
				modifiedShortLinks: map[ids.ID]*ids.ShortID{
					toShortLinkKey(id1, ShortLinkKeyRegisterNode): &linkedID1,
				},
			}},
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			tt.diff.SetShortIDLink(tt.id, tt.key, tt.link)
			require.Equal(t, tt.expectedDiff, tt.diff)
		})
	}
}

func TestDiffGetShortIDLink(t *testing.T) {
	parentStateID := ids.ID{123}
	id1 := ids.ShortID{1}
	linkedID := ids.ShortID{12}
	testErr := errors.New("test err")

	tests := map[string]struct {
		diff             func(*gomock.Controller) *diff
		id               ids.ShortID
		key              ShortLinkKey
		expectedDiff     func(actualDiff *diff) *diff
		expectedLinkedID ids.ShortID
		expectedErr      error
	}{
		"OK: modified": {
			diff: func(c *gomock.Controller) *diff {
				return &diff{caminoDiff: &caminoDiff{
					modifiedShortLinks: map[ids.ID]*ids.ShortID{
						toShortLinkKey(id1, ShortLinkKeyRegisterNode): &linkedID,
					},
				}}
			},
			id:  id1,
			key: ShortLinkKeyRegisterNode,
			expectedDiff: func(actualDiff *diff) *diff {
				return &diff{caminoDiff: &caminoDiff{
					modifiedShortLinks: map[ids.ID]*ids.ShortID{
						toShortLinkKey(id1, ShortLinkKeyRegisterNode): &linkedID,
					},
				}}
			},
			expectedLinkedID: linkedID,
		},
		"OK: in parent": {
			diff: func(c *gomock.Controller) *diff {
				parentState := NewMockChain(c)
				parentState.EXPECT().GetShortIDLink(id1, ShortLinkKeyRegisterNode).Return(linkedID, nil)
				return &diff{
					stateVersions: newMockStateVersions(c, parentStateID, parentState),
					parentID:      parentStateID,
					caminoDiff:    &caminoDiff{},
				}
			},
			id:  id1,
			key: ShortLinkKeyRegisterNode,
			expectedDiff: func(actualDiff *diff) *diff {
				return &diff{
					caminoDiff:    &caminoDiff{},
					parentID:      parentStateID,
					stateVersions: actualDiff.stateVersions,
				}
			},
			expectedLinkedID: linkedID,
		},
		"Fail: removed": {
			diff: func(c *gomock.Controller) *diff {
				return &diff{caminoDiff: &caminoDiff{
					modifiedShortLinks: map[ids.ID]*ids.ShortID{
						toShortLinkKey(id1, ShortLinkKeyRegisterNode): nil,
					},
				}}
			},
			id:  id1,
			key: ShortLinkKeyRegisterNode,
			expectedDiff: func(actualDiff *diff) *diff {
				return &diff{caminoDiff: &caminoDiff{
					modifiedShortLinks: map[ids.ID]*ids.ShortID{
						toShortLinkKey(id1, ShortLinkKeyRegisterNode): nil,
					},
				}}
			},
			expectedErr: database.ErrNotFound,
		},
		"Fail: parent errored": {
			diff: func(c *gomock.Controller) *diff {
				parentState := NewMockChain(c)
				parentState.EXPECT().GetShortIDLink(id1, ShortLinkKeyRegisterNode).Return(ids.ShortEmpty, testErr)
				return &diff{
					stateVersions: newMockStateVersions(c, parentStateID, parentState),
					parentID:      parentStateID,
					caminoDiff:    &caminoDiff{},
				}
			},
			id:  id1,
			key: ShortLinkKeyRegisterNode,
			expectedDiff: func(actualDiff *diff) *diff {
				return &diff{
					stateVersions: actualDiff.stateVersions,
					parentID:      actualDiff.parentID,
					caminoDiff:    &caminoDiff{},
				}
			},
			expectedErr: testErr,
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()
			actualDiff := tt.diff(ctrl)
			linkedID, err := actualDiff.GetShortIDLink(tt.id, tt.key)
			require.ErrorIs(t, err, tt.expectedErr)
			require.Equal(t, tt.expectedLinkedID, linkedID)
			require.Equal(t, tt.expectedDiff(actualDiff), actualDiff)
		})
	}
}

func TestDiffSetClaimable(t *testing.T) {
	ownerID1 := ids.ID{1}
	claimable1 := &Claimable{ValidatorReward: 1}

	tests := map[string]struct {
		diff         *diff
		ownerID      ids.ID
		claimable    *Claimable
		expectedDiff *diff
	}{
		"OK: nil": {
			diff:    &diff{caminoDiff: &caminoDiff{modifiedClaimables: map[ids.ID]*Claimable{}}},
			ownerID: ownerID1,
			expectedDiff: &diff{caminoDiff: &caminoDiff{
				modifiedClaimables: map[ids.ID]*Claimable{
					ownerID1: nil,
				},
			}},
		},
		"OK": {
			diff:      &diff{caminoDiff: &caminoDiff{modifiedClaimables: map[ids.ID]*Claimable{}}},
			ownerID:   ownerID1,
			claimable: claimable1,
			expectedDiff: &diff{caminoDiff: &caminoDiff{
				modifiedClaimables: map[ids.ID]*Claimable{
					ownerID1: claimable1,
				},
			}},
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			tt.diff.SetClaimable(tt.ownerID, tt.claimable)
			require.Equal(t, tt.expectedDiff, tt.diff)
		})
	}
}

func TestDiffGetClaimable(t *testing.T) {
	parentStateID := ids.ID{123}
	ownerID1 := ids.ID{1}
	claimable1 := &Claimable{ValidatorReward: 12}
	testErr := errors.New("test err")

	tests := map[string]struct {
		diff              func(*gomock.Controller) *diff
		ownerID           ids.ID
		expectedDiff      func(actualDiff *diff) *diff
		expectedClaimable *Claimable
		expectedErr       error
	}{
		"OK: modified": {
			diff: func(c *gomock.Controller) *diff {
				return &diff{caminoDiff: &caminoDiff{
					modifiedClaimables: map[ids.ID]*Claimable{
						ownerID1: claimable1,
					},
				}}
			},
			ownerID: ownerID1,
			expectedDiff: func(actualDiff *diff) *diff {
				return &diff{caminoDiff: &caminoDiff{
					modifiedClaimables: map[ids.ID]*Claimable{
						ownerID1: claimable1,
					},
				}}
			},
			expectedClaimable: claimable1,
		},
		"OK: in parent": {
			diff: func(c *gomock.Controller) *diff {
				parentState := NewMockChain(c)
				parentState.EXPECT().GetClaimable(ownerID1).Return(claimable1, nil)
				return &diff{
					stateVersions: newMockStateVersions(c, parentStateID, parentState),
					parentID:      parentStateID,
					caminoDiff:    &caminoDiff{},
				}
			},
			ownerID: ownerID1,
			expectedDiff: func(actualDiff *diff) *diff {
				return &diff{
					caminoDiff:    &caminoDiff{},
					parentID:      parentStateID,
					stateVersions: actualDiff.stateVersions,
				}
			},
			expectedClaimable: claimable1,
		},
		"Fail: removed": {
			diff: func(c *gomock.Controller) *diff {
				return &diff{caminoDiff: &caminoDiff{
					modifiedClaimables: map[ids.ID]*Claimable{
						ownerID1: nil,
					},
				}}
			},
			ownerID: ownerID1,
			expectedDiff: func(actualDiff *diff) *diff {
				return &diff{caminoDiff: &caminoDiff{
					modifiedClaimables: map[ids.ID]*Claimable{
						ownerID1: nil,
					},
				}}
			},
			expectedErr: database.ErrNotFound,
		},
		"Fail: parent errored": {
			diff: func(c *gomock.Controller) *diff {
				parentState := NewMockChain(c)
				parentState.EXPECT().GetClaimable(ownerID1).Return(nil, testErr)
				return &diff{
					stateVersions: newMockStateVersions(c, parentStateID, parentState),
					parentID:      parentStateID,
					caminoDiff:    &caminoDiff{},
				}
			},
			ownerID: ownerID1,
			expectedDiff: func(actualDiff *diff) *diff {
				return &diff{
					stateVersions: actualDiff.stateVersions,
					parentID:      actualDiff.parentID,
					caminoDiff:    &caminoDiff{},
				}
			},
			expectedErr: testErr,
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()
			actualDiff := tt.diff(ctrl)
			claimable, err := actualDiff.GetClaimable(tt.ownerID)
			require.ErrorIs(t, err, tt.expectedErr)
			require.Equal(t, tt.expectedClaimable, claimable)
			require.Equal(t, tt.expectedDiff(actualDiff), actualDiff)
		})
	}
}

func TestDiffSetNotDistributedValidatorReward(t *testing.T) {
	rewardAmount := uint64(111)

	tests := map[string]struct {
		diff         *diff
		reward       uint64
		expectedDiff *diff
	}{
		"OK": {
			diff:   &diff{caminoDiff: &caminoDiff{}},
			reward: rewardAmount,
			expectedDiff: &diff{caminoDiff: &caminoDiff{
				modifiedNotDistributedValidatorReward: &rewardAmount,
			}},
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			tt.diff.SetNotDistributedValidatorReward(tt.reward)
			require.Equal(t, tt.expectedDiff, tt.diff)
		})
	}
}

func TestDiffGetNotDistributedValidatorReward(t *testing.T) {
	parentStateID := ids.ID{123}
	reward := uint64(111)
	testErr := errors.New("test err")

	tests := map[string]struct {
		diff           func(*gomock.Controller) *diff
		expectedDiff   func(actualDiff *diff) *diff
		expectedReward uint64
		expectedErr    error
	}{
		"OK: modified": {
			diff: func(c *gomock.Controller) *diff {
				return &diff{caminoDiff: &caminoDiff{
					modifiedNotDistributedValidatorReward: &reward,
				}}
			},
			expectedDiff: func(actualDiff *diff) *diff {
				return &diff{caminoDiff: &caminoDiff{
					modifiedNotDistributedValidatorReward: &reward,
				}}
			},
			expectedReward: reward,
		},
		"OK: in parent": {
			diff: func(c *gomock.Controller) *diff {
				parentState := NewMockChain(c)
				parentState.EXPECT().GetNotDistributedValidatorReward().Return(reward, nil)
				return &diff{
					stateVersions: newMockStateVersions(c, parentStateID, parentState),
					parentID:      parentStateID,
					caminoDiff:    &caminoDiff{},
				}
			},
			expectedDiff: func(actualDiff *diff) *diff {
				return &diff{
					caminoDiff:    &caminoDiff{},
					parentID:      parentStateID,
					stateVersions: actualDiff.stateVersions,
				}
			},
			expectedReward: reward,
		},
		"Fail: parent errored": {
			diff: func(c *gomock.Controller) *diff {
				parentState := NewMockChain(c)
				parentState.EXPECT().GetNotDistributedValidatorReward().Return(uint64(0), testErr)
				return &diff{
					stateVersions: newMockStateVersions(c, parentStateID, parentState),
					parentID:      parentStateID,
					caminoDiff:    &caminoDiff{},
				}
			},
			expectedDiff: func(actualDiff *diff) *diff {
				return &diff{
					stateVersions: actualDiff.stateVersions,
					parentID:      actualDiff.parentID,
					caminoDiff:    &caminoDiff{},
				}
			},
			expectedErr: testErr,
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()
			actualDiff := tt.diff(ctrl)
			reward, err := actualDiff.GetNotDistributedValidatorReward()
			require.ErrorIs(t, err, tt.expectedErr)
			require.Equal(t, tt.expectedReward, reward)
			require.Equal(t, tt.expectedDiff(actualDiff), actualDiff)
		})
	}
}

func TestDiffGetDeferredValidator(t *testing.T) {
	parentStateID := ids.ID{123}
	subnetID1 := ids.ID{1, 1}
	nodeID1 := ids.NodeID{2, 2}
	staker := &Staker{TxID: ids.ID{1}}
	testErr := errors.New("test err")

	tests := map[string]struct {
		diff              func(*gomock.Controller) *diff
		subnetID          ids.ID
		nodeID            ids.NodeID
		expectedDiff      func(actualDiff *diff) *diff
		expectedValidator *Staker
		expectedErr       error
	}{
		"OK: modified": {
			diff: func(c *gomock.Controller) *diff {
				return &diff{caminoDiff: &caminoDiff{
					deferredStakerDiffs: diffStakers{validatorDiffs: map[ids.ID]map[ids.NodeID]*diffValidator{
						subnetID1: {nodeID1: {validator: staker, validatorStatus: added}},
					}},
				}}
			},
			subnetID: subnetID1,
			nodeID:   nodeID1,
			expectedDiff: func(actualDiff *diff) *diff {
				return &diff{caminoDiff: &caminoDiff{
					deferredStakerDiffs: diffStakers{validatorDiffs: map[ids.ID]map[ids.NodeID]*diffValidator{
						subnetID1: {nodeID1: {validator: staker, validatorStatus: added}},
					}},
				}}
			},
			expectedValidator: staker,
		},
		"OK: in parent": {
			diff: func(c *gomock.Controller) *diff {
				parentState := NewMockChain(c)
				parentState.EXPECT().GetDeferredValidator(subnetID1, nodeID1).Return(staker, nil)
				return &diff{
					stateVersions: newMockStateVersions(c, parentStateID, parentState),
					parentID:      parentStateID,
					caminoDiff:    &caminoDiff{},
				}
			},
			subnetID: subnetID1,
			nodeID:   nodeID1,
			expectedDiff: func(actualDiff *diff) *diff {
				return &diff{
					caminoDiff:    &caminoDiff{deferredStakerDiffs: diffStakers{}},
					parentID:      parentStateID,
					stateVersions: actualDiff.stateVersions,
				}
			},
			expectedValidator: staker,
		},
		"Fail: removed": {
			diff: func(c *gomock.Controller) *diff {
				return &diff{
					caminoDiff: &caminoDiff{
						deferredStakerDiffs: diffStakers{validatorDiffs: map[ids.ID]map[ids.NodeID]*diffValidator{
							subnetID1: {nodeID1: {validatorStatus: deleted}},
						}},
					},
				}
			},
			subnetID: subnetID1,
			nodeID:   nodeID1,
			expectedDiff: func(actualDiff *diff) *diff {
				return &diff{
					caminoDiff: &caminoDiff{
						deferredStakerDiffs: diffStakers{validatorDiffs: map[ids.ID]map[ids.NodeID]*diffValidator{
							subnetID1: {nodeID1: {validatorStatus: deleted}},
						}},
					},
				}
			},
			expectedErr: database.ErrNotFound,
		},
		"Fail: parent errored": {
			diff: func(c *gomock.Controller) *diff {
				parentState := NewMockChain(c)
				parentState.EXPECT().GetDeferredValidator(subnetID1, nodeID1).Return(nil, testErr)
				return &diff{
					stateVersions: newMockStateVersions(c, parentStateID, parentState),
					parentID:      parentStateID,
					caminoDiff:    &caminoDiff{},
				}
			},
			subnetID: subnetID1,
			nodeID:   nodeID1,
			expectedDiff: func(actualDiff *diff) *diff {
				return &diff{
					stateVersions: actualDiff.stateVersions,
					parentID:      actualDiff.parentID,
					caminoDiff:    &caminoDiff{},
				}
			},
			expectedErr: testErr,
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()
			actualDiff := tt.diff(ctrl)
			reward, err := actualDiff.GetDeferredValidator(tt.subnetID, tt.nodeID)
			require.ErrorIs(t, err, tt.expectedErr)
			require.Equal(t, tt.expectedValidator, reward)
			require.Equal(t, tt.expectedDiff(actualDiff), actualDiff)
		})
	}
}

func TestDiffGetDeferredStakerIterator(t *testing.T) {
	parentStateID := ids.ID{123}

	tests := map[string]struct {
		diff         func(*gomock.Controller) *diff
		expectedDiff func(actualDiff *diff) *diff
		expectedErr  error
	}{
		"OK": {
			diff: func(c *gomock.Controller) *diff {
				parentIterator := NewMockStakerIterator(c)
				parentIterator.EXPECT().Next().Return(false)
				parentIterator.EXPECT().Release()

				parentState := NewMockChain(c)
				parentState.EXPECT().GetDeferredStakerIterator().Return(parentIterator, nil)

				return &diff{
					stateVersions: newMockStateVersions(c, parentStateID, parentState),
					parentID:      parentStateID,
					caminoDiff:    &caminoDiff{},
				}
			},
			expectedDiff: func(actualDiff *diff) *diff {
				return &diff{
					caminoDiff:    &caminoDiff{},
					parentID:      parentStateID,
					stateVersions: actualDiff.stateVersions,
				}
			},
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()
			actualDiff := tt.diff(ctrl)
			_, err := actualDiff.GetDeferredStakerIterator()
			require.ErrorIs(t, err, tt.expectedErr)
			// require.Equal(t, tt.expectedStakerIterator, stakerIterator)
			require.Equal(t, tt.expectedDiff(actualDiff), actualDiff)
		})
	}
}

func TestDiffGetProposal(t *testing.T) {
	parentStateID := ids.ID{1}
	proposalID := ids.ID{1, 1}
	proposal := &dac.BaseFeeProposalState{}
	testErr := errors.New("test err")

	tests := map[string]struct {
		diff             func(*gomock.Controller) *diff
		proposalID       ids.ID
		expectedDiff     func(*diff) *diff
		expectedProposal dac.ProposalState
		expectedErr      error
	}{
		"Fail: proposal removed": {
			diff: func(c *gomock.Controller) *diff {
				return &diff{
					stateVersions: NewMockVersions(c),
					caminoDiff: &caminoDiff{
						modifiedProposals: map[ids.ID]*proposalDiff{
							proposalID: {Proposal: proposal, removed: true},
						},
					},
				}
			},
			proposalID: proposalID,
			expectedDiff: func(actualDiff *diff) *diff {
				return &diff{
					stateVersions: actualDiff.stateVersions,
					caminoDiff: &caminoDiff{
						modifiedProposals: map[ids.ID]*proposalDiff{
							proposalID: {Proposal: proposal, removed: true},
						},
					},
				}
			},
			expectedErr: database.ErrNotFound,
		},
		"OK: proposal modified": {
			diff: func(c *gomock.Controller) *diff {
				return &diff{
					stateVersions: NewMockVersions(c),
					caminoDiff: &caminoDiff{
						modifiedProposals: map[ids.ID]*proposalDiff{
							proposalID: {Proposal: proposal},
						},
					},
				}
			},
			proposalID: proposalID,
			expectedDiff: func(actualDiff *diff) *diff {
				return &diff{
					stateVersions: actualDiff.stateVersions,
					caminoDiff: &caminoDiff{
						modifiedProposals: map[ids.ID]*proposalDiff{
							proposalID: {Proposal: proposal},
						},
					},
				}
			},
			expectedProposal: proposal,
		},
		"OK: proposal added": {
			diff: func(c *gomock.Controller) *diff {
				return &diff{
					stateVersions: NewMockVersions(c),
					caminoDiff: &caminoDiff{
						modifiedProposals: map[ids.ID]*proposalDiff{
							proposalID: {Proposal: proposal, added: true},
						},
					},
				}
			},
			proposalID: proposalID,
			expectedDiff: func(actualDiff *diff) *diff {
				return &diff{
					stateVersions: actualDiff.stateVersions,
					caminoDiff: &caminoDiff{
						modifiedProposals: map[ids.ID]*proposalDiff{
							proposalID: {Proposal: proposal, added: true},
						},
					},
				}
			},
			expectedProposal: proposal,
		},
		"OK: proposal in parent state": {
			diff: func(c *gomock.Controller) *diff {
				parentState := NewMockChain(c)
				parentState.EXPECT().GetProposal(proposalID).Return(proposal, nil)
				return &diff{
					stateVersions: newMockStateVersions(c, parentStateID, parentState),
					parentID:      parentStateID,
					caminoDiff:    &caminoDiff{},
				}
			},
			proposalID: proposalID,
			expectedDiff: func(actualDiff *diff) *diff {
				return &diff{
					stateVersions: actualDiff.stateVersions,
					parentID:      actualDiff.parentID,
					caminoDiff:    &caminoDiff{},
				}
			},
			expectedProposal: proposal,
		},
		"Fail: parent errored": {
			diff: func(c *gomock.Controller) *diff {
				parentState := NewMockChain(c)
				parentState.EXPECT().GetProposal(proposalID).Return(nil, testErr)
				return &diff{
					stateVersions: newMockStateVersions(c, parentStateID, parentState),
					parentID:      parentStateID,
					caminoDiff:    &caminoDiff{},
				}
			},
			proposalID: proposalID,
			expectedDiff: func(actualDiff *diff) *diff {
				return &diff{
					stateVersions: actualDiff.stateVersions,
					parentID:      actualDiff.parentID,
					caminoDiff:    &caminoDiff{},
				}
			},
			expectedErr: testErr,
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()
			actualDiff := tt.diff(ctrl)
			actualProposal, err := actualDiff.GetProposal(proposalID)
			require.ErrorIs(t, err, tt.expectedErr)
			require.Equal(t, tt.expectedProposal, actualProposal)
			require.Equal(t, tt.expectedDiff(actualDiff), actualDiff)
		})
	}
}

func TestDiffAddProposal(t *testing.T) {
	proposalID := ids.ID{1}
	proposal := &dac.BaseFeeProposalState{}

	tests := map[string]struct {
		diff         *diff
		proposalID   ids.ID
		proposal     dac.ProposalState
		expectedDiff *diff
	}{
		"OK": {
			diff: &diff{caminoDiff: &caminoDiff{
				modifiedProposals: map[ids.ID]*proposalDiff{},
			}},
			proposalID: proposalID,
			proposal:   proposal,
			expectedDiff: &diff{caminoDiff: &caminoDiff{
				modifiedProposals: map[ids.ID]*proposalDiff{
					proposalID: {Proposal: proposal, added: true},
				},
			}},
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			tt.diff.AddProposal(tt.proposalID, tt.proposal)
			require.Equal(t, tt.expectedDiff, tt.diff)
		})
	}
}

func TestDiffModifyProposal(t *testing.T) {
	proposalID := ids.ID{1}
	proposal := &dac.BaseFeeProposalState{}

	tests := map[string]struct {
		diff         *diff
		proposalID   ids.ID
		proposal     dac.ProposalState
		expectedDiff *diff
	}{
		"OK": {
			diff: &diff{caminoDiff: &caminoDiff{
				modifiedProposals: map[ids.ID]*proposalDiff{},
			}},
			proposalID: proposalID,
			proposal:   proposal,
			expectedDiff: &diff{caminoDiff: &caminoDiff{
				modifiedProposals: map[ids.ID]*proposalDiff{
					proposalID: {Proposal: proposal},
				},
			}},
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			tt.diff.ModifyProposal(tt.proposalID, tt.proposal)
			require.Equal(t, tt.expectedDiff, tt.diff)
		})
	}
}

func TestDiffRemoveProposal(t *testing.T) {
	proposalID := ids.ID{1}
	proposal := &dac.BaseFeeProposalState{}

	tests := map[string]struct {
		diff         *diff
		proposalID   ids.ID
		proposal     dac.ProposalState
		expectedDiff *diff
	}{
		"OK": {
			diff: &diff{caminoDiff: &caminoDiff{
				modifiedProposals: map[ids.ID]*proposalDiff{},
			}},
			proposalID: proposalID,
			proposal:   proposal,
			expectedDiff: &diff{caminoDiff: &caminoDiff{
				modifiedProposals: map[ids.ID]*proposalDiff{
					proposalID: {Proposal: proposal, removed: true},
				},
			}},
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			tt.diff.RemoveProposal(tt.proposalID, tt.proposal)
			require.Equal(t, tt.expectedDiff, tt.diff)
		})
	}
}

func TestDiffAddProposalIDToFinish(t *testing.T) {
	proposalID1 := ids.ID{1}
	proposalID2 := ids.ID{2}
	proposalID3 := ids.ID{3}

	tests := map[string]struct {
		diff         *diff
		proposalID   ids.ID
		expectedDiff *diff
	}{
		"OK": {
			proposalID: proposalID3,
			diff: &diff{caminoDiff: &caminoDiff{
				modifiedProposalIDsToFinish: map[ids.ID]bool{
					proposalID1: true,
					proposalID2: true,
				},
			}},
			expectedDiff: &diff{caminoDiff: &caminoDiff{
				modifiedProposalIDsToFinish: map[ids.ID]bool{
					proposalID1: true,
					proposalID2: true,
					proposalID3: true,
				},
			}},
		},
		"OK: already exist": {
			proposalID: proposalID2,
			diff: &diff{caminoDiff: &caminoDiff{
				modifiedProposalIDsToFinish: map[ids.ID]bool{
					proposalID1: true,
					proposalID2: true,
				},
			}},
			expectedDiff: &diff{caminoDiff: &caminoDiff{
				modifiedProposalIDsToFinish: map[ids.ID]bool{
					proposalID1: true,
					proposalID2: true,
				},
			}},
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			tt.diff.AddProposalIDToFinish(tt.proposalID)
			require.Equal(t, tt.expectedDiff, tt.diff)
		})
	}
}

func TestDiffRemoveProposalIDToFinish(t *testing.T) {
	proposalID1 := ids.ID{1}
	proposalID2 := ids.ID{2}
	proposalID3 := ids.ID{3}

	tests := map[string]struct {
		diff         *diff
		proposalID   ids.ID
		expectedDiff *diff
	}{
		"OK": {
			proposalID: proposalID3,
			diff: &diff{caminoDiff: &caminoDiff{
				modifiedProposalIDsToFinish: map[ids.ID]bool{
					proposalID1: false,
					proposalID2: false,
				},
			}},
			expectedDiff: &diff{caminoDiff: &caminoDiff{
				modifiedProposalIDsToFinish: map[ids.ID]bool{
					proposalID1: false,
					proposalID2: false,
					proposalID3: false,
				},
			}},
		},
		"OK: not exist": {
			proposalID: proposalID2,
			diff: &diff{caminoDiff: &caminoDiff{
				modifiedProposalIDsToFinish: map[ids.ID]bool{
					proposalID1: false,
					proposalID2: false,
				},
			}},
			expectedDiff: &diff{caminoDiff: &caminoDiff{
				modifiedProposalIDsToFinish: map[ids.ID]bool{
					proposalID1: false,
					proposalID2: false,
				},
			}},
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			tt.diff.RemoveProposalIDToFinish(tt.proposalID)
			require.Equal(t, tt.expectedDiff, tt.diff)
		})
	}
}

func TestDiffGetProposalIDsToFinish(t *testing.T) {
	parentStateID := ids.ID{1, 1}
	proposalID1 := ids.ID{1}
	proposalID2 := ids.ID{2}
	proposalID3 := ids.ID{3}
	proposalID4 := ids.ID{4}
	proposalID5 := ids.ID{5}
	proposalID6 := ids.ID{6}
	testErr := errors.New("test err")

	tests := map[string]struct {
		diff                        func(*gomock.Controller) *diff
		expectedDiff                func(actualDiff *diff) *diff
		expectedProposalIDsToFinish []ids.ID
		expectedErr                 error
	}{
		"Parent errored": {
			diff: func(c *gomock.Controller) *diff {
				parentState := NewMockChain(c)
				parentState.EXPECT().GetProposalIDsToFinish().Return(nil, testErr)
				return &diff{
					stateVersions: newMockStateVersions(c, parentStateID, parentState),
					parentID:      parentStateID,
					caminoDiff:    &caminoDiff{},
				}
			},
			expectedDiff: func(actualDiff *diff) *diff {
				return &diff{
					stateVersions: actualDiff.stateVersions,
					parentID:      actualDiff.parentID,
					caminoDiff:    &caminoDiff{},
				}
			},
			expectedErr: testErr,
		},
		"OK: no proposals to finish": {
			diff: func(c *gomock.Controller) *diff {
				parentState := NewMockChain(c)
				parentState.EXPECT().GetProposalIDsToFinish().Return(nil, nil)
				return &diff{
					stateVersions: newMockStateVersions(c, parentStateID, parentState),
					parentID:      parentStateID,
					caminoDiff:    &caminoDiff{},
				}
			},
			expectedDiff: func(actualDiff *diff) *diff {
				return &diff{
					stateVersions: actualDiff.stateVersions,
					parentID:      actualDiff.parentID,
					caminoDiff:    &caminoDiff{},
				}
			},
			expectedProposalIDsToFinish: nil,
		},
		"OK: only parent proposals to finish": {
			diff: func(c *gomock.Controller) *diff {
				parentState := NewMockChain(c)
				parentState.EXPECT().GetProposalIDsToFinish().Return([]ids.ID{proposalID1, proposalID2}, nil)
				return &diff{
					stateVersions: newMockStateVersions(c, parentStateID, parentState),
					parentID:      parentStateID,
					caminoDiff:    &caminoDiff{},
				}
			},
			expectedDiff: func(actualDiff *diff) *diff {
				return &diff{
					stateVersions: actualDiff.stateVersions,
					parentID:      actualDiff.parentID,
					caminoDiff:    &caminoDiff{},
				}
			},
			expectedProposalIDsToFinish: []ids.ID{proposalID1, proposalID2},
		},
		"OK: only diff proposals to finish": {
			diff: func(c *gomock.Controller) *diff {
				parentState := NewMockChain(c)
				parentState.EXPECT().GetProposalIDsToFinish().Return(nil, nil)
				return &diff{
					stateVersions: newMockStateVersions(c, parentStateID, parentState),
					parentID:      parentStateID,
					caminoDiff: &caminoDiff{
						modifiedProposalIDsToFinish: map[ids.ID]bool{
							proposalID1: true,
							proposalID2: false,
							proposalID3: true,
						},
					},
				}
			},
			expectedDiff: func(actualDiff *diff) *diff {
				return &diff{
					stateVersions: actualDiff.stateVersions,
					parentID:      actualDiff.parentID,
					caminoDiff: &caminoDiff{
						modifiedProposalIDsToFinish: map[ids.ID]bool{
							proposalID1: true,
							proposalID2: false,
							proposalID3: true,
						},
					},
				}
			},
			expectedProposalIDsToFinish: []ids.ID{proposalID1, proposalID3},
		},
		"OK": {
			diff: func(c *gomock.Controller) *diff {
				parentState := NewMockChain(c)
				parentState.EXPECT().GetProposalIDsToFinish().Return([]ids.ID{proposalID1, proposalID2, proposalID3}, nil)
				return &diff{
					stateVersions: newMockStateVersions(c, parentStateID, parentState),
					parentID:      parentStateID,
					caminoDiff: &caminoDiff{
						modifiedProposalIDsToFinish: map[ids.ID]bool{
							proposalID2: false,
							proposalID4: true,
							proposalID5: false,
							proposalID6: true,
						},
					},
				}
			},
			expectedDiff: func(actualDiff *diff) *diff {
				return &diff{
					stateVersions: actualDiff.stateVersions,
					parentID:      actualDiff.parentID,
					caminoDiff: &caminoDiff{
						modifiedProposalIDsToFinish: map[ids.ID]bool{
							proposalID2: false,
							proposalID4: true,
							proposalID5: false,
							proposalID6: true,
						},
					},
				}
			},
			expectedProposalIDsToFinish: []ids.ID{proposalID1, proposalID3, proposalID4, proposalID6},
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()
			diff := tt.diff(ctrl)
			proposalIDsToFinish, err := diff.GetProposalIDsToFinish()
			require.ErrorIs(t, err, tt.expectedErr)
			require.Equal(t, tt.expectedProposalIDsToFinish, proposalIDsToFinish)
			require.Equal(t, tt.expectedDiff(diff), diff)
		})
	}
}

func TestDiffGetNextProposalExpirationTime(t *testing.T) {
	parentStateID := ids.ID{1}
	earlyProposalTxID1 := ids.ID{1, 1}
	earlyProposalTxID2 := ids.ID{1, 2}
	midProposalTxID := ids.ID{1, 11}
	lateProposalTxID1 := ids.ID{1, 101}
	lateProposalTxID2 := ids.ID{1, 102}
	earlyProposal := &dac.BaseFeeProposalState{End: 101}
	midProposal := &dac.BaseFeeProposalState{End: 102}
	lateProposal := &dac.BaseFeeProposalState{End: 103}
	testErr := errors.New("test err")

	tests := map[string]struct {
		diff                       func(*gomock.Controller, set.Set[ids.ID]) *diff
		removedProposalIDs         set.Set[ids.ID]
		expectedNextExpirationTime time.Time
		expectedDiff               func(*diff) *diff
		expectedErr                error
	}{
		"Fail: parent errored": {
			diff: func(c *gomock.Controller, removedProposalIDs set.Set[ids.ID]) *diff {
				parentState := NewMockChain(c)
				parentState.EXPECT().GetNextProposalExpirationTime(nil).Return(time.Time{}, testErr)
				return &diff{
					stateVersions: newMockStateVersions(c, parentStateID, parentState),
					parentID:      parentStateID,
					caminoDiff:    &caminoDiff{},
				}
			},
			expectedDiff: func(actualDiff *diff) *diff {
				return &diff{
					stateVersions: actualDiff.stateVersions,
					parentID:      actualDiff.parentID,
					caminoDiff:    &caminoDiff{},
				}
			},
			expectedNextExpirationTime: time.Time{},
			expectedErr:                testErr,
		},
		"Fail: no proposals": {
			diff: func(c *gomock.Controller, removedProposalIDs set.Set[ids.ID]) *diff {
				parentState := NewMockChain(c)
				parentState.EXPECT().GetNextProposalExpirationTime(nil).Return(mockable.MaxTime, database.ErrNotFound)
				return &diff{
					stateVersions: newMockStateVersions(c, parentStateID, parentState),
					parentID:      parentStateID,
					caminoDiff:    &caminoDiff{},
				}
			},
			expectedDiff: func(actualDiff *diff) *diff {
				return &diff{
					stateVersions: actualDiff.stateVersions,
					parentID:      actualDiff.parentID,
					caminoDiff:    &caminoDiff{},
				}
			},
			expectedNextExpirationTime: mockable.MaxTime,
			expectedErr:                database.ErrNotFound,
		},
		"Fail: proposals in parent state only, but all removed": {
			diff: func(c *gomock.Controller, removedProposalIDs set.Set[ids.ID]) *diff {
				removedProposalIDs.Add(earlyProposalTxID1, earlyProposalTxID2)
				parentState := NewMockChain(c)
				parentState.EXPECT().GetNextProposalExpirationTime(removedProposalIDs).
					Return(mockable.MaxTime, database.ErrNotFound)
				return &diff{
					stateVersions: newMockStateVersions(c, parentStateID, parentState),
					parentID:      parentStateID,
					caminoDiff: &caminoDiff{
						modifiedProposals: map[ids.ID]*proposalDiff{
							earlyProposalTxID1: {Proposal: earlyProposal, removed: true},
							earlyProposalTxID2: {Proposal: earlyProposal, removed: true},
						},
					},
				}
			},
			expectedDiff: func(actualDiff *diff) *diff {
				return &diff{
					stateVersions: actualDiff.stateVersions,
					parentID:      actualDiff.parentID,
					caminoDiff: &caminoDiff{
						modifiedProposals: map[ids.ID]*proposalDiff{
							earlyProposalTxID1: {Proposal: earlyProposal, removed: true},
							earlyProposalTxID2: {Proposal: earlyProposal, removed: true},
						},
					},
				}
			},
			expectedNextExpirationTime: mockable.MaxTime,
			expectedErr:                database.ErrNotFound,
		},
		"OK: proposals in parent state only, but one removed": {
			diff: func(c *gomock.Controller, removedProposalIDs set.Set[ids.ID]) *diff {
				removedProposalIDs.Add(earlyProposalTxID1)
				parentState := NewMockChain(c)
				parentState.EXPECT().GetNextProposalExpirationTime(removedProposalIDs).
					Return(earlyProposal.EndTime(), nil)
				return &diff{
					stateVersions: newMockStateVersions(c, parentStateID, parentState),
					parentID:      parentStateID,
					caminoDiff: &caminoDiff{
						modifiedProposals: map[ids.ID]*proposalDiff{
							earlyProposalTxID1: {Proposal: earlyProposal, removed: true},
						},
					},
				}
			},
			expectedDiff: func(actualDiff *diff) *diff {
				return &diff{
					stateVersions: actualDiff.stateVersions,
					parentID:      actualDiff.parentID,
					caminoDiff: &caminoDiff{
						modifiedProposals: map[ids.ID]*proposalDiff{
							earlyProposalTxID1: {Proposal: earlyProposal, removed: true},
						},
					},
				}
			},
			expectedNextExpirationTime: earlyProposal.EndTime(),
		},
		"OK: proposals in added (late) and parent state (early), but all parent removed": {
			diff: func(c *gomock.Controller, removedProposalIDs set.Set[ids.ID]) *diff {
				removedProposalIDs.Add(earlyProposalTxID1)
				parentState := NewMockChain(c)
				parentState.EXPECT().GetNextProposalExpirationTime(removedProposalIDs).
					Return(mockable.MaxTime, nil)
				return &diff{
					stateVersions: newMockStateVersions(c, parentStateID, parentState),
					parentID:      parentStateID,
					caminoDiff: &caminoDiff{
						modifiedProposals: map[ids.ID]*proposalDiff{
							lateProposalTxID1:  {Proposal: lateProposal, added: true},
							earlyProposalTxID1: {Proposal: earlyProposal, removed: true},
						},
					},
				}
			},
			expectedDiff: func(actualDiff *diff) *diff {
				return &diff{
					stateVersions: actualDiff.stateVersions,
					parentID:      actualDiff.parentID,
					caminoDiff: &caminoDiff{
						modifiedProposals: map[ids.ID]*proposalDiff{
							lateProposalTxID1:  {Proposal: lateProposal, added: true},
							earlyProposalTxID1: {Proposal: earlyProposal, removed: true},
						},
					},
				}
			},
			expectedNextExpirationTime: lateProposal.EndTime(),
		},
		"OK: proposals in added (late) and parent state (early), but one parent removed": {
			diff: func(c *gomock.Controller, removedProposalIDs set.Set[ids.ID]) *diff {
				removedProposalIDs.Add(earlyProposalTxID1)
				parentState := NewMockChain(c)
				parentState.EXPECT().GetNextProposalExpirationTime(removedProposalIDs).
					Return(earlyProposal.EndTime(), nil)
				return &diff{
					stateVersions: newMockStateVersions(c, parentStateID, parentState),
					parentID:      parentStateID,
					caminoDiff: &caminoDiff{
						modifiedProposals: map[ids.ID]*proposalDiff{
							lateProposalTxID1:  {Proposal: lateProposal, added: true},
							earlyProposalTxID1: {Proposal: earlyProposal, removed: true},
						},
					},
				}
			},
			expectedDiff: func(actualDiff *diff) *diff {
				return &diff{
					stateVersions: actualDiff.stateVersions,
					parentID:      actualDiff.parentID,
					caminoDiff: &caminoDiff{
						modifiedProposals: map[ids.ID]*proposalDiff{
							lateProposalTxID1:  {Proposal: lateProposal, added: true},
							earlyProposalTxID1: {Proposal: earlyProposal, removed: true},
						},
					},
				}
			},
			expectedNextExpirationTime: earlyProposal.EndTime(),
		},
		"OK: proposals in added (early) and parent state (late), but all parent removed": {
			diff: func(c *gomock.Controller, removedProposalIDs set.Set[ids.ID]) *diff {
				removedProposalIDs.Add(lateProposalTxID1)
				parentState := NewMockChain(c)
				parentState.EXPECT().GetNextProposalExpirationTime(removedProposalIDs).
					Return(mockable.MaxTime, nil)
				return &diff{
					stateVersions: newMockStateVersions(c, parentStateID, parentState),
					parentID:      parentStateID,
					caminoDiff: &caminoDiff{
						modifiedProposals: map[ids.ID]*proposalDiff{
							earlyProposalTxID1: {Proposal: earlyProposal, added: true},
							lateProposalTxID1:  {Proposal: lateProposal, removed: true},
						},
					},
				}
			},
			expectedDiff: func(actualDiff *diff) *diff {
				return &diff{
					stateVersions: actualDiff.stateVersions,
					parentID:      actualDiff.parentID,
					caminoDiff: &caminoDiff{
						modifiedProposals: map[ids.ID]*proposalDiff{
							earlyProposalTxID1: {Proposal: earlyProposal, added: true},
							lateProposalTxID1:  {Proposal: lateProposal, removed: true},
						},
					},
				}
			},
			expectedNextExpirationTime: earlyProposal.EndTime(),
		},
		"OK: proposals in added (early) and parent state (late), but one removed": {
			diff: func(c *gomock.Controller, removedProposalIDs set.Set[ids.ID]) *diff {
				removedProposalIDs.Add(lateProposalTxID1)
				parentState := NewMockChain(c)
				parentState.EXPECT().GetNextProposalExpirationTime(removedProposalIDs).
					Return(lateProposal.EndTime(), nil)
				return &diff{
					stateVersions: newMockStateVersions(c, parentStateID, parentState),
					parentID:      parentStateID,
					caminoDiff: &caminoDiff{
						modifiedProposals: map[ids.ID]*proposalDiff{
							earlyProposalTxID1: {Proposal: earlyProposal, added: true},
							lateProposalTxID1:  {Proposal: lateProposal, removed: true},
						},
					},
				}
			},
			expectedDiff: func(actualDiff *diff) *diff {
				return &diff{
					stateVersions: actualDiff.stateVersions,
					parentID:      actualDiff.parentID,
					caminoDiff: &caminoDiff{
						modifiedProposals: map[ids.ID]*proposalDiff{
							earlyProposalTxID1: {Proposal: earlyProposal, added: true},
							lateProposalTxID1:  {Proposal: lateProposal, removed: true},
						},
					},
				}
			},
			expectedNextExpirationTime: earlyProposal.EndTime(),
		},
		"OK: proposals in added only": {
			diff: func(c *gomock.Controller, removedProposalIDs set.Set[ids.ID]) *diff {
				parentState := NewMockChain(c)
				parentState.EXPECT().GetNextProposalExpirationTime(removedProposalIDs).
					Return(mockable.MaxTime, database.ErrNotFound)
				return &diff{
					stateVersions: newMockStateVersions(c, parentStateID, parentState),
					parentID:      parentStateID,
					caminoDiff: &caminoDiff{
						modifiedProposals: map[ids.ID]*proposalDiff{
							earlyProposalTxID1: {Proposal: earlyProposal, added: true},
							lateProposalTxID1:  {Proposal: lateProposal, added: true},
						},
					},
				}
			},
			expectedDiff: func(actualDiff *diff) *diff {
				return &diff{
					stateVersions: actualDiff.stateVersions,
					parentID:      actualDiff.parentID,
					caminoDiff: &caminoDiff{
						modifiedProposals: map[ids.ID]*proposalDiff{
							earlyProposalTxID1: {Proposal: earlyProposal, added: true},
							lateProposalTxID1:  {Proposal: lateProposal, added: true},
						},
					},
				}
			},
			expectedNextExpirationTime: earlyProposal.EndTime(),
		},
		"OK: proposals in parent state only": {
			diff: func(c *gomock.Controller, removedProposalIDs set.Set[ids.ID]) *diff {
				parentState := NewMockChain(c)
				parentState.EXPECT().GetNextProposalExpirationTime(removedProposalIDs).
					Return(earlyProposal.EndTime(), nil)
				return &diff{
					stateVersions: newMockStateVersions(c, parentStateID, parentState),
					parentID:      parentStateID,
					caminoDiff:    &caminoDiff{},
				}
			},
			expectedDiff: func(actualDiff *diff) *diff {
				return &diff{
					stateVersions: actualDiff.stateVersions,
					parentID:      actualDiff.parentID,
					caminoDiff:    &caminoDiff{},
				}
			},
			expectedNextExpirationTime: earlyProposal.EndTime(),
		},
		"OK: proposals in added (late) and parent state (early)": {
			diff: func(c *gomock.Controller, removedProposalIDs set.Set[ids.ID]) *diff {
				parentState := NewMockChain(c)
				parentState.EXPECT().GetNextProposalExpirationTime(removedProposalIDs).
					Return(earlyProposal.EndTime(), nil)
				return &diff{
					stateVersions: newMockStateVersions(c, parentStateID, parentState),
					parentID:      parentStateID,
					caminoDiff: &caminoDiff{
						modifiedProposals: map[ids.ID]*proposalDiff{
							lateProposalTxID1: {Proposal: lateProposal, added: true},
						},
					},
				}
			},
			expectedDiff: func(actualDiff *diff) *diff {
				return &diff{
					stateVersions: actualDiff.stateVersions,
					parentID:      actualDiff.parentID,
					caminoDiff: &caminoDiff{
						modifiedProposals: map[ids.ID]*proposalDiff{
							lateProposalTxID1: {Proposal: lateProposal, added: true},
						},
					},
				}
			},
			expectedNextExpirationTime: earlyProposal.EndTime(),
		},
		"OK: proposals in added (early, mid) and parent state (late)": {
			diff: func(c *gomock.Controller, removedProposalIDs set.Set[ids.ID]) *diff {
				parentState := NewMockChain(c)
				parentState.EXPECT().GetNextProposalExpirationTime(removedProposalIDs).
					Return(lateProposal.EndTime(), nil)
				return &diff{
					stateVersions: newMockStateVersions(c, parentStateID, parentState),
					parentID:      parentStateID,
					caminoDiff: &caminoDiff{
						modifiedProposals: map[ids.ID]*proposalDiff{
							earlyProposalTxID1: {Proposal: earlyProposal, added: true},
							midProposalTxID:    {Proposal: midProposal, added: true},
						},
					},
				}
			},
			expectedDiff: func(actualDiff *diff) *diff {
				return &diff{
					stateVersions: actualDiff.stateVersions,
					parentID:      actualDiff.parentID,
					caminoDiff: &caminoDiff{
						modifiedProposals: map[ids.ID]*proposalDiff{
							earlyProposalTxID1: {Proposal: earlyProposal, added: true},
							midProposalTxID:    {Proposal: midProposal, added: true},
						},
					},
				}
			},
			expectedNextExpirationTime: earlyProposal.EndTime(),
		},
		"Fail: proposals in parent state only, but all removed in arg": {
			diff: func(c *gomock.Controller, removedProposalIDs set.Set[ids.ID]) *diff {
				parentState := NewMockChain(c)
				parentState.EXPECT().GetNextProposalExpirationTime(removedProposalIDs).
					Return(mockable.MaxTime, database.ErrNotFound)
				return &diff{
					stateVersions: newMockStateVersions(c, parentStateID, parentState),
					parentID:      parentStateID,
					caminoDiff:    &caminoDiff{},
				}
			},
			removedProposalIDs: set.Set[ids.ID]{
				earlyProposalTxID1: struct{}{},
			},
			expectedDiff: func(actualDiff *diff) *diff {
				return &diff{
					stateVersions: actualDiff.stateVersions,
					parentID:      actualDiff.parentID,
					caminoDiff:    &caminoDiff{},
				}
			},
			expectedNextExpirationTime: mockable.MaxTime,
			expectedErr:                database.ErrNotFound,
		},
		"Fail: proposals in parent state only, but all removed (one in arg, one in diff)": {
			diff: func(c *gomock.Controller, removedProposalIDs set.Set[ids.ID]) *diff {
				removedProposalIDs.Add(earlyProposalTxID1)
				parentState := NewMockChain(c)
				parentState.EXPECT().GetNextProposalExpirationTime(removedProposalIDs).
					Return(mockable.MaxTime, database.ErrNotFound)
				return &diff{
					stateVersions: newMockStateVersions(c, parentStateID, parentState),
					parentID:      parentStateID,
					caminoDiff: &caminoDiff{
						modifiedProposals: map[ids.ID]*proposalDiff{
							earlyProposalTxID1: {Proposal: earlyProposal, removed: true},
						},
					},
				}
			},
			removedProposalIDs: set.Set[ids.ID]{
				earlyProposalTxID2: struct{}{},
			},
			expectedDiff: func(actualDiff *diff) *diff {
				return &diff{
					stateVersions: actualDiff.stateVersions,
					parentID:      actualDiff.parentID,
					caminoDiff: &caminoDiff{
						modifiedProposals: map[ids.ID]*proposalDiff{
							earlyProposalTxID1: {Proposal: earlyProposal, removed: true},
						},
					},
				}
			},
			expectedNextExpirationTime: mockable.MaxTime,
			expectedErr:                database.ErrNotFound,
		},
		"OK: proposals in added (late) and parent state (early), but all parent removed (one in arg, one in diff)": {
			diff: func(c *gomock.Controller, removedProposalIDs set.Set[ids.ID]) *diff {
				removedProposalIDs.Add(earlyProposalTxID1)
				parentState := NewMockChain(c)
				parentState.EXPECT().GetNextProposalExpirationTime(removedProposalIDs).
					Return(mockable.MaxTime, nil)
				return &diff{
					stateVersions: newMockStateVersions(c, parentStateID, parentState),
					parentID:      parentStateID,
					caminoDiff: &caminoDiff{
						modifiedProposals: map[ids.ID]*proposalDiff{
							lateProposalTxID1:  {Proposal: lateProposal, added: true},
							earlyProposalTxID1: {Proposal: earlyProposal, removed: true},
						},
					},
				}
			},
			removedProposalIDs: set.Set[ids.ID]{
				earlyProposalTxID2: struct{}{},
			},
			expectedDiff: func(actualDiff *diff) *diff {
				return &diff{
					stateVersions: actualDiff.stateVersions,
					parentID:      actualDiff.parentID,
					caminoDiff: &caminoDiff{
						modifiedProposals: map[ids.ID]*proposalDiff{
							lateProposalTxID1:  {Proposal: lateProposal, added: true},
							earlyProposalTxID1: {Proposal: earlyProposal, removed: true},
						},
					},
				}
			},
			expectedNextExpirationTime: lateProposal.EndTime(),
		},
		"OK: proposals in added (late) and parent state (early), but all parent removed in arg": {
			diff: func(c *gomock.Controller, removedProposalIDs set.Set[ids.ID]) *diff {
				parentState := NewMockChain(c)
				parentState.EXPECT().GetNextProposalExpirationTime(removedProposalIDs).
					Return(mockable.MaxTime, nil)
				return &diff{
					stateVersions: newMockStateVersions(c, parentStateID, parentState),
					parentID:      parentStateID,
					caminoDiff: &caminoDiff{
						modifiedProposals: map[ids.ID]*proposalDiff{
							lateProposalTxID1: {Proposal: lateProposal, added: true},
						},
					},
				}
			},
			removedProposalIDs: set.Set[ids.ID]{
				earlyProposalTxID1: struct{}{},
			},
			expectedDiff: func(actualDiff *diff) *diff {
				return &diff{
					stateVersions: actualDiff.stateVersions,
					parentID:      actualDiff.parentID,
					caminoDiff: &caminoDiff{
						modifiedProposals: map[ids.ID]*proposalDiff{
							lateProposalTxID1: {Proposal: lateProposal, added: true},
						},
					},
				}
			},
			expectedNextExpirationTime: lateProposal.EndTime(),
		},
		"OK: proposals in added (late) and parent state (early), but some parent removed (one in arg, one in diff)": {
			diff: func(c *gomock.Controller, removedProposalIDs set.Set[ids.ID]) *diff {
				removedProposalIDs.Add(earlyProposalTxID1)
				parentState := NewMockChain(c)
				parentState.EXPECT().GetNextProposalExpirationTime(removedProposalIDs).
					Return(earlyProposal.EndTime(), nil)
				return &diff{
					stateVersions: newMockStateVersions(c, parentStateID, parentState),
					parentID:      parentStateID,
					caminoDiff: &caminoDiff{
						modifiedProposals: map[ids.ID]*proposalDiff{
							lateProposalTxID1:  {Proposal: lateProposal, added: true},
							earlyProposalTxID1: {Proposal: earlyProposal, removed: true},
						},
					},
				}
			},
			removedProposalIDs: set.Set[ids.ID]{
				earlyProposalTxID2: struct{}{},
			},
			expectedDiff: func(actualDiff *diff) *diff {
				return &diff{
					stateVersions: actualDiff.stateVersions,
					parentID:      actualDiff.parentID,
					caminoDiff: &caminoDiff{
						modifiedProposals: map[ids.ID]*proposalDiff{
							lateProposalTxID1:  {Proposal: lateProposal, added: true},
							earlyProposalTxID1: {Proposal: earlyProposal, removed: true},
						},
					},
				}
			},
			expectedNextExpirationTime: earlyProposal.EndTime(),
		},
		"OK: proposals in added (late) and parent state (early), but some parent removed in arg": {
			diff: func(c *gomock.Controller, removedProposalIDs set.Set[ids.ID]) *diff {
				parentState := NewMockChain(c)
				parentState.EXPECT().GetNextProposalExpirationTime(removedProposalIDs).
					Return(earlyProposal.EndTime(), nil)
				return &diff{
					stateVersions: newMockStateVersions(c, parentStateID, parentState),
					parentID:      parentStateID,
					caminoDiff: &caminoDiff{
						modifiedProposals: map[ids.ID]*proposalDiff{
							lateProposalTxID1: {Proposal: lateProposal, added: true},
						},
					},
				}
			},
			removedProposalIDs: set.Set[ids.ID]{
				earlyProposalTxID1: struct{}{},
			},
			expectedDiff: func(actualDiff *diff) *diff {
				return &diff{
					stateVersions: actualDiff.stateVersions,
					parentID:      actualDiff.parentID,
					caminoDiff: &caminoDiff{
						modifiedProposals: map[ids.ID]*proposalDiff{
							lateProposalTxID1: {Proposal: lateProposal, added: true},
						},
					},
				}
			},
			expectedNextExpirationTime: earlyProposal.EndTime(),
		},
		"OK: proposals in added (early) and parent state (late), but all parent removed (one in arg, one in diff)": {
			diff: func(c *gomock.Controller, removedProposalIDs set.Set[ids.ID]) *diff {
				removedProposalIDs.Add(lateProposalTxID1)
				parentState := NewMockChain(c)
				parentState.EXPECT().GetNextProposalExpirationTime(removedProposalIDs).
					Return(mockable.MaxTime, nil)
				return &diff{
					stateVersions: newMockStateVersions(c, parentStateID, parentState),
					parentID:      parentStateID,
					caminoDiff: &caminoDiff{
						modifiedProposals: map[ids.ID]*proposalDiff{
							earlyProposalTxID1: {Proposal: earlyProposal, added: true},
							lateProposalTxID1:  {Proposal: lateProposal, removed: true},
						},
					},
				}
			},
			removedProposalIDs: set.Set[ids.ID]{
				lateProposalTxID2: struct{}{},
			},
			expectedDiff: func(actualDiff *diff) *diff {
				return &diff{
					stateVersions: actualDiff.stateVersions,
					parentID:      actualDiff.parentID,
					caminoDiff: &caminoDiff{
						modifiedProposals: map[ids.ID]*proposalDiff{
							earlyProposalTxID1: {Proposal: earlyProposal, added: true},
							lateProposalTxID1:  {Proposal: lateProposal, removed: true},
						},
					},
				}
			},
			expectedNextExpirationTime: earlyProposal.EndTime(),
		},
		"OK: proposals in added (early) and parent state (late), but all parent removed in arg": {
			diff: func(c *gomock.Controller, removedProposalIDs set.Set[ids.ID]) *diff {
				parentState := NewMockChain(c)
				parentState.EXPECT().GetNextProposalExpirationTime(removedProposalIDs).
					Return(mockable.MaxTime, nil)
				return &diff{
					stateVersions: newMockStateVersions(c, parentStateID, parentState),
					parentID:      parentStateID,
					caminoDiff: &caminoDiff{
						modifiedProposals: map[ids.ID]*proposalDiff{
							earlyProposalTxID1: {Proposal: earlyProposal, added: true},
						},
					},
				}
			},
			removedProposalIDs: set.Set[ids.ID]{
				lateProposalTxID1: struct{}{},
			},
			expectedDiff: func(actualDiff *diff) *diff {
				return &diff{
					stateVersions: actualDiff.stateVersions,
					parentID:      actualDiff.parentID,
					caminoDiff: &caminoDiff{
						modifiedProposals: map[ids.ID]*proposalDiff{
							earlyProposalTxID1: {Proposal: earlyProposal, added: true},
						},
					},
				}
			},
			expectedNextExpirationTime: earlyProposal.EndTime(),
		},
		"OK: proposals in added (early) and parent state (late), but some removed (one in arg, one in diff)": {
			diff: func(c *gomock.Controller, removedProposalIDs set.Set[ids.ID]) *diff {
				removedProposalIDs.Add(lateProposalTxID1)
				parentState := NewMockChain(c)
				parentState.EXPECT().GetNextProposalExpirationTime(removedProposalIDs).
					Return(lateProposal.EndTime(), nil)
				return &diff{
					stateVersions: newMockStateVersions(c, parentStateID, parentState),
					parentID:      parentStateID,
					caminoDiff: &caminoDiff{
						modifiedProposals: map[ids.ID]*proposalDiff{
							earlyProposalTxID1: {Proposal: earlyProposal, added: true},
							lateProposalTxID1:  {Proposal: lateProposal, removed: true},
						},
					},
				}
			},
			removedProposalIDs: set.Set[ids.ID]{
				lateProposalTxID2: struct{}{},
			},
			expectedDiff: func(actualDiff *diff) *diff {
				return &diff{
					stateVersions: actualDiff.stateVersions,
					parentID:      actualDiff.parentID,
					caminoDiff: &caminoDiff{
						modifiedProposals: map[ids.ID]*proposalDiff{
							earlyProposalTxID1: {Proposal: earlyProposal, added: true},
							lateProposalTxID1:  {Proposal: lateProposal, removed: true},
						},
					},
				}
			},
			expectedNextExpirationTime: earlyProposal.EndTime(),
		},
		"OK: proposals in added (early) and parent state (late), but some removed in arg": {
			diff: func(c *gomock.Controller, removedProposalIDs set.Set[ids.ID]) *diff {
				removedProposalIDs.Add(lateProposalTxID1)
				parentState := NewMockChain(c)
				parentState.EXPECT().GetNextProposalExpirationTime(removedProposalIDs).
					Return(lateProposal.EndTime(), nil)
				return &diff{
					stateVersions: newMockStateVersions(c, parentStateID, parentState),
					parentID:      parentStateID,
					caminoDiff: &caminoDiff{
						modifiedProposals: map[ids.ID]*proposalDiff{
							earlyProposalTxID1: {Proposal: earlyProposal, added: true},
						},
					},
				}
			},
			removedProposalIDs: set.Set[ids.ID]{
				lateProposalTxID1: struct{}{},
			},
			expectedDiff: func(actualDiff *diff) *diff {
				return &diff{
					stateVersions: actualDiff.stateVersions,
					parentID:      actualDiff.parentID,
					caminoDiff: &caminoDiff{
						modifiedProposals: map[ids.ID]*proposalDiff{
							earlyProposalTxID1: {Proposal: earlyProposal, added: true},
						},
					},
				}
			},
			expectedNextExpirationTime: earlyProposal.EndTime(),
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()
			actualDiff := tt.diff(ctrl, tt.removedProposalIDs)
			nextExpirationTime, err := actualDiff.GetNextProposalExpirationTime(tt.removedProposalIDs)
			require.ErrorIs(t, err, tt.expectedErr)
			require.Equal(t, tt.expectedNextExpirationTime, nextExpirationTime)
			require.Equal(t, tt.expectedDiff(actualDiff), actualDiff)
		})
	}
}

func TestDiffGetNextToExpireProposalIDsAndTime(t *testing.T) {
	parentStateID := ids.ID{1}
	earlyProposalTxID1 := ids.ID{1, 1}
	earlyProposalTxID2 := ids.ID{1, 2}
	earlyProposalTxID3 := ids.ID{1, 3}
	midProposalTxID := ids.ID{1, 10}
	lateProposalTxID1 := ids.ID{1, 101}
	lateProposalTxID2 := ids.ID{1, 102}
	lateProposalTxID3 := ids.ID{1, 103}
	earlyProposal := &dac.BaseFeeProposalState{End: 101}
	midProposal := &dac.BaseFeeProposalState{End: 102}
	lateProposal := &dac.BaseFeeProposalState{End: 103}
	testErr := errors.New("test err")

	tests := map[string]struct {
		diff                       func(*gomock.Controller, set.Set[ids.ID]) *diff
		removedProposalIDs         set.Set[ids.ID]
		expectedDiff               func(*diff) *diff
		expectedNextToExpireIDs    []ids.ID
		expectedNextExpirationTime time.Time
		expectedErr                error
	}{
		"Fail: parent errored": {
			diff: func(c *gomock.Controller, removedProposalIDs set.Set[ids.ID]) *diff {
				parentState := NewMockChain(c)
				parentState.EXPECT().GetNextToExpireProposalIDsAndTime(nil).Return(nil, time.Time{}, testErr)
				return &diff{
					stateVersions: newMockStateVersions(c, parentStateID, parentState),
					parentID:      parentStateID,
					caminoDiff:    &caminoDiff{},
				}
			},
			expectedDiff: func(actualDiff *diff) *diff {
				return &diff{
					stateVersions: actualDiff.stateVersions,
					parentID:      actualDiff.parentID,
					caminoDiff:    &caminoDiff{},
				}
			},
			expectedNextExpirationTime: time.Time{},
			expectedErr:                testErr,
		},
		"Fail: no proposals": {
			diff: func(c *gomock.Controller, removedProposalIDs set.Set[ids.ID]) *diff {
				parentState := NewMockChain(c)
				parentState.EXPECT().GetNextToExpireProposalIDsAndTime(nil).
					Return(nil, mockable.MaxTime, database.ErrNotFound)
				return &diff{
					stateVersions: newMockStateVersions(c, parentStateID, parentState),
					parentID:      parentStateID,
					caminoDiff:    &caminoDiff{},
				}
			},
			expectedDiff: func(actualDiff *diff) *diff {
				return &diff{
					stateVersions: actualDiff.stateVersions,
					parentID:      actualDiff.parentID,
					caminoDiff:    &caminoDiff{},
				}
			},
			expectedNextExpirationTime: mockable.MaxTime,
			expectedErr:                database.ErrNotFound,
		},
		"Fail: proposals in parent state only, but all removed": {
			diff: func(c *gomock.Controller, removedProposalIDs set.Set[ids.ID]) *diff {
				removedProposalIDs.Add(earlyProposalTxID1, earlyProposalTxID2)
				parentState := NewMockChain(c)
				parentState.EXPECT().GetNextToExpireProposalIDsAndTime(removedProposalIDs).
					Return(nil, mockable.MaxTime, database.ErrNotFound)
				return &diff{
					stateVersions: newMockStateVersions(c, parentStateID, parentState),
					parentID:      parentStateID,
					caminoDiff: &caminoDiff{
						modifiedProposals: map[ids.ID]*proposalDiff{
							earlyProposalTxID1: {Proposal: earlyProposal, removed: true},
							earlyProposalTxID2: {Proposal: earlyProposal, removed: true},
						},
					},
				}
			},
			expectedDiff: func(actualDiff *diff) *diff {
				return &diff{
					stateVersions: actualDiff.stateVersions,
					parentID:      actualDiff.parentID,
					caminoDiff: &caminoDiff{
						modifiedProposals: map[ids.ID]*proposalDiff{
							earlyProposalTxID1: {Proposal: earlyProposal, removed: true},
							earlyProposalTxID2: {Proposal: earlyProposal, removed: true},
						},
					},
				}
			},
			expectedNextExpirationTime: mockable.MaxTime,
			expectedErr:                database.ErrNotFound,
		},
		"OK: proposals in parent state only, but one removed": {
			diff: func(c *gomock.Controller, removedProposalIDs set.Set[ids.ID]) *diff {
				removedProposalIDs.Add(earlyProposalTxID1)
				parentState := NewMockChain(c)
				parentState.EXPECT().GetNextToExpireProposalIDsAndTime(removedProposalIDs).
					Return([]ids.ID{earlyProposalTxID2}, earlyProposal.EndTime(), nil)
				return &diff{
					stateVersions: newMockStateVersions(c, parentStateID, parentState),
					parentID:      parentStateID,
					caminoDiff: &caminoDiff{
						modifiedProposals: map[ids.ID]*proposalDiff{
							earlyProposalTxID1: {Proposal: earlyProposal, removed: true},
						},
					},
				}
			},
			expectedDiff: func(actualDiff *diff) *diff {
				return &diff{
					stateVersions: actualDiff.stateVersions,
					parentID:      actualDiff.parentID,
					caminoDiff: &caminoDiff{
						modifiedProposals: map[ids.ID]*proposalDiff{
							earlyProposalTxID1: {Proposal: earlyProposal, removed: true},
						},
					},
				}
			},
			expectedNextExpirationTime: earlyProposal.EndTime(),
			expectedNextToExpireIDs:    []ids.ID{earlyProposalTxID2},
		},
		"OK: proposals in added (late) and parent state (early), but all parent removed": {
			diff: func(c *gomock.Controller, removedProposalIDs set.Set[ids.ID]) *diff {
				removedProposalIDs.Add(earlyProposalTxID1, earlyProposalTxID2)
				parentState := NewMockChain(c)
				parentState.EXPECT().GetNextToExpireProposalIDsAndTime(removedProposalIDs).
					Return(nil, mockable.MaxTime, database.ErrNotFound)
				return &diff{
					stateVersions: newMockStateVersions(c, parentStateID, parentState),
					parentID:      parentStateID,
					caminoDiff: &caminoDiff{
						modifiedProposals: map[ids.ID]*proposalDiff{
							lateProposalTxID1:  {Proposal: lateProposal, added: true},
							earlyProposalTxID1: {Proposal: earlyProposal, removed: true},
							earlyProposalTxID2: {Proposal: earlyProposal, removed: true},
						},
					},
				}
			},
			expectedNextToExpireIDs: []ids.ID{lateProposalTxID1},
			expectedDiff: func(actualDiff *diff) *diff {
				return &diff{
					stateVersions: actualDiff.stateVersions,
					parentID:      actualDiff.parentID,
					caminoDiff: &caminoDiff{
						modifiedProposals: map[ids.ID]*proposalDiff{
							lateProposalTxID1:  {Proposal: lateProposal, added: true},
							earlyProposalTxID1: {Proposal: earlyProposal, removed: true},
							earlyProposalTxID2: {Proposal: earlyProposal, removed: true},
						},
					},
				}
			},
			expectedNextExpirationTime: lateProposal.EndTime(),
		},
		"OK: proposals in added (late) and parent state (early), but one parent removed": {
			diff: func(c *gomock.Controller, removedProposalIDs set.Set[ids.ID]) *diff {
				removedProposalIDs.Add(earlyProposalTxID1)
				parentState := NewMockChain(c)
				parentState.EXPECT().GetNextToExpireProposalIDsAndTime(removedProposalIDs).
					Return([]ids.ID{earlyProposalTxID2}, earlyProposal.EndTime(), nil)
				return &diff{
					stateVersions: newMockStateVersions(c, parentStateID, parentState),
					parentID:      parentStateID,
					caminoDiff: &caminoDiff{
						modifiedProposals: map[ids.ID]*proposalDiff{
							lateProposalTxID1:  {Proposal: lateProposal, added: true},
							earlyProposalTxID1: {Proposal: earlyProposal, removed: true},
						},
					},
				}
			},
			expectedNextToExpireIDs: []ids.ID{earlyProposalTxID2},
			expectedDiff: func(actualDiff *diff) *diff {
				return &diff{
					stateVersions: actualDiff.stateVersions,
					parentID:      actualDiff.parentID,
					caminoDiff: &caminoDiff{
						modifiedProposals: map[ids.ID]*proposalDiff{
							lateProposalTxID1:  {Proposal: lateProposal, added: true},
							earlyProposalTxID1: {Proposal: earlyProposal, removed: true},
						},
					},
				}
			},
			expectedNextExpirationTime: earlyProposal.EndTime(),
		},
		"OK: proposals in added (early) and parent state (late), but all parent removed": {
			diff: func(c *gomock.Controller, removedProposalIDs set.Set[ids.ID]) *diff {
				removedProposalIDs.Add(lateProposalTxID1, lateProposalTxID2)
				parentState := NewMockChain(c)
				parentState.EXPECT().GetNextToExpireProposalIDsAndTime(removedProposalIDs).
					Return(nil, mockable.MaxTime, nil)
				return &diff{
					stateVersions: newMockStateVersions(c, parentStateID, parentState),
					parentID:      parentStateID,
					caminoDiff: &caminoDiff{
						modifiedProposals: map[ids.ID]*proposalDiff{
							earlyProposalTxID1: {Proposal: earlyProposal, added: true},
							lateProposalTxID1:  {Proposal: lateProposal, removed: true},
							lateProposalTxID2:  {Proposal: lateProposal, removed: true},
						},
					},
				}
			},
			expectedNextToExpireIDs: []ids.ID{earlyProposalTxID1},
			expectedDiff: func(actualDiff *diff) *diff {
				return &diff{
					stateVersions: actualDiff.stateVersions,
					parentID:      actualDiff.parentID,
					caminoDiff: &caminoDiff{
						modifiedProposals: map[ids.ID]*proposalDiff{
							earlyProposalTxID1: {Proposal: earlyProposal, added: true},
							lateProposalTxID1:  {Proposal: lateProposal, removed: true},
							lateProposalTxID2:  {Proposal: lateProposal, removed: true},
						},
					},
				}
			},
			expectedNextExpirationTime: earlyProposal.EndTime(),
		},
		"OK: proposals in added (early) and parent state (late), but one parent removed": {
			diff: func(c *gomock.Controller, removedProposalIDs set.Set[ids.ID]) *diff {
				removedProposalIDs.Add(lateProposalTxID1)
				parentState := NewMockChain(c)
				parentState.EXPECT().GetNextToExpireProposalIDsAndTime(removedProposalIDs).
					Return([]ids.ID{lateProposalTxID2}, lateProposal.EndTime(), nil)
				return &diff{
					stateVersions: newMockStateVersions(c, parentStateID, parentState),
					parentID:      parentStateID,
					caminoDiff: &caminoDiff{
						modifiedProposals: map[ids.ID]*proposalDiff{
							earlyProposalTxID1: {Proposal: earlyProposal, added: true},
							lateProposalTxID1:  {Proposal: lateProposal, removed: true},
						},
					},
				}
			},
			expectedNextToExpireIDs: []ids.ID{earlyProposalTxID1},
			expectedDiff: func(actualDiff *diff) *diff {
				return &diff{
					stateVersions: actualDiff.stateVersions,
					parentID:      actualDiff.parentID,
					caminoDiff: &caminoDiff{
						modifiedProposals: map[ids.ID]*proposalDiff{
							earlyProposalTxID1: {Proposal: earlyProposal, added: true},
							lateProposalTxID1:  {Proposal: lateProposal, removed: true},
						},
					},
				}
			},
			expectedNextExpirationTime: earlyProposal.EndTime(),
		},
		"OK: proposals in added only (early, late)": {
			diff: func(c *gomock.Controller, removedProposalIDs set.Set[ids.ID]) *diff {
				parentState := NewMockChain(c)
				parentState.EXPECT().GetNextToExpireProposalIDsAndTime(removedProposalIDs).Return(nil, mockable.MaxTime, database.ErrNotFound)
				return &diff{
					stateVersions: newMockStateVersions(c, parentStateID, parentState),
					parentID:      parentStateID,
					caminoDiff: &caminoDiff{
						modifiedProposals: map[ids.ID]*proposalDiff{
							earlyProposalTxID1: {Proposal: earlyProposal, added: true},
							lateProposalTxID1:  {Proposal: lateProposal, added: true},
						},
					},
				}
			},
			expectedNextToExpireIDs: []ids.ID{earlyProposalTxID1},
			expectedDiff: func(actualDiff *diff) *diff {
				return &diff{
					stateVersions: actualDiff.stateVersions,
					parentID:      actualDiff.parentID,
					caminoDiff: &caminoDiff{
						modifiedProposals: map[ids.ID]*proposalDiff{
							earlyProposalTxID1: {Proposal: earlyProposal, added: true},
							lateProposalTxID1:  {Proposal: lateProposal, added: true},
						},
					},
				}
			},
			expectedNextExpirationTime: earlyProposal.EndTime(),
		},
		"OK: proposals in parent state only": {
			diff: func(c *gomock.Controller, removedProposalIDs set.Set[ids.ID]) *diff {
				parentState := NewMockChain(c)
				parentState.EXPECT().GetNextToExpireProposalIDsAndTime(removedProposalIDs).Return(
					[]ids.ID{earlyProposalTxID1, earlyProposalTxID2},
					earlyProposal.EndTime(),
					nil,
				)
				return &diff{
					stateVersions: newMockStateVersions(c, parentStateID, parentState),
					parentID:      parentStateID,
					caminoDiff:    &caminoDiff{},
				}
			},
			expectedNextToExpireIDs: []ids.ID{earlyProposalTxID1, earlyProposalTxID2},
			expectedDiff: func(actualDiff *diff) *diff {
				return &diff{
					stateVersions: actualDiff.stateVersions,
					parentID:      actualDiff.parentID,
					caminoDiff:    &caminoDiff{},
				}
			},
			expectedNextExpirationTime: earlyProposal.EndTime(),
		},
		"OK: proposals in added (late) and parent state (early)": {
			diff: func(c *gomock.Controller, removedProposalIDs set.Set[ids.ID]) *diff {
				parentState := NewMockChain(c)
				parentState.EXPECT().GetNextToExpireProposalIDsAndTime(removedProposalIDs).Return(
					[]ids.ID{earlyProposalTxID1},
					earlyProposal.EndTime(),
					nil,
				)
				return &diff{
					stateVersions: newMockStateVersions(c, parentStateID, parentState),
					parentID:      parentStateID,
					caminoDiff: &caminoDiff{
						modifiedProposals: map[ids.ID]*proposalDiff{
							lateProposalTxID1: {Proposal: lateProposal, added: true},
						},
					},
				}
			},
			expectedNextToExpireIDs: []ids.ID{earlyProposalTxID1},
			expectedDiff: func(actualDiff *diff) *diff {
				return &diff{
					stateVersions: actualDiff.stateVersions,
					parentID:      actualDiff.parentID,
					caminoDiff: &caminoDiff{
						modifiedProposals: map[ids.ID]*proposalDiff{
							lateProposalTxID1: {Proposal: lateProposal, added: true},
						},
					},
				}
			},
			expectedNextExpirationTime: earlyProposal.EndTime(),
		},
		"OK: proposals in added (early, mid) and parent state (late)": {
			diff: func(c *gomock.Controller, removedProposalIDs set.Set[ids.ID]) *diff {
				parentState := NewMockChain(c)
				parentState.EXPECT().GetNextToExpireProposalIDsAndTime(removedProposalIDs).Return(
					[]ids.ID{lateProposalTxID1},
					lateProposal.EndTime(),
					nil,
				)
				return &diff{
					stateVersions: newMockStateVersions(c, parentStateID, parentState),
					parentID:      parentStateID,
					caminoDiff: &caminoDiff{
						modifiedProposals: map[ids.ID]*proposalDiff{
							earlyProposalTxID1: {Proposal: earlyProposal, added: true},
							midProposalTxID:    {Proposal: midProposal, added: true},
						},
					},
				}
			},
			expectedNextToExpireIDs: []ids.ID{earlyProposalTxID1},
			expectedDiff: func(actualDiff *diff) *diff {
				return &diff{
					stateVersions: actualDiff.stateVersions,
					parentID:      actualDiff.parentID,
					caminoDiff: &caminoDiff{
						modifiedProposals: map[ids.ID]*proposalDiff{
							earlyProposalTxID1: {Proposal: earlyProposal, added: true},
							midProposalTxID:    {Proposal: midProposal, added: true},
						},
					},
				}
			},
			expectedNextExpirationTime: earlyProposal.EndTime(),
		},
		"OK: proposals in added (early1, late) and parent state (early2)": {
			diff: func(c *gomock.Controller, removedProposalIDs set.Set[ids.ID]) *diff {
				parentState := NewMockChain(c)
				parentState.EXPECT().GetNextToExpireProposalIDsAndTime(removedProposalIDs).Return(
					[]ids.ID{earlyProposalTxID2},
					earlyProposal.EndTime(),
					nil,
				)
				return &diff{
					stateVersions: newMockStateVersions(c, parentStateID, parentState),
					parentID:      parentStateID,
					caminoDiff: &caminoDiff{
						modifiedProposals: map[ids.ID]*proposalDiff{
							earlyProposalTxID1: {Proposal: earlyProposal, added: true},
							lateProposalTxID1:  {Proposal: lateProposal, added: true},
						},
					},
				}
			},
			expectedNextToExpireIDs: []ids.ID{earlyProposalTxID1, earlyProposalTxID2},
			expectedDiff: func(actualDiff *diff) *diff {
				return &diff{
					stateVersions: actualDiff.stateVersions,
					parentID:      actualDiff.parentID,
					caminoDiff: &caminoDiff{
						modifiedProposals: map[ids.ID]*proposalDiff{
							earlyProposalTxID1: {Proposal: earlyProposal, added: true},
							lateProposalTxID1:  {Proposal: lateProposal, added: true},
						},
					},
				}
			},
			expectedNextExpirationTime: earlyProposal.EndTime(),
		},
		"Fail: proposals in parent state only, but all removed in arg": {
			diff: func(c *gomock.Controller, removedProposalIDs set.Set[ids.ID]) *diff {
				parentState := NewMockChain(c)
				parentState.EXPECT().GetNextToExpireProposalIDsAndTime(removedProposalIDs).
					Return(nil, mockable.MaxTime, database.ErrNotFound)
				return &diff{
					stateVersions: newMockStateVersions(c, parentStateID, parentState),
					parentID:      parentStateID,
					caminoDiff:    &caminoDiff{},
				}
			},
			removedProposalIDs: set.Set[ids.ID]{
				earlyProposalTxID1: struct{}{},
			},
			expectedDiff: func(actualDiff *diff) *diff {
				return &diff{
					stateVersions: actualDiff.stateVersions,
					parentID:      actualDiff.parentID,
					caminoDiff:    &caminoDiff{},
				}
			},
			expectedNextExpirationTime: mockable.MaxTime,
			expectedErr:                database.ErrNotFound,
		},
		"OK: proposals in parent state only, but one removed in arg": {
			diff: func(c *gomock.Controller, removedProposalIDs set.Set[ids.ID]) *diff {
				parentState := NewMockChain(c)
				parentState.EXPECT().GetNextToExpireProposalIDsAndTime(removedProposalIDs).
					Return([]ids.ID{earlyProposalTxID2}, earlyProposal.EndTime(), nil)
				return &diff{
					stateVersions: newMockStateVersions(c, parentStateID, parentState),
					parentID:      parentStateID,
					caminoDiff:    &caminoDiff{},
				}
			},
			removedProposalIDs: set.Set[ids.ID]{
				earlyProposalTxID1: struct{}{},
			},
			expectedDiff: func(actualDiff *diff) *diff {
				return &diff{
					stateVersions: actualDiff.stateVersions,
					parentID:      actualDiff.parentID,
					caminoDiff:    &caminoDiff{},
				}
			},
			expectedNextExpirationTime: earlyProposal.EndTime(),
			expectedNextToExpireIDs:    []ids.ID{earlyProposalTxID2},
		},
		"OK: proposals in added (late) and parent state (early), but all parent removed in arg": {
			diff: func(c *gomock.Controller, removedProposalIDs set.Set[ids.ID]) *diff {
				parentState := NewMockChain(c)
				parentState.EXPECT().GetNextToExpireProposalIDsAndTime(removedProposalIDs).
					Return(nil, mockable.MaxTime, database.ErrNotFound)
				return &diff{
					stateVersions: newMockStateVersions(c, parentStateID, parentState),
					parentID:      parentStateID,
					caminoDiff: &caminoDiff{
						modifiedProposals: map[ids.ID]*proposalDiff{
							lateProposalTxID1: {Proposal: lateProposal, added: true},
						},
					},
				}
			},
			removedProposalIDs: set.Set[ids.ID]{
				earlyProposalTxID1: struct{}{},
			},
			expectedNextToExpireIDs: []ids.ID{lateProposalTxID1},
			expectedDiff: func(actualDiff *diff) *diff {
				return &diff{
					stateVersions: actualDiff.stateVersions,
					parentID:      actualDiff.parentID,
					caminoDiff: &caminoDiff{
						modifiedProposals: map[ids.ID]*proposalDiff{
							lateProposalTxID1: {Proposal: lateProposal, added: true},
						},
					},
				}
			},
			expectedNextExpirationTime: lateProposal.EndTime(),
		},
		"OK: proposals in added (late) and parent state (early), but one parent removed in arg": {
			diff: func(c *gomock.Controller, removedProposalIDs set.Set[ids.ID]) *diff {
				parentState := NewMockChain(c)
				parentState.EXPECT().GetNextToExpireProposalIDsAndTime(removedProposalIDs).
					Return([]ids.ID{earlyProposalTxID2}, earlyProposal.EndTime(), nil)
				return &diff{
					stateVersions: newMockStateVersions(c, parentStateID, parentState),
					parentID:      parentStateID,
					caminoDiff: &caminoDiff{
						modifiedProposals: map[ids.ID]*proposalDiff{
							lateProposalTxID1: {Proposal: lateProposal, added: true},
						},
					},
				}
			},
			removedProposalIDs: set.Set[ids.ID]{
				earlyProposalTxID1: struct{}{},
			},
			expectedNextToExpireIDs: []ids.ID{earlyProposalTxID2},
			expectedDiff: func(actualDiff *diff) *diff {
				return &diff{
					stateVersions: actualDiff.stateVersions,
					parentID:      actualDiff.parentID,
					caminoDiff: &caminoDiff{
						modifiedProposals: map[ids.ID]*proposalDiff{
							lateProposalTxID1: {Proposal: lateProposal, added: true},
						},
					},
				}
			},
			expectedNextExpirationTime: earlyProposal.EndTime(),
		},
		"OK: proposals in added (early) and parent state (late), but all parent removed in arg": {
			diff: func(c *gomock.Controller, removedProposalIDs set.Set[ids.ID]) *diff {
				parentState := NewMockChain(c)
				parentState.EXPECT().GetNextToExpireProposalIDsAndTime(removedProposalIDs).
					Return(nil, mockable.MaxTime, nil)
				return &diff{
					stateVersions: newMockStateVersions(c, parentStateID, parentState),
					parentID:      parentStateID,
					caminoDiff: &caminoDiff{
						modifiedProposals: map[ids.ID]*proposalDiff{
							earlyProposalTxID1: {Proposal: earlyProposal, added: true},
						},
					},
				}
			},
			removedProposalIDs: set.Set[ids.ID]{
				lateProposalTxID1: struct{}{},
			},
			expectedNextToExpireIDs: []ids.ID{earlyProposalTxID1},
			expectedDiff: func(actualDiff *diff) *diff {
				return &diff{
					stateVersions: actualDiff.stateVersions,
					parentID:      actualDiff.parentID,
					caminoDiff: &caminoDiff{
						modifiedProposals: map[ids.ID]*proposalDiff{
							earlyProposalTxID1: {Proposal: earlyProposal, added: true},
						},
					},
				}
			},
			expectedNextExpirationTime: earlyProposal.EndTime(),
		},
		"OK: proposals in added (early) and parent state (late), but one parent removed in arg": {
			diff: func(c *gomock.Controller, removedProposalIDs set.Set[ids.ID]) *diff {
				parentState := NewMockChain(c)
				parentState.EXPECT().GetNextToExpireProposalIDsAndTime(removedProposalIDs).
					Return([]ids.ID{lateProposalTxID2}, lateProposal.EndTime(), nil)
				return &diff{
					stateVersions: newMockStateVersions(c, parentStateID, parentState),
					parentID:      parentStateID,
					caminoDiff: &caminoDiff{
						modifiedProposals: map[ids.ID]*proposalDiff{
							earlyProposalTxID1: {Proposal: earlyProposal, added: true},
						},
					},
				}
			},
			removedProposalIDs: set.Set[ids.ID]{
				lateProposalTxID1: struct{}{},
			},
			expectedNextToExpireIDs: []ids.ID{earlyProposalTxID1},
			expectedDiff: func(actualDiff *diff) *diff {
				return &diff{
					stateVersions: actualDiff.stateVersions,
					parentID:      actualDiff.parentID,
					caminoDiff: &caminoDiff{
						modifiedProposals: map[ids.ID]*proposalDiff{
							earlyProposalTxID1: {Proposal: earlyProposal, added: true},
						},
					},
				}
			},
			expectedNextExpirationTime: earlyProposal.EndTime(),
		},
		"Fail: proposals in parent state only, but all removed (one in arg, one in diff)": {
			diff: func(c *gomock.Controller, removedProposalIDs set.Set[ids.ID]) *diff {
				removedProposalIDs.Add(earlyProposalTxID1)
				parentState := NewMockChain(c)
				parentState.EXPECT().GetNextToExpireProposalIDsAndTime(removedProposalIDs).
					Return(nil, mockable.MaxTime, database.ErrNotFound)
				return &diff{
					stateVersions: newMockStateVersions(c, parentStateID, parentState),
					parentID:      parentStateID,
					caminoDiff: &caminoDiff{
						modifiedProposals: map[ids.ID]*proposalDiff{
							earlyProposalTxID1: {Proposal: earlyProposal, removed: true},
						},
					},
				}
			},
			removedProposalIDs: set.Set[ids.ID]{
				earlyProposalTxID2: struct{}{},
			},
			expectedDiff: func(actualDiff *diff) *diff {
				return &diff{
					stateVersions: actualDiff.stateVersions,
					parentID:      actualDiff.parentID,
					caminoDiff: &caminoDiff{
						modifiedProposals: map[ids.ID]*proposalDiff{
							earlyProposalTxID1: {Proposal: earlyProposal, removed: true},
						},
					},
				}
			},
			expectedNextExpirationTime: mockable.MaxTime,
			expectedErr:                database.ErrNotFound,
		},
		"OK: proposals in parent state only, but some removed (one in arg, one in diff)": {
			diff: func(c *gomock.Controller, removedProposalIDs set.Set[ids.ID]) *diff {
				removedProposalIDs.Add(earlyProposalTxID1)
				parentState := NewMockChain(c)
				parentState.EXPECT().GetNextToExpireProposalIDsAndTime(removedProposalIDs).
					Return([]ids.ID{earlyProposalTxID3}, earlyProposal.EndTime(), nil)
				return &diff{
					stateVersions: newMockStateVersions(c, parentStateID, parentState),
					parentID:      parentStateID,
					caminoDiff: &caminoDiff{
						modifiedProposals: map[ids.ID]*proposalDiff{
							earlyProposalTxID1: {Proposal: earlyProposal, removed: true},
						},
					},
				}
			},
			removedProposalIDs: set.Set[ids.ID]{
				earlyProposalTxID2: struct{}{},
			},
			expectedDiff: func(actualDiff *diff) *diff {
				return &diff{
					stateVersions: actualDiff.stateVersions,
					parentID:      actualDiff.parentID,
					caminoDiff: &caminoDiff{
						modifiedProposals: map[ids.ID]*proposalDiff{
							earlyProposalTxID1: {Proposal: earlyProposal, removed: true},
						},
					},
				}
			},
			expectedNextExpirationTime: earlyProposal.EndTime(),
			expectedNextToExpireIDs:    []ids.ID{earlyProposalTxID3},
		},
		"OK: proposals in added (late) and parent state (early), but all parent removed (one in arg, one in diff)": {
			diff: func(c *gomock.Controller, removedProposalIDs set.Set[ids.ID]) *diff {
				removedProposalIDs.Add(earlyProposalTxID1)
				parentState := NewMockChain(c)
				parentState.EXPECT().GetNextToExpireProposalIDsAndTime(removedProposalIDs).
					Return(nil, mockable.MaxTime, database.ErrNotFound)
				return &diff{
					stateVersions: newMockStateVersions(c, parentStateID, parentState),
					parentID:      parentStateID,
					caminoDiff: &caminoDiff{
						modifiedProposals: map[ids.ID]*proposalDiff{
							lateProposalTxID1:  {Proposal: lateProposal, added: true},
							earlyProposalTxID1: {Proposal: lateProposal, removed: true},
						},
					},
				}
			},
			removedProposalIDs: set.Set[ids.ID]{
				earlyProposalTxID2: struct{}{},
			},
			expectedNextToExpireIDs: []ids.ID{lateProposalTxID1},
			expectedDiff: func(actualDiff *diff) *diff {
				return &diff{
					stateVersions: actualDiff.stateVersions,
					parentID:      actualDiff.parentID,
					caminoDiff: &caminoDiff{
						modifiedProposals: map[ids.ID]*proposalDiff{
							lateProposalTxID1:  {Proposal: lateProposal, added: true},
							earlyProposalTxID1: {Proposal: lateProposal, removed: true},
						},
					},
				}
			},
			expectedNextExpirationTime: lateProposal.EndTime(),
		},
		"OK: proposals in added (late) and parent state (early), but some parent removed (one in arg, one in diff)": {
			diff: func(c *gomock.Controller, removedProposalIDs set.Set[ids.ID]) *diff {
				removedProposalIDs.Add(earlyProposalTxID1)
				parentState := NewMockChain(c)
				parentState.EXPECT().GetNextToExpireProposalIDsAndTime(removedProposalIDs).
					Return([]ids.ID{earlyProposalTxID3}, earlyProposal.EndTime(), nil)
				return &diff{
					stateVersions: newMockStateVersions(c, parentStateID, parentState),
					parentID:      parentStateID,
					caminoDiff: &caminoDiff{
						modifiedProposals: map[ids.ID]*proposalDiff{
							lateProposalTxID1:  {Proposal: lateProposal, added: true},
							earlyProposalTxID1: {Proposal: lateProposal, removed: true},
						},
					},
				}
			},
			removedProposalIDs: set.Set[ids.ID]{
				earlyProposalTxID2: struct{}{},
			},
			expectedNextToExpireIDs: []ids.ID{earlyProposalTxID3},
			expectedDiff: func(actualDiff *diff) *diff {
				return &diff{
					stateVersions: actualDiff.stateVersions,
					parentID:      actualDiff.parentID,
					caminoDiff: &caminoDiff{
						modifiedProposals: map[ids.ID]*proposalDiff{
							lateProposalTxID1:  {Proposal: lateProposal, added: true},
							earlyProposalTxID1: {Proposal: lateProposal, removed: true},
						},
					},
				}
			},
			expectedNextExpirationTime: earlyProposal.EndTime(),
		},
		"OK: proposals in added (early) and parent state (late), but all parent removed (one in arg, one in diff)": {
			diff: func(c *gomock.Controller, removedProposalIDs set.Set[ids.ID]) *diff {
				removedProposalIDs.Add(lateProposalTxID1)
				parentState := NewMockChain(c)
				parentState.EXPECT().GetNextToExpireProposalIDsAndTime(removedProposalIDs).
					Return(nil, mockable.MaxTime, nil)
				return &diff{
					stateVersions: newMockStateVersions(c, parentStateID, parentState),
					parentID:      parentStateID,
					caminoDiff: &caminoDiff{
						modifiedProposals: map[ids.ID]*proposalDiff{
							earlyProposalTxID1: {Proposal: earlyProposal, added: true},
							lateProposalTxID1:  {Proposal: lateProposal, removed: true},
						},
					},
				}
			},
			removedProposalIDs: set.Set[ids.ID]{
				lateProposalTxID2: struct{}{},
			},
			expectedNextToExpireIDs: []ids.ID{earlyProposalTxID1},
			expectedDiff: func(actualDiff *diff) *diff {
				return &diff{
					stateVersions: actualDiff.stateVersions,
					parentID:      actualDiff.parentID,
					caminoDiff: &caminoDiff{
						modifiedProposals: map[ids.ID]*proposalDiff{
							earlyProposalTxID1: {Proposal: earlyProposal, added: true},
							lateProposalTxID1:  {Proposal: lateProposal, removed: true},
						},
					},
				}
			},
			expectedNextExpirationTime: earlyProposal.EndTime(),
		},
		"OK: proposals in added (early) and parent state (late), but some parent removed (one in arg, one in diff)": {
			diff: func(c *gomock.Controller, removedProposalIDs set.Set[ids.ID]) *diff {
				removedProposalIDs.Add(lateProposalTxID1)
				parentState := NewMockChain(c)
				parentState.EXPECT().GetNextToExpireProposalIDsAndTime(removedProposalIDs).
					Return([]ids.ID{lateProposalTxID3}, lateProposal.EndTime(), nil)
				return &diff{
					stateVersions: newMockStateVersions(c, parentStateID, parentState),
					parentID:      parentStateID,
					caminoDiff: &caminoDiff{
						modifiedProposals: map[ids.ID]*proposalDiff{
							earlyProposalTxID1: {Proposal: earlyProposal, added: true},
							lateProposalTxID1:  {Proposal: lateProposal, removed: true},
						},
					},
				}
			},
			removedProposalIDs: set.Set[ids.ID]{
				lateProposalTxID2: struct{}{},
			},
			expectedNextToExpireIDs: []ids.ID{earlyProposalTxID1},
			expectedDiff: func(actualDiff *diff) *diff {
				return &diff{
					stateVersions: actualDiff.stateVersions,
					parentID:      actualDiff.parentID,
					caminoDiff: &caminoDiff{
						modifiedProposals: map[ids.ID]*proposalDiff{
							earlyProposalTxID1: {Proposal: earlyProposal, added: true},
							lateProposalTxID1:  {Proposal: lateProposal, removed: true},
						},
					},
				}
			},
			expectedNextExpirationTime: earlyProposal.EndTime(),
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()
			actualDiff := tt.diff(ctrl, tt.removedProposalIDs)
			nextToExpireIDs, nextExpirationTime, err := actualDiff.GetNextToExpireProposalIDsAndTime(tt.removedProposalIDs)
			require.ErrorIs(t, err, tt.expectedErr)
			require.Equal(t, tt.expectedNextExpirationTime, nextExpirationTime)
			require.Equal(t, tt.expectedNextToExpireIDs, nextToExpireIDs)
			require.Equal(t, tt.expectedDiff(actualDiff), actualDiff)
		})
	}
}

func TestDiffGetBaseFee(t *testing.T) {
	parentStateID := ids.ID{123}
	baseFee := uint64(123)
	testErr := errors.New("test err")

	tests := map[string]struct {
		diff            func(*gomock.Controller) *diff
		expectedDiff    func(actualDiff *diff) *diff
		expectedBaseFee uint64
		expectedErr     error
	}{
		"OK: modified": {
			diff: func(c *gomock.Controller) *diff {
				return &diff{caminoDiff: &caminoDiff{
					modifiedBaseFee: &baseFee,
				}}
			},
			expectedDiff: func(actualDiff *diff) *diff {
				return &diff{caminoDiff: &caminoDiff{
					modifiedBaseFee: &baseFee,
				}}
			},
			expectedBaseFee: baseFee,
		},
		"OK: in parent": {
			diff: func(c *gomock.Controller) *diff {
				parentState := NewMockChain(c)
				parentState.EXPECT().GetBaseFee().Return(baseFee, nil)
				return &diff{
					stateVersions: newMockStateVersions(c, parentStateID, parentState),
					parentID:      parentStateID,
					caminoDiff:    &caminoDiff{},
				}
			},
			expectedDiff: func(actualDiff *diff) *diff {
				return &diff{
					caminoDiff:    &caminoDiff{},
					parentID:      parentStateID,
					stateVersions: actualDiff.stateVersions,
				}
			},
			expectedBaseFee: baseFee,
		},
		"Fail: parent errored": {
			diff: func(c *gomock.Controller) *diff {
				parentState := NewMockChain(c)
				parentState.EXPECT().GetBaseFee().Return(uint64(0), testErr)
				return &diff{
					stateVersions: newMockStateVersions(c, parentStateID, parentState),
					parentID:      parentStateID,
					caminoDiff:    &caminoDiff{},
				}
			},
			expectedDiff: func(actualDiff *diff) *diff {
				return &diff{
					stateVersions: actualDiff.stateVersions,
					parentID:      actualDiff.parentID,
					caminoDiff:    &caminoDiff{},
				}
			},
			expectedErr: testErr,
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()
			actualDiff := tt.diff(ctrl)
			baseFee, err := actualDiff.GetBaseFee()
			require.ErrorIs(t, err, tt.expectedErr)
			require.Equal(t, tt.expectedBaseFee, baseFee)
			require.Equal(t, tt.expectedDiff(actualDiff), actualDiff)
		})
	}
}

func TestDiffSetBaseFee(t *testing.T) {
	baseFee := uint64(123)

	tests := map[string]struct {
		diff         *diff
		baseFee      uint64
		expectedDiff *diff
	}{
		"OK": {
			diff:    &diff{caminoDiff: &caminoDiff{}},
			baseFee: baseFee,
			expectedDiff: &diff{caminoDiff: &caminoDiff{
				modifiedBaseFee: &baseFee,
			}},
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			tt.diff.SetBaseFee(tt.baseFee)
			require.Equal(t, tt.expectedDiff, tt.diff)
		})
	}
}

func TestDiffApplyCaminoState(t *testing.T) {
	reward := uint64(12345)
	baseFee := uint64(6789)
	tests := map[string]struct {
		diff         *diff
		state        func(*gomock.Controller, *diff) *MockState
		expectedDiff *diff
	}{
		"OK": {
			diff: &diff{caminoDiff: &caminoDiff{
				modifiedAddressStates: map[ids.ShortID]txs.AddressState{
					{1}: 101,
					{2}: 0,
				},
				modifiedDepositOffers: map[ids.ID]*deposit.Offer{
					{3}: {ID: ids.ID{3}},
					{4}: nil,
				},
				modifiedDeposits: map[ids.ID]*depositDiff{
					{5}: {Deposit: &deposit.Deposit{Amount: 105}},
					{6}: {Deposit: &deposit.Deposit{Amount: 106}, added: true},
					{7}: {Deposit: &deposit.Deposit{Amount: 107}, removed: true},
				},
				modifiedMultisigAliases: map[ids.ShortID]*multisig.AliasWithNonce{
					{8}: {Alias: multisig.Alias{ID: ids.ShortID{108}}},
					{9}: nil,
				},
				modifiedShortLinks: map[ids.ID]*ids.ShortID{
					toShortLinkKey(ids.ShortID{10}, ShortLinkKeyRegisterNode): {110},
					toShortLinkKey(ids.ShortID{11}, ShortLinkKeyRegisterNode): nil,
				},
				modifiedClaimables: map[ids.ID]*Claimable{
					{12}: {ValidatorReward: 112},
					{13}: nil,
				},
				modifiedProposals: map[ids.ID]*proposalDiff{
					{14}: {Proposal: &dac.BaseFeeProposalState{
						AllowedVoters: []ids.ShortID{},
						SimpleVoteOptions: dac.SimpleVoteOptions[uint64]{
							Options: []dac.SimpleVoteOption[uint64]{{Value: 1, Weight: 1}},
						},
					}},
					{15}: {Proposal: &dac.BaseFeeProposalState{
						AllowedVoters: []ids.ShortID{{115}},
						SimpleVoteOptions: dac.SimpleVoteOptions[uint64]{
							Options: []dac.SimpleVoteOption[uint64]{{Value: 2}},
						},
					}, added: true},
					{16}: {Proposal: &dac.BaseFeeProposalState{
						SimpleVoteOptions: dac.SimpleVoteOptions[uint64]{
							Options: []dac.SimpleVoteOption[uint64]{{Value: 3}},
						},
					}, removed: true},
				},
				modifiedProposalIDsToFinish: map[ids.ID]bool{
					{17}: true, {18}: false, {19}: true, {20}: false,
				},
				modifiedNotDistributedValidatorReward: &reward,
				modifiedBaseFee:                       &baseFee,
				deferredStakerDiffs:                   diffStakers{},
			}},
			state: func(c *gomock.Controller, d *diff) *MockState {
				s := NewMockState(c)
				s.EXPECT().SetNotDistributedValidatorReward(*d.caminoDiff.modifiedNotDistributedValidatorReward)
				s.EXPECT().SetBaseFee(*d.caminoDiff.modifiedBaseFee)
				for k, v := range d.caminoDiff.modifiedAddressStates {
					s.EXPECT().SetAddressStates(k, v)
				}
				for _, depositOffer := range d.caminoDiff.modifiedDepositOffers {
					s.EXPECT().SetDepositOffer(depositOffer)
				}
				for depositTxID, depositDiff := range d.caminoDiff.modifiedDeposits {
					switch {
					case depositDiff.added:
						s.EXPECT().AddDeposit(depositTxID, depositDiff.Deposit)
					case depositDiff.removed:
						s.EXPECT().RemoveDeposit(depositTxID, depositDiff.Deposit)
					default:
						s.EXPECT().ModifyDeposit(depositTxID, depositDiff.Deposit)
					}
				}
				for _, v := range d.caminoDiff.modifiedMultisigAliases {
					s.EXPECT().SetMultisigAlias(v)
				}
				for fullKey, link := range d.caminoDiff.modifiedShortLinks {
					id, key := fromShortLinkKey(fullKey)
					s.EXPECT().SetShortIDLink(id, key, link)
				}
				for ownerID, claimable := range d.caminoDiff.modifiedClaimables {
					s.EXPECT().SetClaimable(ownerID, claimable)
				}
				for proposalID, proposalDiff := range d.caminoDiff.modifiedProposals {
					switch {
					case proposalDiff.added:
						s.EXPECT().AddProposal(proposalID, proposalDiff.Proposal)
					case proposalDiff.removed:
						s.EXPECT().RemoveProposal(proposalID, proposalDiff.Proposal)
					default:
						s.EXPECT().ModifyProposal(proposalID, proposalDiff.Proposal)
					}
				}
				for proposalID, added := range d.caminoDiff.modifiedProposalIDsToFinish {
					if added {
						s.EXPECT().AddProposalIDToFinish(proposalID)
					} else {
						s.EXPECT().RemoveProposalIDToFinish(proposalID)
					}
				}
				for _, validatorDiffs := range d.caminoDiff.deferredStakerDiffs.validatorDiffs {
					for _, validatorDiff := range validatorDiffs {
						switch validatorDiff.validatorStatus {
						case deleted:
							s.EXPECT().DeleteDeferredValidator(validatorDiff.validator)
						case added:
							s.EXPECT().PutDeferredValidator(validatorDiff.validator)
						}
					}
				}
				return s
			},
			expectedDiff: &diff{caminoDiff: &caminoDiff{
				modifiedAddressStates: map[ids.ShortID]txs.AddressState{
					{1}: 101,
					{2}: 0,
				},
				modifiedDepositOffers: map[ids.ID]*deposit.Offer{
					{3}: {ID: ids.ID{3}},
					{4}: nil,
				},
				modifiedDeposits: map[ids.ID]*depositDiff{
					{5}: {Deposit: &deposit.Deposit{Amount: 105}},
					{6}: {Deposit: &deposit.Deposit{Amount: 106}, added: true},
					{7}: {Deposit: &deposit.Deposit{Amount: 107}, removed: true},
				},
				modifiedMultisigAliases: map[ids.ShortID]*multisig.AliasWithNonce{
					{8}: {Alias: multisig.Alias{ID: ids.ShortID{108}}},
					{9}: nil,
				},
				modifiedShortLinks: map[ids.ID]*ids.ShortID{
					toShortLinkKey(ids.ShortID{10}, ShortLinkKeyRegisterNode): {110},
					toShortLinkKey(ids.ShortID{11}, ShortLinkKeyRegisterNode): nil,
				},
				modifiedClaimables: map[ids.ID]*Claimable{
					{12}: {ValidatorReward: 112},
					{13}: nil,
				},
				modifiedProposals: map[ids.ID]*proposalDiff{
					{14}: {Proposal: &dac.BaseFeeProposalState{
						AllowedVoters: []ids.ShortID{},
						SimpleVoteOptions: dac.SimpleVoteOptions[uint64]{
							Options: []dac.SimpleVoteOption[uint64]{{Value: 1, Weight: 1}},
						},
					}},
					{15}: {Proposal: &dac.BaseFeeProposalState{
						AllowedVoters: []ids.ShortID{{115}},
						SimpleVoteOptions: dac.SimpleVoteOptions[uint64]{
							Options: []dac.SimpleVoteOption[uint64]{{Value: 2}},
						},
					}, added: true},
					{16}: {Proposal: &dac.BaseFeeProposalState{
						SimpleVoteOptions: dac.SimpleVoteOptions[uint64]{
							Options: []dac.SimpleVoteOption[uint64]{{Value: 3}},
						},
					}, removed: true},
				},
				modifiedProposalIDsToFinish: map[ids.ID]bool{
					{17}: true, {18}: false, {19}: true, {20}: false,
				},
				deferredStakerDiffs:                   diffStakers{},
				modifiedNotDistributedValidatorReward: &reward,
				modifiedBaseFee:                       &baseFee,
			}},
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()
			tt.diff.ApplyCaminoState(tt.state(ctrl, tt.diff))
			require.Equal(t, tt.expectedDiff, tt.diff)
		})
	}
}

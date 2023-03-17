// Copyright (C) 2023, Chain4Travel AG. All rights reserved.
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
	midDepositTxID := ids.ID{10}
	lateDepositTxID1 := ids.ID{100}
	lateDepositTxID2 := ids.ID{200}
	earlyDeposit := &deposit.Deposit{Duration: 101}
	midDeposit := &deposit.Deposit{Duration: 102}
	lateDeposit := &deposit.Deposit{Duration: 103}

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
					[]ids.ID{earlyDepositTxID1, earlyDepositTxID2},
					earlyDeposit.EndTime(),
					nil,
				)
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
			expectedNextUnlockTime: time.Time{},
			expectedErr:            errInvalidatedNextToUnlockTime,
		},
		"Fail: deposits in parent state only, but one removed": {
			diff: func(c *gomock.Controller) *diff {
				parentState := NewMockChain(c)
				parentState.EXPECT().GetNextToUnlockDepositIDsAndTime().Return(
					[]ids.ID{earlyDepositTxID1, earlyDepositTxID2},
					earlyDeposit.EndTime(),
					nil,
				)
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
			expectedNextUnlockTime: time.Time{},
			expectedErr:            errInvalidatedNextToUnlockTime,
		},
		"Fail: deposits in added (late) and parent state (early), but all removed": {
			diff: func(c *gomock.Controller) *diff {
				parentState := NewMockChain(c)
				parentState.EXPECT().GetNextToUnlockDepositIDsAndTime().Return(
					[]ids.ID{earlyDepositTxID1, earlyDepositTxID2},
					earlyDeposit.EndTime(),
					nil,
				)
				return &diff{
					stateVersions: newMockStateVersions(c, parentStateID, parentState),
					parentID:      parentStateID,
					caminoDiff: &caminoDiff{
						modifiedDeposits: map[ids.ID]*depositDiff{
							lateDepositTxID1:  {Deposit: lateDeposit, added: true},
							midDepositTxID:    {Deposit: midDeposit, removed: true},
							earlyDepositTxID1: {Deposit: earlyDeposit, removed: true},
							earlyDepositTxID2: {Deposit: earlyDeposit, removed: true},
						},
					},
				}
			},
			expectedNextUnlockTime: time.Time{},
			expectedErr:            errInvalidatedNextToUnlockTime,
		},
		"Fail: deposits in added (late) and parent state (early), but one removed": {
			diff: func(c *gomock.Controller) *diff {
				parentState := NewMockChain(c)
				parentState.EXPECT().GetNextToUnlockDepositIDsAndTime().Return(
					[]ids.ID{earlyDepositTxID1, earlyDepositTxID2},
					earlyDeposit.EndTime(),
					nil,
				)
				return &diff{
					stateVersions: newMockStateVersions(c, parentStateID, parentState),
					parentID:      parentStateID,
					caminoDiff: &caminoDiff{
						modifiedDeposits: map[ids.ID]*depositDiff{
							lateDepositTxID1:  {Deposit: lateDeposit, added: true},
							midDepositTxID:    {Deposit: midDeposit, removed: true},
							earlyDepositTxID1: {Deposit: earlyDeposit, removed: true},
						},
					},
				}
			},
			expectedNextUnlockTime: time.Time{},
			expectedErr:            errInvalidatedNextToUnlockTime,
		},
		"Fail: deposits in added (early) and parent state (late), but all removed": {
			diff: func(c *gomock.Controller) *diff {
				parentState := NewMockChain(c)
				parentState.EXPECT().GetNextToUnlockDepositIDsAndTime().Return(
					[]ids.ID{lateDepositTxID1, lateDepositTxID2},
					earlyDeposit.EndTime(),
					nil,
				)
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
			expectedNextUnlockTime: time.Time{},
			expectedErr:            errInvalidatedNextToUnlockTime,
		},
		"Fail: deposits in added (early) and parent state (late), but one removed": {
			diff: func(c *gomock.Controller) *diff {
				parentState := NewMockChain(c)
				parentState.EXPECT().GetNextToUnlockDepositIDsAndTime().Return(
					[]ids.ID{lateDepositTxID1, lateDepositTxID2},
					earlyDeposit.EndTime(),
					nil,
				)
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
			expectedNextUnlockTime: time.Time{},
			expectedErr:            errInvalidatedNextToUnlockTime,
		},
		"OK: deposits in added only": {
			diff: func(c *gomock.Controller) *diff {
				parentState := NewMockChain(c)
				parentState.EXPECT().GetNextToUnlockDepositIDsAndTime().Return(nil, time.Time{}, database.ErrNotFound)
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
			diff: func(c *gomock.Controller) *diff {
				parentState := NewMockChain(c)
				parentState.EXPECT().GetNextToUnlockDepositIDsAndTime().Return(
					[]ids.ID{earlyDepositTxID1, lateDepositTxID1},
					earlyDeposit.EndTime(),
					nil,
				)
				return &diff{
					stateVersions: newMockStateVersions(c, parentStateID, parentState),
					parentID:      parentStateID,
					caminoDiff:    &caminoDiff{},
				}
			},
			expectedNextUnlockTime: earlyDeposit.EndTime(),
		},
		"OK: deposits in added (late) and parent state (early)": {
			diff: func(c *gomock.Controller) *diff {
				parentState := NewMockChain(c)
				parentState.EXPECT().GetNextToUnlockDepositIDsAndTime().Return(
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
			expectedNextUnlockTime: earlyDeposit.EndTime(),
		},
		"OK: deposits in added (early, mid) and parent state (late)": {
			diff: func(c *gomock.Controller) *diff {
				parentState := NewMockChain(c)
				parentState.EXPECT().GetNextToUnlockDepositIDsAndTime().Return(
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
			expectedNextUnlockTime: earlyDeposit.EndTime(),
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

func TestDiffGetNextToUnlockDepositIDsAndTime(t *testing.T) {
	parentStateID := ids.GenerateTestID()
	earlyDepositTxID1 := ids.ID{1}
	earlyDepositTxID2 := ids.ID{2}
	midDepositTxID := ids.ID{10}
	lateDepositTxID1 := ids.ID{100}
	lateDepositTxID2 := ids.ID{200}
	earlyDeposit := &deposit.Deposit{Duration: 101}
	midDeposit := &deposit.Deposit{Duration: 102}
	lateDeposit := &deposit.Deposit{Duration: 103}

	tests := map[string]struct {
		diff                   func(*gomock.Controller) *diff
		expectedNextUnlockIDs  []ids.ID
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
					[]ids.ID{earlyDepositTxID1, earlyDepositTxID2},
					earlyDeposit.EndTime(),
					nil,
				)
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
			expectedNextUnlockTime: time.Time{},
			expectedErr:            errInvalidatedNextToUnlockTime,
		},
		"Fail: deposits in parent state only, but one removed": {
			diff: func(c *gomock.Controller) *diff {
				parentState := NewMockChain(c)
				parentState.EXPECT().GetNextToUnlockDepositIDsAndTime().Return(
					[]ids.ID{earlyDepositTxID1, earlyDepositTxID2},
					earlyDeposit.EndTime(),
					nil,
				)
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
			expectedNextUnlockTime: time.Time{},
			expectedErr:            errInvalidatedNextToUnlockTime,
		},
		"Fail: deposits in added (late) and parent state (early), but all removed": {
			diff: func(c *gomock.Controller) *diff {
				parentState := NewMockChain(c)
				parentState.EXPECT().GetNextToUnlockDepositIDsAndTime().Return(
					[]ids.ID{earlyDepositTxID1, earlyDepositTxID2},
					earlyDeposit.EndTime(),
					nil,
				)
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
			expectedNextUnlockIDs:  nil,
			expectedNextUnlockTime: time.Time{},
			expectedErr:            errInvalidatedNextToUnlockTime,
		},
		"Fail: deposits in added (late) and parent state (early), but one removed": {
			diff: func(c *gomock.Controller) *diff {
				parentState := NewMockChain(c)
				parentState.EXPECT().GetNextToUnlockDepositIDsAndTime().Return(
					[]ids.ID{earlyDepositTxID1, earlyDepositTxID2},
					earlyDeposit.EndTime(),
					nil,
				)
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
			expectedNextUnlockIDs:  nil,
			expectedNextUnlockTime: time.Time{},
			expectedErr:            errInvalidatedNextToUnlockTime,
		},
		"Fail: deposits in added (early) and parent state (late), but all removed": {
			diff: func(c *gomock.Controller) *diff {
				parentState := NewMockChain(c)
				parentState.EXPECT().GetNextToUnlockDepositIDsAndTime().Return(
					[]ids.ID{lateDepositTxID1, lateDepositTxID2},
					earlyDeposit.EndTime(),
					nil,
				)
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
			expectedNextUnlockIDs:  nil,
			expectedNextUnlockTime: time.Time{},
			expectedErr:            errInvalidatedNextToUnlockTime,
		},
		"Fail: deposits in added (early) and parent state (late), but one removed": {
			diff: func(c *gomock.Controller) *diff {
				parentState := NewMockChain(c)
				parentState.EXPECT().GetNextToUnlockDepositIDsAndTime().Return(
					[]ids.ID{lateDepositTxID1, lateDepositTxID2},
					earlyDeposit.EndTime(),
					nil,
				)
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
			expectedNextUnlockIDs:  nil,
			expectedNextUnlockTime: time.Time{},
			expectedErr:            errInvalidatedNextToUnlockTime,
		},
		"OK: deposits in added only (early, late)": {
			diff: func(c *gomock.Controller) *diff {
				parentState := NewMockChain(c)
				parentState.EXPECT().GetNextToUnlockDepositIDsAndTime().Return(nil, time.Time{}, database.ErrNotFound)
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
			diff: func(c *gomock.Controller) *diff {
				parentState := NewMockChain(c)
				parentState.EXPECT().GetNextToUnlockDepositIDsAndTime().Return(
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
			diff: func(c *gomock.Controller) *diff {
				parentState := NewMockChain(c)
				parentState.EXPECT().GetNextToUnlockDepositIDsAndTime().Return(
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
			diff: func(c *gomock.Controller) *diff {
				parentState := NewMockChain(c)
				parentState.EXPECT().GetNextToUnlockDepositIDsAndTime().Return(
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
			diff: func(c *gomock.Controller) *diff {
				parentState := NewMockChain(c)
				parentState.EXPECT().GetNextToUnlockDepositIDsAndTime().Return(
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
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()
			nextUnlockIDs, nextUnlockTime, err := tt.diff(ctrl).GetNextToUnlockDepositIDsAndTime()
			require.ErrorIs(t, err, tt.expectedErr)
			require.Equal(t, tt.expectedNextUnlockTime, nextUnlockTime)
			require.Equal(t, tt.expectedNextUnlockIDs, nextUnlockIDs)
		})
	}
}

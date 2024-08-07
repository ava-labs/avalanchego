// Copyright (C) 2022-2023, Chain4Travel AG. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"errors"
	"testing"
	"time"

	"github.com/ava-labs/avalanchego/cache"
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/utils/timer/mockable"
	"github.com/ava-labs/avalanchego/vms/platformvm/blocks"
	"github.com/ava-labs/avalanchego/vms/platformvm/deposit"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/require"
)

func TestGetDeposit(t *testing.T) {
	depositTxID := ids.GenerateTestID()
	deposit1 := &deposit.Deposit{
		RewardOwner: &secp256k1fx.OutputOwners{
			Addrs: []ids.ShortID{{1}},
		},
	}
	depositBytes, err := blocks.GenesisCodec.Marshal(blocks.Version, deposit1)
	require.NoError(t, err)
	testError := errors.New("test error")

	tests := map[string]struct {
		caminoState         func(*gomock.Controller) *caminoState
		depositTxID         ids.ID
		expectedCaminoState func(*caminoState) *caminoState
		expectedDeposit     *deposit.Deposit
		expectedErr         error
	}{
		"Fail: deposit removed": {
			caminoState: func(c *gomock.Controller) *caminoState {
				return &caminoState{
					caminoDiff: &caminoDiff{
						modifiedDeposits: map[ids.ID]*depositDiff{
							depositTxID: {Deposit: deposit1, removed: true},
						},
					},
				}
			},
			expectedCaminoState: func(actualCaminoState *caminoState) *caminoState {
				return &caminoState{
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
		"Fail: deposit in cache, but removed": {
			caminoState: func(c *gomock.Controller) *caminoState {
				cache := cache.NewMockCacher[ids.ID, *deposit.Deposit](c)
				cache.EXPECT().Get(depositTxID).Return(nil, true)
				return &caminoState{
					depositsCache: cache,
					caminoDiff:    &caminoDiff{},
				}
			},
			expectedCaminoState: func(actualCaminoState *caminoState) *caminoState {
				return &caminoState{
					depositsCache: actualCaminoState.depositsCache,
					caminoDiff:    &caminoDiff{},
				}
			},
			depositTxID: depositTxID,
			expectedErr: database.ErrNotFound,
		},
		"OK: deposit added": {
			caminoState: func(c *gomock.Controller) *caminoState {
				return &caminoState{
					caminoDiff: &caminoDiff{
						modifiedDeposits: map[ids.ID]*depositDiff{
							depositTxID: {Deposit: deposit1, added: true},
						},
					},
				}
			},
			expectedCaminoState: func(actualCaminoState *caminoState) *caminoState {
				return &caminoState{
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
		"OK: deposit modified": {
			caminoState: func(c *gomock.Controller) *caminoState {
				return &caminoState{
					caminoDiff: &caminoDiff{
						modifiedDeposits: map[ids.ID]*depositDiff{
							depositTxID: {Deposit: deposit1},
						},
					},
				}
			},
			expectedCaminoState: func(actualCaminoState *caminoState) *caminoState {
				return &caminoState{
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
		"OK: deposit in cache": {
			caminoState: func(c *gomock.Controller) *caminoState {
				cache := cache.NewMockCacher[ids.ID, *deposit.Deposit](c)
				cache.EXPECT().Get(depositTxID).Return(deposit1, true)
				return &caminoState{
					depositsCache: cache,
					caminoDiff:    &caminoDiff{},
				}
			},
			expectedCaminoState: func(actualCaminoState *caminoState) *caminoState {
				return &caminoState{
					depositsCache: actualCaminoState.depositsCache,
					caminoDiff:    &caminoDiff{},
				}
			},
			depositTxID:     depositTxID,
			expectedDeposit: deposit1,
		},
		"OK: deposit in db": {
			caminoState: func(c *gomock.Controller) *caminoState {
				cache := cache.NewMockCacher[ids.ID, *deposit.Deposit](c)
				cache.EXPECT().Get(depositTxID).Return(nil, false)
				cache.EXPECT().Put(depositTxID, deposit1)
				db := database.NewMockDatabase(c)
				db.EXPECT().Get(depositTxID[:]).Return(depositBytes, nil)
				return &caminoState{
					depositsDB:    db,
					depositsCache: cache,
					caminoDiff:    &caminoDiff{},
				}
			},
			expectedCaminoState: func(actualCaminoState *caminoState) *caminoState {
				return &caminoState{
					depositsDB:    actualCaminoState.depositsDB,
					depositsCache: actualCaminoState.depositsCache,
					caminoDiff:    &caminoDiff{},
				}
			},
			depositTxID:     depositTxID,
			expectedDeposit: deposit1,
		},
		"Fail: db error": {
			caminoState: func(c *gomock.Controller) *caminoState {
				cache := cache.NewMockCacher[ids.ID, *deposit.Deposit](c)
				cache.EXPECT().Get(depositTxID).Return(nil, false)
				db := database.NewMockDatabase(c)
				db.EXPECT().Get(depositTxID[:]).Return(nil, testError)
				return &caminoState{
					depositsDB:    db,
					depositsCache: cache,
					caminoDiff:    &caminoDiff{},
				}
			},
			expectedCaminoState: func(actualCaminoState *caminoState) *caminoState {
				return &caminoState{
					depositsDB:    actualCaminoState.depositsDB,
					depositsCache: actualCaminoState.depositsCache,
					caminoDiff:    &caminoDiff{},
				}
			},
			depositTxID: depositTxID,
			expectedErr: testError,
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			caminoState := tt.caminoState(ctrl)
			actualDeposit, err := caminoState.GetDeposit(tt.depositTxID)
			require.ErrorIs(t, err, tt.expectedErr)
			require.Equal(t, tt.expectedDeposit, actualDeposit)
			require.Equal(t, tt.expectedCaminoState(caminoState), caminoState)
		})
	}
}

func TestAddDeposit(t *testing.T) {
	depositTxID := ids.GenerateTestID()
	deposit1 := &deposit.Deposit{Duration: 101, Amount: 1}

	tests := map[string]struct {
		caminoState         *caminoState
		depositTxID         ids.ID
		deposit             *deposit.Deposit
		expectedCaminoState *caminoState
	}{
		"OK": {
			caminoState: &caminoState{
				caminoDiff: &caminoDiff{modifiedDeposits: map[ids.ID]*depositDiff{}},
			},
			depositTxID: depositTxID,
			deposit:     deposit1,
			expectedCaminoState: &caminoState{
				caminoDiff: &caminoDiff{
					modifiedDeposits: map[ids.ID]*depositDiff{
						depositTxID: {Deposit: deposit1, added: true},
					},
				},
			},
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			tt.caminoState.AddDeposit(tt.depositTxID, tt.deposit)
			require.Equal(t, tt.expectedCaminoState, tt.caminoState)
		})
	}
}

func TestModifyDeposit(t *testing.T) {
	depositTxID := ids.GenerateTestID()
	deposit1 := &deposit.Deposit{Duration: 101, Amount: 1}

	tests := map[string]struct {
		caminoState         func(*gomock.Controller) *caminoState
		depositTxID         ids.ID
		deposit             *deposit.Deposit
		expectedCaminoState func(*caminoState) *caminoState
	}{
		"OK": {
			caminoState: func(c *gomock.Controller) *caminoState {
				depositsCache := cache.NewMockCacher[ids.ID, *deposit.Deposit](c)
				depositsCache.EXPECT().Evict(depositTxID)
				return &caminoState{
					depositsCache: depositsCache,
					caminoDiff:    &caminoDiff{modifiedDeposits: map[ids.ID]*depositDiff{}},
				}
			},
			depositTxID: depositTxID,
			deposit:     deposit1,
			expectedCaminoState: func(actualCaminoState *caminoState) *caminoState {
				return &caminoState{
					depositsCache: actualCaminoState.depositsCache,
					caminoDiff: &caminoDiff{
						modifiedDeposits: map[ids.ID]*depositDiff{
							depositTxID: {Deposit: deposit1},
						},
					},
				}
			},
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			actualCaminoState := tt.caminoState(ctrl)
			actualCaminoState.ModifyDeposit(tt.depositTxID, tt.deposit)
			require.Equal(t, tt.expectedCaminoState(actualCaminoState), actualCaminoState)
		})
	}
}

func TestRemoveDeposit(t *testing.T) {
	depositTxID := ids.GenerateTestID()
	deposit1 := &deposit.Deposit{Duration: 101, Amount: 1}

	tests := map[string]struct {
		caminoState         func(*gomock.Controller) *caminoState
		depositTxID         ids.ID
		deposit             *deposit.Deposit
		expectedCaminoState func(*caminoState) *caminoState
	}{
		"OK": {
			caminoState: func(c *gomock.Controller) *caminoState {
				depositsCache := cache.NewMockCacher[ids.ID, *deposit.Deposit](c)
				depositsCache.EXPECT().Evict(depositTxID)
				return &caminoState{
					depositsCache: depositsCache,
					caminoDiff:    &caminoDiff{modifiedDeposits: map[ids.ID]*depositDiff{}},
				}
			},
			depositTxID: depositTxID,
			deposit:     deposit1,
			expectedCaminoState: func(actualCaminoState *caminoState) *caminoState {
				return &caminoState{
					depositsCache: actualCaminoState.depositsCache,
					caminoDiff: &caminoDiff{
						modifiedDeposits: map[ids.ID]*depositDiff{
							depositTxID: {Deposit: deposit1, removed: true},
						},
					},
				}
			},
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			actualCaminoState := tt.caminoState(ctrl)
			actualCaminoState.RemoveDeposit(tt.depositTxID, tt.deposit)
			require.Equal(t, tt.expectedCaminoState(actualCaminoState), actualCaminoState)
		})
	}
}

func TestGetNextToUnlockDepositTime(t *testing.T) {
	depositTxID1 := ids.GenerateTestID()
	depositTxID2 := ids.GenerateTestID()
	depositTxID31 := ids.GenerateTestID()
	depositTxID32 := ids.GenerateTestID()
	deposit2 := &deposit.Deposit{Duration: 102}
	deposit31 := &deposit.Deposit{Duration: 103}
	deposit32 := &deposit.Deposit{Duration: 103}
	deposit1Endtime := time.Unix(100, 0)

	tests := map[string]struct {
		caminoState            func(c *gomock.Controller) *caminoState
		removedDepositIDs      set.Set[ids.ID]
		expectedCaminoState    func(*caminoState) *caminoState
		expectedNextUnlockTime time.Time
		expectedErr            error
	}{
		"Fail: no deposits": {
			caminoState: func(c *gomock.Controller) *caminoState {
				return &caminoState{}
			},
			expectedCaminoState: func(actualCaminoState *caminoState) *caminoState {
				return &caminoState{}
			},
			expectedNextUnlockTime: mockable.MaxTime,
			expectedErr:            database.ErrNotFound,
		},
		"Fail: no deposits (all removed)": {
			caminoState: func(c *gomock.Controller) *caminoState {
				it := database.NewMockIterator(c)
				it.EXPECT().Next().Return(false)
				it.EXPECT().Error().Return(nil)
				it.EXPECT().Release()

				db := database.NewMockDatabase(c)
				db.EXPECT().NewIterator().Return(it)

				return &caminoState{
					depositIDsByEndtimeDB:    db,
					depositsNextToUnlockTime: &deposit1Endtime,
					depositsNextToUnlockIDs:  []ids.ID{depositTxID1},
				}
			},
			removedDepositIDs: set.Set[ids.ID]{depositTxID1: struct{}{}},
			expectedCaminoState: func(actualCaminoState *caminoState) *caminoState {
				return &caminoState{
					depositIDsByEndtimeDB:    actualCaminoState.depositIDsByEndtimeDB,
					depositsNextToUnlockTime: &deposit1Endtime,
					depositsNextToUnlockIDs:  []ids.ID{depositTxID1},
				}
			},
			expectedNextUnlockTime: mockable.MaxTime,
			expectedErr:            database.ErrNotFound,
		},
		"OK": {
			caminoState: func(c *gomock.Controller) *caminoState {
				return &caminoState{
					depositsNextToUnlockTime: &deposit1Endtime,
					depositsNextToUnlockIDs:  []ids.ID{depositTxID1},
				}
			},
			expectedCaminoState: func(actualCaminoState *caminoState) *caminoState {
				return &caminoState{
					depositsNextToUnlockTime: &deposit1Endtime,
					depositsNextToUnlockIDs:  []ids.ID{depositTxID1},
				}
			},
			expectedNextUnlockTime: deposit1Endtime,
		},
		"Ok: in-mem deposits removed, but db has some": {
			caminoState: func(c *gomock.Controller) *caminoState {
				it := database.NewMockIterator(c)
				it.EXPECT().Next().Return(true).Times(3)
				it.EXPECT().Key().Return(depositToKey(depositTxID2[:], deposit2))
				it.EXPECT().Key().Return(depositToKey(depositTxID31[:], deposit31))
				it.EXPECT().Key().Return(depositToKey(depositTxID32[:], deposit32))
				it.EXPECT().Next().Return(false)
				it.EXPECT().Error().Return(nil)
				it.EXPECT().Release()

				db := database.NewMockDatabase(c)
				db.EXPECT().NewIterator().Return(it)

				return &caminoState{
					depositIDsByEndtimeDB:    db,
					depositsNextToUnlockTime: &deposit1Endtime,
					depositsNextToUnlockIDs:  []ids.ID{depositTxID1},
				}
			},
			removedDepositIDs: set.Set[ids.ID]{
				depositTxID1: struct{}{},
				depositTxID2: struct{}{},
			},
			expectedCaminoState: func(actualCaminoState *caminoState) *caminoState {
				return &caminoState{
					depositIDsByEndtimeDB:    actualCaminoState.depositIDsByEndtimeDB,
					depositsNextToUnlockTime: &deposit1Endtime,
					depositsNextToUnlockIDs:  []ids.ID{depositTxID1},
				}
			},
			expectedNextUnlockTime: deposit31.EndTime(),
		},
		"OK: some deposits removed": {
			caminoState: func(c *gomock.Controller) *caminoState {
				return &caminoState{
					depositsNextToUnlockTime: &deposit1Endtime,
					depositsNextToUnlockIDs:  []ids.ID{depositTxID1, depositTxID2},
				}
			},
			removedDepositIDs: set.Set[ids.ID]{
				depositTxID1: struct{}{},
			},
			expectedCaminoState: func(actualCaminoState *caminoState) *caminoState {
				return &caminoState{
					depositsNextToUnlockTime: &deposit1Endtime,
					depositsNextToUnlockIDs:  []ids.ID{depositTxID1, depositTxID2},
				}
			},
			expectedNextUnlockTime: deposit1Endtime,
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			caminoState := tt.caminoState(ctrl)
			nextUnlockTime, err := caminoState.GetNextToUnlockDepositTime(tt.removedDepositIDs)
			require.ErrorIs(t, err, tt.expectedErr)
			require.Equal(t, tt.expectedNextUnlockTime, nextUnlockTime)
			require.Equal(t, tt.expectedCaminoState(caminoState), caminoState)
		})
	}
}

func TestGetNextToUnlockDepositIDsAndTime(t *testing.T) {
	depositTxID1 := ids.GenerateTestID()
	depositTxID2 := ids.GenerateTestID()
	depositTxID31 := ids.GenerateTestID()
	depositTxID32 := ids.GenerateTestID()
	deposit2 := &deposit.Deposit{Duration: 102}
	deposit31 := &deposit.Deposit{Duration: 103}
	deposit32 := &deposit.Deposit{Duration: 103}
	deposit1Endtime := time.Unix(100, 0)

	tests := map[string]struct {
		caminoState            func(c *gomock.Controller) *caminoState
		removedDepositIDs      set.Set[ids.ID]
		expectedCaminoState    func(*caminoState) *caminoState
		expectedNextUnlockTime time.Time
		expectedNextUnlockIDs  []ids.ID
		expectedErr            error
	}{
		"Fail: no deposits": {
			caminoState: func(c *gomock.Controller) *caminoState {
				return &caminoState{}
			},
			expectedCaminoState: func(actualCaminoState *caminoState) *caminoState {
				return &caminoState{}
			},
			expectedNextUnlockTime: mockable.MaxTime,
			expectedErr:            database.ErrNotFound,
		},
		"Fail: no deposits (all removed)": {
			caminoState: func(c *gomock.Controller) *caminoState {
				it := database.NewMockIterator(c)
				it.EXPECT().Next().Return(false)
				it.EXPECT().Error().Return(nil)
				it.EXPECT().Release()

				db := database.NewMockDatabase(c)
				db.EXPECT().NewIterator().Return(it)

				return &caminoState{
					depositIDsByEndtimeDB:    db,
					depositsNextToUnlockTime: &deposit1Endtime,
					depositsNextToUnlockIDs:  []ids.ID{depositTxID1},
				}
			},
			removedDepositIDs: set.Set[ids.ID]{depositTxID1: struct{}{}},
			expectedCaminoState: func(actualCaminoState *caminoState) *caminoState {
				return &caminoState{
					depositIDsByEndtimeDB:    actualCaminoState.depositIDsByEndtimeDB,
					depositsNextToUnlockTime: &deposit1Endtime,
					depositsNextToUnlockIDs:  []ids.ID{depositTxID1},
				}
			},
			expectedNextUnlockTime: mockable.MaxTime,
			expectedErr:            database.ErrNotFound,
		},
		"OK": {
			caminoState: func(c *gomock.Controller) *caminoState {
				return &caminoState{
					depositsNextToUnlockTime: &deposit1Endtime,
					depositsNextToUnlockIDs:  []ids.ID{depositTxID1},
				}
			},
			expectedCaminoState: func(actualCaminoState *caminoState) *caminoState {
				return &caminoState{
					depositsNextToUnlockTime: &deposit1Endtime,
					depositsNextToUnlockIDs:  []ids.ID{depositTxID1},
				}
			},
			expectedNextUnlockTime: deposit1Endtime,
			expectedNextUnlockIDs:  []ids.ID{depositTxID1},
		},
		"Ok: in-mem deposits removed, but db has some": {
			caminoState: func(c *gomock.Controller) *caminoState {
				it := database.NewMockIterator(c)
				it.EXPECT().Next().Return(true).Times(3)
				it.EXPECT().Key().Return(depositToKey(depositTxID2[:], deposit2))
				it.EXPECT().Key().Return(depositToKey(depositTxID31[:], deposit31))
				it.EXPECT().Key().Return(depositToKey(depositTxID32[:], deposit32))
				it.EXPECT().Next().Return(false)
				it.EXPECT().Error().Return(nil)
				it.EXPECT().Release()

				db := database.NewMockDatabase(c)
				db.EXPECT().NewIterator().Return(it)

				return &caminoState{
					depositIDsByEndtimeDB:    db,
					depositsNextToUnlockTime: &deposit1Endtime,
					depositsNextToUnlockIDs:  []ids.ID{depositTxID1},
				}
			},
			removedDepositIDs: set.Set[ids.ID]{
				depositTxID1: struct{}{},
				depositTxID2: struct{}{},
			},
			expectedCaminoState: func(actualCaminoState *caminoState) *caminoState {
				return &caminoState{
					depositIDsByEndtimeDB:    actualCaminoState.depositIDsByEndtimeDB,
					depositsNextToUnlockTime: &deposit1Endtime,
					depositsNextToUnlockIDs:  []ids.ID{depositTxID1},
				}
			},
			expectedNextUnlockTime: deposit31.EndTime(),
			expectedNextUnlockIDs:  []ids.ID{depositTxID31, depositTxID32},
		},
		"OK: some deposits removed": {
			caminoState: func(c *gomock.Controller) *caminoState {
				return &caminoState{
					depositsNextToUnlockTime: &deposit1Endtime,
					depositsNextToUnlockIDs:  []ids.ID{depositTxID1, depositTxID2},
				}
			},
			removedDepositIDs: set.Set[ids.ID]{
				depositTxID1: struct{}{},
			},
			expectedCaminoState: func(actualCaminoState *caminoState) *caminoState {
				return &caminoState{
					depositsNextToUnlockTime: &deposit1Endtime,
					depositsNextToUnlockIDs:  []ids.ID{depositTxID1, depositTxID2},
				}
			},
			expectedNextUnlockTime: deposit1Endtime,
			expectedNextUnlockIDs:  []ids.ID{depositTxID2},
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			caminoState := tt.caminoState(ctrl)
			nextUnlockIDs, nextUnlockTime, err := caminoState.GetNextToUnlockDepositIDsAndTime(tt.removedDepositIDs)
			require.ErrorIs(t, err, tt.expectedErr)
			require.Equal(t, tt.expectedNextUnlockTime, nextUnlockTime)
			require.Equal(t, tt.expectedNextUnlockIDs, nextUnlockIDs)
			require.Equal(t, tt.expectedCaminoState(caminoState), caminoState)
		})
	}
}

func TestWriteDeposits(t *testing.T) {
	testError := errors.New("test error")
	depositTxID1 := ids.ID{1}
	depositTxID2 := ids.ID{2}
	depositTxID3 := ids.ID{3}
	deposit1 := &deposit.Deposit{
		Duration: 101,
		RewardOwner: &secp256k1fx.OutputOwners{
			Addrs: []ids.ShortID{{1}},
		},
	}
	deposit2 := &deposit.Deposit{
		Duration: 101,
		RewardOwner: &secp256k1fx.OutputOwners{
			Addrs: []ids.ShortID{{2}},
		},
	}
	deposit3 := &deposit.Deposit{
		Duration: 101,
		RewardOwner: &secp256k1fx.OutputOwners{
			Addrs: []ids.ShortID{{3}},
		},
	}
	depositEndtime := deposit2.EndTime()
	deposit1Bytes, err := blocks.GenesisCodec.Marshal(blocks.Version, deposit1)
	require.NoError(t, err)
	deposit2Bytes, err := blocks.GenesisCodec.Marshal(blocks.Version, deposit2)
	require.NoError(t, err)

	tests := map[string]struct {
		caminoState         func(*gomock.Controller) *caminoState
		expectedCaminoState func(*caminoState) *caminoState
		expectedErr         error
	}{
		"Fail: db errored on modified deposit Put": {
			caminoState: func(c *gomock.Controller) *caminoState {
				depositsDB := database.NewMockDatabase(c)
				depositsDB.EXPECT().Put(depositTxID1[:], deposit1Bytes).Return(testError)
				return &caminoState{
					caminoDiff: &caminoDiff{
						modifiedDeposits: map[ids.ID]*depositDiff{
							depositTxID1: {Deposit: deposit1},
						},
					},
					depositsDB: depositsDB,
				}
			},
			expectedCaminoState: func(actualCaminoState *caminoState) *caminoState {
				return &caminoState{
					caminoDiff: &caminoDiff{
						modifiedDeposits: map[ids.ID]*depositDiff{},
					},
					depositsDB: actualCaminoState.depositsDB,
				}
			},
			expectedErr: testError,
		},
		"Fail: db errored on added deposit Put": {
			caminoState: func(c *gomock.Controller) *caminoState {
				depositsDB := database.NewMockDatabase(c)
				depositsDB.EXPECT().Put(depositTxID1[:], deposit1Bytes).Return(testError)
				return &caminoState{
					caminoDiff: &caminoDiff{
						modifiedDeposits: map[ids.ID]*depositDiff{
							depositTxID1: {Deposit: deposit1, added: true},
						},
					},
					depositsDB: depositsDB,
				}
			},
			expectedCaminoState: func(actualCaminoState *caminoState) *caminoState {
				return &caminoState{
					caminoDiff: &caminoDiff{
						modifiedDeposits: map[ids.ID]*depositDiff{},
					},
					depositsDB: actualCaminoState.depositsDB,
				}
			},
			expectedErr: testError,
		},
		"Fail: db errored on removed deposit Delete": {
			caminoState: func(c *gomock.Controller) *caminoState {
				depositsDB := database.NewMockDatabase(c)
				depositsDB.EXPECT().Delete(depositTxID1[:]).Return(testError)
				return &caminoState{
					caminoDiff: &caminoDiff{
						modifiedDeposits: map[ids.ID]*depositDiff{
							depositTxID1: {Deposit: deposit1, removed: true},
						},
					},
					depositsDB: depositsDB,
				}
			},
			expectedCaminoState: func(actualCaminoState *caminoState) *caminoState {
				return &caminoState{
					caminoDiff: &caminoDiff{
						modifiedDeposits: map[ids.ID]*depositDiff{},
					},
					depositsDB: actualCaminoState.depositsDB,
				}
			},
			expectedErr: testError,
		},
		"OK: add, modify and delete; nextUnlock partial removal, added new": {
			caminoState: func(c *gomock.Controller) *caminoState {
				depositsDB := database.NewMockDatabase(c)
				depositsDB.EXPECT().Put(depositTxID1[:], deposit1Bytes).Return(nil)
				depositsDB.EXPECT().Put(depositTxID2[:], deposit2Bytes).Return(nil)
				depositsDB.EXPECT().Delete(depositTxID3[:]).Return(nil)

				depositIDsByEndtimeDB := database.NewMockDatabase(c)
				depositIDsByEndtimeDB.EXPECT().Put(depositToKey(depositTxID1[:], deposit1), nil).Return(nil)
				depositIDsByEndtimeDB.EXPECT().Delete(depositToKey(depositTxID3[:], deposit3)).Return(nil)

				return &caminoState{
					depositIDsByEndtimeDB: depositIDsByEndtimeDB,
					depositsDB:            depositsDB,
					caminoDiff: &caminoDiff{
						modifiedDeposits: map[ids.ID]*depositDiff{
							depositTxID1: {Deposit: deposit1, added: true},
							depositTxID2: {Deposit: deposit2},
							depositTxID3: {Deposit: deposit3, removed: true},
						},
					},
					depositsNextToUnlockTime: &depositEndtime,
					depositsNextToUnlockIDs:  []ids.ID{depositTxID2, depositTxID3},
				}
			},
			expectedCaminoState: func(actualCaminoState *caminoState) *caminoState {
				return &caminoState{
					depositIDsByEndtimeDB: actualCaminoState.depositIDsByEndtimeDB,
					depositsDB:            actualCaminoState.depositsDB,
					caminoDiff: &caminoDiff{
						modifiedDeposits: map[ids.ID]*depositDiff{},
					},
					depositsNextToUnlockTime: &depositEndtime,
					depositsNextToUnlockIDs:  []ids.ID{depositTxID1, depositTxID2},
				}
			},
		},
		"OK: nextUnlock full removal, can't add new, peek into db": {
			caminoState: func(c *gomock.Controller) *caminoState {
				depositsDB := database.NewMockDatabase(c)
				depositsDB.EXPECT().Put(depositTxID1[:], deposit1Bytes).Return(nil)
				depositsDB.EXPECT().Delete(depositTxID2[:]).Return(nil)

				depositsIterator := database.NewMockIterator(c)
				depositsIterator.EXPECT().Next().Return(true)
				depositsIterator.EXPECT().Key().Return(depositToKey(depositTxID1[:], deposit1))
				depositsIterator.EXPECT().Next().Return(false)
				depositsIterator.EXPECT().Error().Return(nil)
				depositsIterator.EXPECT().Release()

				depositIDsByEndtimeDB := database.NewMockDatabase(c)
				depositIDsByEndtimeDB.EXPECT().Put(depositToKey(depositTxID1[:], deposit1), nil).Return(nil)
				depositIDsByEndtimeDB.EXPECT().Delete(depositToKey(depositTxID2[:], deposit2)).Return(nil)
				depositIDsByEndtimeDB.EXPECT().NewIterator().Return(depositsIterator)

				return &caminoState{
					depositIDsByEndtimeDB: depositIDsByEndtimeDB,
					depositsDB:            depositsDB,
					caminoDiff: &caminoDiff{
						modifiedDeposits: map[ids.ID]*depositDiff{
							depositTxID1: {Deposit: deposit1, added: true},
							depositTxID2: {Deposit: deposit2, removed: true},
						},
					},
					depositsNextToUnlockTime: &depositEndtime,
					depositsNextToUnlockIDs:  []ids.ID{depositTxID2},
				}
			},
			expectedCaminoState: func(actualCaminoState *caminoState) *caminoState {
				return &caminoState{
					depositIDsByEndtimeDB: actualCaminoState.depositIDsByEndtimeDB,
					depositsDB:            actualCaminoState.depositsDB,
					caminoDiff: &caminoDiff{
						modifiedDeposits: map[ids.ID]*depositDiff{},
					},
					depositsNextToUnlockTime: &depositEndtime,
					depositsNextToUnlockIDs:  []ids.ID{depositTxID1},
				}
			},
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			actualCaminoState := tt.caminoState(ctrl)
			require.ErrorIs(t, actualCaminoState.writeDeposits(), tt.expectedErr)
			require.Equal(t, tt.expectedCaminoState(actualCaminoState), actualCaminoState)
		})
	}
}

func TestLoadDeposits(t *testing.T) {
	depositTxID1 := ids.GenerateTestID()
	depositTxID2 := ids.GenerateTestID()
	depositTxID3 := ids.GenerateTestID()
	deposit1 := &deposit.Deposit{Amount: 1, Duration: 101}
	deposit2 := &deposit.Deposit{Amount: 2, Duration: 101}
	deposit3 := &deposit.Deposit{Amount: 1, Duration: 103}

	tests := map[string]struct {
		caminoState         func(*gomock.Controller) *caminoState
		expectedCaminoState func(*caminoState) *caminoState
		expectedErr         error
	}{
		"OK": {
			caminoState: func(c *gomock.Controller) *caminoState {
				depositsIterator := database.NewMockIterator(c)
				depositsIterator.EXPECT().Next().Return(true).Times(3)
				depositsIterator.EXPECT().Key().Return(depositToKey(depositTxID1[:], deposit1))
				depositsIterator.EXPECT().Key().Return(depositToKey(depositTxID2[:], deposit2))
				depositsIterator.EXPECT().Key().Return(depositToKey(depositTxID3[:], deposit3))
				depositsIterator.EXPECT().Error().Return(nil)
				depositsIterator.EXPECT().Release()
				depositIDsByEndtimeDB := database.NewMockDatabase(c)
				depositIDsByEndtimeDB.EXPECT().NewIterator().Return(depositsIterator)
				return &caminoState{depositIDsByEndtimeDB: depositIDsByEndtimeDB}
			},
			expectedCaminoState: func(actualCaminoState *caminoState) *caminoState {
				nextTime := deposit1.EndTime()
				return &caminoState{
					depositIDsByEndtimeDB:    actualCaminoState.depositIDsByEndtimeDB,
					depositsNextToUnlockTime: &nextTime,
					depositsNextToUnlockIDs:  []ids.ID{depositTxID1, depositTxID2},
				}
			},
		},
		"OK: no deposits": {
			caminoState: func(c *gomock.Controller) *caminoState {
				depositsIterator := database.NewMockIterator(c)
				depositsIterator.EXPECT().Next().Return(false)
				depositsIterator.EXPECT().Error().Return(nil)
				depositsIterator.EXPECT().Release()
				depositIDsByEndtimeDB := database.NewMockDatabase(c)
				depositIDsByEndtimeDB.EXPECT().NewIterator().Return(depositsIterator)
				return &caminoState{depositIDsByEndtimeDB: depositIDsByEndtimeDB}
			},
			expectedCaminoState: func(actualCaminoState *caminoState) *caminoState {
				return &caminoState{
					depositIDsByEndtimeDB: actualCaminoState.depositIDsByEndtimeDB,
				}
			},
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			actualCaminoState := tt.caminoState(ctrl)
			require.ErrorIs(t, actualCaminoState.loadDeposits(), tt.expectedErr)
			require.Equal(t, tt.expectedCaminoState(actualCaminoState), actualCaminoState)
		})
	}
}

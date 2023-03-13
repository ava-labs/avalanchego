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
	"github.com/ava-labs/avalanchego/vms/platformvm/blocks"
	"github.com/ava-labs/avalanchego/vms/platformvm/deposit"
	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/require"
)

func TestGetDeposit(t *testing.T) {
	depositTxID := ids.GenerateTestID()
	deposit1 := &deposit.Deposit{Duration: 101}
	deposit2 := &deposit.Deposit{Duration: 102}
	depositBytes, err := blocks.GenesisCodec.Marshal(blocks.Version, deposit1)
	require.NoError(t, err)

	tests := map[string]struct {
		caminoState     func(*gomock.Controller) *caminoState
		depositTxID     ids.ID
		expectedDeposit *deposit.Deposit
		expectedErr     error
	}{
		"Fail: deposit in removed": {
			caminoState: func(c *gomock.Controller) *caminoState {
				return &caminoState{
					depositsDB:    database.NewMockDatabase(c),
					depositsCache: cache.NewMockCacher(c),
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
			depositTxID:     depositTxID,
			expectedDeposit: nil,
			expectedErr:     database.ErrNotFound,
		},
		"OK: deposit in modified": {
			caminoState: func(c *gomock.Controller) *caminoState {
				return &caminoState{
					depositsDB:    database.NewMockDatabase(c),
					depositsCache: cache.NewMockCacher(c),
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
		"OK: deposit in cache": {
			caminoState: func(c *gomock.Controller) *caminoState {
				cache := cache.NewMockCacher(c)
				cache.EXPECT().Get(depositTxID).Return(deposit1, true)
				return &caminoState{
					depositsDB:    database.NewMockDatabase(c),
					depositsCache: cache,
					caminoDiff:    &caminoDiff{},
				}
			},
			depositTxID:     depositTxID,
			expectedDeposit: deposit1,
		},
		"OK: deposit in db": {
			caminoState: func(c *gomock.Controller) *caminoState {
				cache := cache.NewMockCacher(c)
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
			depositTxID:     depositTxID,
			expectedDeposit: deposit1,
		},
		"Fail: db error": {
			caminoState: func(c *gomock.Controller) *caminoState {
				cache := cache.NewMockCacher(c)
				cache.EXPECT().Get(depositTxID).Return(nil, false)
				db := database.NewMockDatabase(c)
				db.EXPECT().Get(depositTxID[:]).Return(nil, database.ErrNotFound)
				return &caminoState{
					depositsDB:    db,
					depositsCache: cache,
					caminoDiff:    &caminoDiff{},
				}
			},
			depositTxID: depositTxID,
			expectedErr: database.ErrNotFound,
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()
			actualDeposit, err := tt.caminoState(ctrl).GetDeposit(tt.depositTxID)
			require.ErrorIs(t, err, tt.expectedErr)
			require.Equal(t, tt.expectedDeposit, actualDeposit)
		})
	}
}

func TestSetDeposit(t *testing.T) {
	depositTxID := ids.GenerateTestID()
	deposit1 := &deposit.Deposit{Duration: 101, Amount: 1}

	tests := map[string]struct {
		caminoState         func(*gomock.Controller) *caminoState
		depositTxID         ids.ID
		deposit             *deposit.Deposit
		expectedCaminoState func(cache.Cacher) *caminoState
	}{
		"OK": {
			caminoState: func(c *gomock.Controller) *caminoState {
				depositsCache := cache.NewMockCacher(c)
				depositsCache.EXPECT().Evict(depositTxID)
				return &caminoState{
					depositsCache: depositsCache,
					caminoDiff:    &caminoDiff{modifiedDeposits: map[ids.ID]*deposit.Deposit{}},
				}
			},
			depositTxID: depositTxID,
			deposit:     deposit1,
			expectedCaminoState: func(depositsCache cache.Cacher) *caminoState {
				return &caminoState{
					depositsCache: depositsCache,
					caminoDiff: &caminoDiff{
						modifiedDeposits: map[ids.ID]*deposit.Deposit{
							depositTxID: deposit1,
						},
					},
				}
			},
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()
			actualCaminoState := tt.caminoState(ctrl)
			actualCaminoState.SetDeposit(tt.depositTxID, tt.deposit)
			require.Equal(t, tt.expectedCaminoState(actualCaminoState.depositsCache), actualCaminoState)
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
		expectedCaminoState func(cache.Cacher) *caminoState
	}{
		"OK": {
			caminoState: func(c *gomock.Controller) *caminoState {
				depositsCache := cache.NewMockCacher(c)
				depositsCache.EXPECT().Evict(depositTxID)
				return &caminoState{
					depositsCache: depositsCache,
					caminoDiff:    &caminoDiff{removedDeposits: map[ids.ID]*deposit.Deposit{}},
				}
			},
			depositTxID: depositTxID,
			deposit:     deposit1,
			expectedCaminoState: func(depositsCache cache.Cacher) *caminoState {
				return &caminoState{
					depositsCache: depositsCache,
					caminoDiff: &caminoDiff{
						removedDeposits: map[ids.ID]*deposit.Deposit{
							depositTxID: deposit1,
						},
					},
				}
			},
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()
			actualCaminoState := tt.caminoState(ctrl)
			actualCaminoState.RemoveDeposit(tt.depositTxID, tt.deposit)
			require.Equal(t, tt.expectedCaminoState(actualCaminoState.depositsCache), actualCaminoState)
		})
	}
}

func TestGetNextToUnlockDepositTime(t *testing.T) {
	time1 := time.Unix(100, 0)

	tests := map[string]struct {
		caminoState            *caminoState
		expectedNextUnlockTime time.Time
		expectedErr            error
	}{
		"Fail: no deposits": {
			caminoState:            &caminoState{},
			expectedNextUnlockTime: time.Time{},
			expectedErr:            database.ErrNotFound,
		},
		"OK": {
			caminoState:            &caminoState{depositsNextToUnlockTime: &time1},
			expectedNextUnlockTime: time1,
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			nextUnlockTime, err := tt.caminoState.GetNextToUnlockDepositTime()
			require.ErrorIs(t, err, tt.expectedErr)
			require.Equal(t, tt.expectedNextUnlockTime, nextUnlockTime)
		})
	}
}

func TestGetNextToUnlockDepositIDsAndTime(t *testing.T) {
	depositTxID1 := ids.GenerateTestID()
	depositTxID2 := ids.GenerateTestID()
	time1 := time.Unix(100, 0)

	tests := map[string]struct {
		caminoState            *caminoState
		expectedNextUnlockTime time.Time
		expectedNextUnlockIDs  []ids.ID
		expectedErr            error
	}{
		"Fail: no deposits": {
			caminoState:            &caminoState{},
			expectedNextUnlockTime: time.Time{},
			expectedErr:            database.ErrNotFound,
		},
		"OK": {
			caminoState: &caminoState{
				depositsNextToUnlockTime: &time1,
				depositsNextToUnlockIDs:  []ids.ID{depositTxID1, depositTxID2},
			},
			expectedNextUnlockTime: time1,
			expectedNextUnlockIDs:  []ids.ID{depositTxID1, depositTxID2},
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			nextUnlockIDs, nextUnlockTime, err := tt.caminoState.GetNextToUnlockDepositIDsAndTime()
			require.ErrorIs(t, err, tt.expectedErr)
			require.Equal(t, tt.expectedNextUnlockTime, nextUnlockTime)
			require.Equal(t, tt.expectedNextUnlockIDs, nextUnlockIDs)

		})
	}
}

func TestWriteDeposits(t *testing.T) {
	testError := errors.New("test error")
	depositTxID2 := ids.GenerateTestID()
	depositTxID1 := ids.GenerateTestID()
	deposit1 := &deposit.Deposit{Duration: 101}
	deposit2 := &deposit.Deposit{Duration: 101}
	deposit1Bytes, err := blocks.GenesisCodec.Marshal(blocks.Version, deposit1)
	require.NoError(t, err)

	tests := map[string]struct {
		caminoState         func(*gomock.Controller) *caminoState
		expectedCaminoState func(database.Database, database.Database) *caminoState
		expectedErr         error
	}{
		"Fail: db errored on modifiedDeposits Put": {
			caminoState: func(c *gomock.Controller) *caminoState {
				depositsDB := database.NewMockDatabase(c)
				depositsDB.EXPECT().Put(depositTxID1[:], deposit1Bytes).Return(testError)
				return &caminoState{
					caminoDiff: &caminoDiff{
						modifiedDeposits: map[ids.ID]*deposit.Deposit{
							depositTxID1: deposit1,
						},
					},
					depositsDB: depositsDB,
				}
			},
			expectedCaminoState: func(depositsDB, depositIDsByEndtimeDB database.Database) *caminoState {
				return &caminoState{
					caminoDiff: &caminoDiff{
						modifiedDeposits: make(map[ids.ID]*deposit.Deposit),
					},
					depositsDB: depositsDB,
				}
			},
			expectedErr: testError,
		},
		"OK": {
			caminoState: func(c *gomock.Controller) *caminoState {
				depositsDB := database.NewMockDatabase(c)
				depositsDB.EXPECT().Put(depositTxID1[:], deposit1Bytes).Return(nil)
				depositsDB.EXPECT().Delete(depositTxID2[:]).Return(nil)
				depositIDsByEndtimeDB := database.NewMockDatabase(c)
				depositIDsByEndtimeDB.EXPECT().Put(depositToKey(depositTxID1[:], deposit1), nil).Return(nil)
				depositIDsByEndtimeDB.EXPECT().Delete(depositToKey(depositTxID2[:], deposit2)).Return(nil)
				return &caminoState{
					caminoDiff: &caminoDiff{
						modifiedDeposits: map[ids.ID]*deposit.Deposit{
							depositTxID1: deposit1,
						},
						removedDeposits: map[ids.ID]*deposit.Deposit{
							depositTxID2: deposit2,
						},
					},
					depositsDB:            depositsDB,
					depositIDsByEndtimeDB: depositIDsByEndtimeDB,
				}
			},
			expectedCaminoState: func(depositsDB, depositIDsByEndtimeDB database.Database) *caminoState {
				return &caminoState{
					caminoDiff: &caminoDiff{
						modifiedDeposits: make(map[ids.ID]*deposit.Deposit),
						removedDeposits:  make(map[ids.ID]*deposit.Deposit),
					},
					depositsDB:            depositsDB,
					depositIDsByEndtimeDB: depositIDsByEndtimeDB,
				}
			},
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()
			actualCaminoState := tt.caminoState(ctrl)
			require.ErrorIs(t, actualCaminoState.writeDeposits(), tt.expectedErr)
			expectedCaminoState := tt.expectedCaminoState(
				actualCaminoState.depositsDB,
				actualCaminoState.depositIDsByEndtimeDB,
			)
			require.Equal(t, expectedCaminoState, actualCaminoState)
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
		expectedCaminoState func(database.Database) *caminoState
		expectedErr         error
	}{
		"OK": {
			caminoState: func(c *gomock.Controller) *caminoState {
				depositsIterator := database.NewMockIterator(c)
				depositsIterator.EXPECT().Next().Return(true).Times(3)
				depositsIterator.EXPECT().Key().Return(depositToKey(depositTxID1[:], deposit1))
				depositsIterator.EXPECT().Key().Return(depositToKey(depositTxID2[:], deposit2))
				depositsIterator.EXPECT().Key().Return(depositToKey(depositTxID3[:], deposit3))
				depositsIterator.EXPECT().Release()
				depositIDsByEndtimeDB := database.NewMockDatabase(c)
				depositIDsByEndtimeDB.EXPECT().NewIterator().Return(depositsIterator)
				return &caminoState{depositIDsByEndtimeDB: depositIDsByEndtimeDB}
			},
			expectedCaminoState: func(depositIDsByEndtimeDB database.Database) *caminoState {
				nextTime := deposit1.EndTime()
				return &caminoState{
					depositIDsByEndtimeDB:    depositIDsByEndtimeDB,
					depositsNextToUnlockTime: &nextTime,
					depositsNextToUnlockIDs:  []ids.ID{depositTxID1, depositTxID2},
				}
			},
		},
		"OK: no deposits": {
			caminoState: func(c *gomock.Controller) *caminoState {
				depositsIterator := database.NewMockIterator(c)
				depositsIterator.EXPECT().Next().Return(false)
				depositsIterator.EXPECT().Release()
				depositIDsByEndtimeDB := database.NewMockDatabase(c)
				depositIDsByEndtimeDB.EXPECT().NewIterator().Return(depositsIterator)
				return &caminoState{depositIDsByEndtimeDB: depositIDsByEndtimeDB}
			},
			expectedCaminoState: func(depositIDsByEndtimeDB database.Database) *caminoState {
				return &caminoState{
					depositIDsByEndtimeDB: depositIDsByEndtimeDB,
				}
			},
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()
			actualCaminoState := tt.caminoState(ctrl)
			require.ErrorIs(t, actualCaminoState.loadDeposits(), tt.expectedErr)
			require.Equal(t, tt.expectedCaminoState(actualCaminoState.depositIDsByEndtimeDB), actualCaminoState)
		})
	}
}

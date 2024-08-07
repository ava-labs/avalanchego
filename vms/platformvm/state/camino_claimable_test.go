// Copyright (C) 2022-2024, Chain4Travel AG. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"errors"
	"testing"

	"github.com/ava-labs/avalanchego/cache"
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/vms/platformvm/blocks"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/require"
)

func TestGetClaimable(t *testing.T) {
	claimableOwnerID := ids.ID{1}
	claimable := &Claimable{Owner: &secp256k1fx.OutputOwners{Addrs: []ids.ShortID{}}}
	claimableBytes, err := blocks.GenesisCodec.Marshal(blocks.Version, claimable)
	require.NoError(t, err)
	testError := errors.New("test error")

	tests := map[string]struct {
		caminoState         func(*gomock.Controller) *caminoState
		claimableOwnerID    ids.ID
		expectedCaminoState func(*caminoState) *caminoState
		expectedClaimable   *Claimable
		expectedErr         error
	}{
		"Fail: claimable removed": {
			caminoState: func(c *gomock.Controller) *caminoState {
				return &caminoState{
					caminoDiff: &caminoDiff{
						modifiedClaimables: map[ids.ID]*Claimable{claimableOwnerID: nil},
					},
				}
			},
			claimableOwnerID: claimableOwnerID,
			expectedCaminoState: func(*caminoState) *caminoState {
				return &caminoState{
					caminoDiff: &caminoDiff{
						modifiedClaimables: map[ids.ID]*Claimable{claimableOwnerID: nil},
					},
				}
			},
			expectedErr: database.ErrNotFound,
		},
		"Fail: claimable in cache, but removed": {
			caminoState: func(c *gomock.Controller) *caminoState {
				cache := cache.NewMockCacher[ids.ID, *Claimable](c)
				cache.EXPECT().Get(claimableOwnerID).Return(nil, true)
				return &caminoState{
					claimablesCache: cache,
					caminoDiff:      &caminoDiff{},
				}
			},
			claimableOwnerID: claimableOwnerID,
			expectedCaminoState: func(actualCaminoState *caminoState) *caminoState {
				return &caminoState{
					claimablesCache: actualCaminoState.claimablesCache,
					caminoDiff:      &caminoDiff{},
				}
			},
			expectedErr: database.ErrNotFound,
		},
		"Fail: not found in db": {
			caminoState: func(c *gomock.Controller) *caminoState {
				cache := cache.NewMockCacher[ids.ID, *Claimable](c)
				cache.EXPECT().Get(claimableOwnerID).Return(nil, false)
				cache.EXPECT().Put(claimableOwnerID, nil)
				db := database.NewMockDatabase(c)
				db.EXPECT().Get(claimableOwnerID[:]).Return(nil, database.ErrNotFound)
				return &caminoState{
					claimablesDB:    db,
					claimablesCache: cache,
					caminoDiff:      &caminoDiff{},
				}
			},
			claimableOwnerID: claimableOwnerID,
			expectedCaminoState: func(actualCaminoState *caminoState) *caminoState {
				return &caminoState{
					claimablesDB:    actualCaminoState.claimablesDB,
					claimablesCache: actualCaminoState.claimablesCache,
					caminoDiff:      &caminoDiff{},
				}
			},
			expectedErr: database.ErrNotFound,
		},
		"OK: claimable added/modified": {
			caminoState: func(c *gomock.Controller) *caminoState {
				return &caminoState{
					caminoDiff: &caminoDiff{
						modifiedClaimables: map[ids.ID]*Claimable{claimableOwnerID: claimable},
					},
				}
			},
			claimableOwnerID: claimableOwnerID,
			expectedCaminoState: func(actualCaminoState *caminoState) *caminoState {
				return &caminoState{
					claimablesCache: actualCaminoState.claimablesCache,
					caminoDiff: &caminoDiff{
						modifiedClaimables: map[ids.ID]*Claimable{claimableOwnerID: claimable},
					},
				}
			},
			expectedClaimable: claimable,
		},
		"OK: claimable in cache": {
			caminoState: func(c *gomock.Controller) *caminoState {
				cache := cache.NewMockCacher[ids.ID, *Claimable](c)
				cache.EXPECT().Get(claimableOwnerID).Return(claimable, true)
				return &caminoState{
					claimablesCache: cache,
					caminoDiff:      &caminoDiff{},
				}
			},
			claimableOwnerID: claimableOwnerID,
			expectedCaminoState: func(actualCaminoState *caminoState) *caminoState {
				return &caminoState{
					claimablesCache: actualCaminoState.claimablesCache,
					caminoDiff:      &caminoDiff{},
				}
			},
			expectedClaimable: claimable,
		},
		"OK: claimable in db": {
			caminoState: func(c *gomock.Controller) *caminoState {
				cache := cache.NewMockCacher[ids.ID, *Claimable](c)
				cache.EXPECT().Get(claimableOwnerID).Return(nil, false)
				cache.EXPECT().Put(claimableOwnerID, claimable)
				db := database.NewMockDatabase(c)
				db.EXPECT().Get(claimableOwnerID[:]).Return(claimableBytes, nil)
				return &caminoState{
					claimablesDB:    db,
					claimablesCache: cache,
					caminoDiff:      &caminoDiff{},
				}
			},
			claimableOwnerID: claimableOwnerID,
			expectedCaminoState: func(actualCaminoState *caminoState) *caminoState {
				return &caminoState{
					claimablesDB:    actualCaminoState.claimablesDB,
					claimablesCache: actualCaminoState.claimablesCache,
					caminoDiff:      &caminoDiff{},
				}
			},
			expectedClaimable: claimable,
		},
		"Fail: db error": {
			caminoState: func(c *gomock.Controller) *caminoState {
				cache := cache.NewMockCacher[ids.ID, *Claimable](c)
				cache.EXPECT().Get(claimableOwnerID).Return(nil, false)
				db := database.NewMockDatabase(c)
				db.EXPECT().Get(claimableOwnerID[:]).Return(nil, testError)
				return &caminoState{
					claimablesDB:    db,
					claimablesCache: cache,
					caminoDiff:      &caminoDiff{},
				}
			},
			claimableOwnerID: claimableOwnerID,
			expectedCaminoState: func(actualCaminoState *caminoState) *caminoState {
				return &caminoState{
					claimablesDB:    actualCaminoState.claimablesDB,
					claimablesCache: actualCaminoState.claimablesCache,
					caminoDiff:      &caminoDiff{},
				}
			},
			expectedErr: testError,
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			actualCaminoState := tt.caminoState(ctrl)
			claimable, err := actualCaminoState.GetClaimable(tt.claimableOwnerID)
			require.ErrorIs(t, err, tt.expectedErr)
			require.Equal(t, tt.expectedClaimable, claimable)
			require.Equal(t, tt.expectedCaminoState(actualCaminoState), actualCaminoState)
		})
	}
}

func TestSetClaimable(t *testing.T) {
	ownerID := ids.GenerateTestID()
	claimable := &Claimable{
		Owner:                &secp256k1fx.OutputOwners{},
		ValidatorReward:      1,
		ExpiredDepositReward: 1,
	}

	tests := map[string]struct {
		caminoState         func(*gomock.Controller) *caminoState
		claimableOwnerID    ids.ID
		claimable           *Claimable
		expectedCaminoState func(*caminoState) *caminoState
	}{
		"OK": {
			caminoState: func(c *gomock.Controller) *caminoState {
				cache := cache.NewMockCacher[ids.ID, *Claimable](c)
				cache.EXPECT().Evict(ownerID)
				return &caminoState{
					claimablesCache: cache,
					caminoDiff:      &caminoDiff{modifiedClaimables: map[ids.ID]*Claimable{}},
				}
			},
			claimableOwnerID: ownerID,
			claimable:        claimable,
			expectedCaminoState: func(actualCaminoState *caminoState) *caminoState {
				return &caminoState{
					claimablesCache: actualCaminoState.claimablesCache,
					caminoDiff: &caminoDiff{
						modifiedClaimables: map[ids.ID]*Claimable{ownerID: claimable},
					},
				}
			},
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			caminoState := tt.caminoState(ctrl)
			caminoState.SetClaimable(tt.claimableOwnerID, tt.claimable)
			require.Equal(t, tt.expectedCaminoState(caminoState), caminoState)
		})
	}
}

func TestSetNotDistributedValidatorReward(t *testing.T) {
	tests := map[string]struct {
		caminoState         *caminoState
		reward              uint64
		expectedCaminoState func(uint64) *caminoState
	}{
		"OK": {
			caminoState: &caminoState{
				caminoDiff: &caminoDiff{},
			},
			expectedCaminoState: func(reward uint64) *caminoState {
				return &caminoState{
					caminoDiff: &caminoDiff{
						modifiedNotDistributedValidatorReward: &reward,
					},
				}
			},
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			tt.caminoState.SetNotDistributedValidatorReward(tt.reward)
			require.Equal(t, tt.expectedCaminoState(tt.reward), tt.caminoState)
		})
	}
}

func TestGetNotDistributedValidatorReward(t *testing.T) {
	tests := map[string]struct {
		caminoState                           func(c *gomock.Controller) *caminoState
		expectedCaminoState                   func(*caminoState) *caminoState
		expectedNotDistributedValidatorReward uint64
	}{
		"OK": {
			caminoState: func(c *gomock.Controller) *caminoState {
				return &caminoState{
					notDistributedValidatorReward: 11,
					caminoDiff:                    &caminoDiff{},
				}
			},
			expectedCaminoState: func(*caminoState) *caminoState {
				return &caminoState{
					notDistributedValidatorReward: 11,
					caminoDiff:                    &caminoDiff{},
				}
			},
			expectedNotDistributedValidatorReward: 11,
		},
		"OK: modified": {
			caminoState: func(c *gomock.Controller) *caminoState {
				modifiedReward := uint64(15)
				return &caminoState{
					notDistributedValidatorReward: 11,
					caminoDiff: &caminoDiff{
						modifiedNotDistributedValidatorReward: &modifiedReward,
					},
				}
			},
			expectedCaminoState: func(*caminoState) *caminoState {
				modifiedReward := uint64(15)
				return &caminoState{
					notDistributedValidatorReward: 11,
					caminoDiff: &caminoDiff{
						modifiedNotDistributedValidatorReward: &modifiedReward,
					},
				}
			},
			expectedNotDistributedValidatorReward: 15,
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			actualCaminoState := tt.caminoState(ctrl)
			notDistributedValidatorReward, err := actualCaminoState.GetNotDistributedValidatorReward()
			require.NoError(t, err)
			require.Equal(t, tt.expectedNotDistributedValidatorReward, notDistributedValidatorReward)
			require.Equal(t, tt.expectedCaminoState(actualCaminoState), actualCaminoState)
		})
	}
}

func TestWriteClaimableAndValidatorRewards(t *testing.T) {
	testError := errors.New("test error")
	claimableOwnerID1 := ids.ID{1}
	claimableOwnerID2 := ids.ID{2}
	claimable1 := &Claimable{Owner: &secp256k1fx.OutputOwners{}, ValidatorReward: 1, ExpiredDepositReward: 2}
	claimableBytes1, err := blocks.GenesisCodec.Marshal(blocks.Version, claimable1)
	require.NoError(t, err)

	tests := map[string]struct {
		caminoState         func(*gomock.Controller) *caminoState
		expectedCaminoState func(*caminoState) *caminoState
		expectedErr         error
	}{
		"Fail: db errored on modifiedClaimables Put": {
			caminoState: func(c *gomock.Controller) *caminoState {
				claimablesDB := database.NewMockDatabase(c)
				claimablesDB.EXPECT().Put(claimableOwnerID1[:], claimableBytes1).Return(testError)
				return &caminoState{
					claimablesDB: claimablesDB,
					caminoDiff: &caminoDiff{
						modifiedClaimables: map[ids.ID]*Claimable{
							claimableOwnerID1: claimable1,
						},
					},
				}
			},
			expectedCaminoState: func(actualState *caminoState) *caminoState {
				return &caminoState{
					claimablesDB: actualState.claimablesDB,
					caminoDiff: &caminoDiff{
						modifiedClaimables: map[ids.ID]*Claimable{},
					},
				}
			},
			expectedErr: testError,
		},
		"Fail: db errored on modifiedClaimables Delete": {
			caminoState: func(c *gomock.Controller) *caminoState {
				claimablesDB := database.NewMockDatabase(c)
				claimablesDB.EXPECT().Delete(claimableOwnerID1[:]).Return(testError)
				return &caminoState{
					caminoDiff: &caminoDiff{
						modifiedClaimables: map[ids.ID]*Claimable{
							claimableOwnerID1: nil,
						},
					},
					claimablesDB: claimablesDB,
				}
			},
			expectedCaminoState: func(actualState *caminoState) *caminoState {
				return &caminoState{
					caminoDiff: &caminoDiff{
						modifiedClaimables: map[ids.ID]*Claimable{},
					},
					claimablesDB: actualState.claimablesDB,
				}
			},
			expectedErr: testError,
		},
		"OK: modifiedNotDistributedValidatorReward is nil": {
			caminoState: func(c *gomock.Controller) *caminoState {
				return &caminoState{caminoDiff: &caminoDiff{}}
			},
			expectedCaminoState: func(actualState *caminoState) *caminoState {
				return &caminoState{caminoDiff: &caminoDiff{}}
			},
		},
		"OK": {
			caminoState: func(c *gomock.Controller) *caminoState {
				notDistributedReward := uint64(11)

				caminoDB := database.NewMockDatabase(c)
				caminoDB.EXPECT().Put(
					notDistributedValidatorRewardKey,
					database.PackUInt64(notDistributedReward),
				).Return(nil)

				claimablesDB := database.NewMockDatabase(c)
				claimablesDB.EXPECT().Put(claimableOwnerID1[:], claimableBytes1).Return(nil)
				claimablesDB.EXPECT().Delete(claimableOwnerID2[:]).Return(nil)

				return &caminoState{
					caminoDB:     caminoDB,
					claimablesDB: claimablesDB,
					caminoDiff: &caminoDiff{
						modifiedNotDistributedValidatorReward: &notDistributedReward,
						modifiedClaimables: map[ids.ID]*Claimable{
							claimableOwnerID1: claimable1,
							claimableOwnerID2: nil,
						},
					},
				}
			},
			expectedCaminoState: func(actualState *caminoState) *caminoState {
				return &caminoState{
					caminoDB:                      actualState.caminoDB,
					claimablesDB:                  actualState.claimablesDB,
					notDistributedValidatorReward: 11,
					caminoDiff: &caminoDiff{
						modifiedClaimables: map[ids.ID]*Claimable{},
					},
				}
			},
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			actualCaminoState := tt.caminoState(ctrl)
			require.ErrorIs(t, actualCaminoState.writeClaimableAndValidatorRewards(), tt.expectedErr)
			require.Equal(t, tt.expectedCaminoState(actualCaminoState), actualCaminoState)
		})
	}
}

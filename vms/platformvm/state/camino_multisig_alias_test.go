// Copyright (C) 2023, Chain4Travel AG. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"errors"
	"testing"

	"github.com/ava-labs/avalanchego/cache"
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/vms/components/multisig"
	"github.com/ava-labs/avalanchego/vms/platformvm/blocks"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/require"
)

func TestGetMultisigAlias(t *testing.T) {
	multisigAlias := &multisig.AliasWithNonce{
		Alias: multisig.Alias{
			ID:     ids.ShortID{1},
			Owners: &secp256k1fx.OutputOwners{Addrs: []ids.ShortID{}},
			Memo:   []byte("multisigAlias memo"),
		},
	}
	multisigAliasBytes, err := blocks.GenesisCodec.Marshal(blocks.Version, &msigAlias{
		Owners: multisigAlias.Owners,
		Memo:   multisigAlias.Memo,
	})
	require.NoError(t, err)
	testError := errors.New("test error")

	tests := map[string]struct {
		caminoState           func(*gomock.Controller) *caminoState
		multisigAliasID       ids.ShortID
		expectedCaminoState   func(*caminoState) *caminoState
		expectedMultisigAlias *multisig.AliasWithNonce
		expectedErr           error
	}{
		"Fail: msig alias removed": {
			caminoState: func(c *gomock.Controller) *caminoState {
				return &caminoState{
					caminoDiff: &caminoDiff{
						modifiedMultisigAliases: map[ids.ShortID]*multisig.AliasWithNonce{multisigAlias.ID: nil},
					},
				}
			},
			multisigAliasID: multisigAlias.ID,
			expectedCaminoState: func(actualCaminoState *caminoState) *caminoState {
				return &caminoState{
					caminoDiff: &caminoDiff{
						modifiedMultisigAliases: map[ids.ShortID]*multisig.AliasWithNonce{multisigAlias.ID: nil},
					},
				}
			},
			expectedErr: database.ErrNotFound,
		},
		"Fail: msig alias in cache, but removed": {
			caminoState: func(c *gomock.Controller) *caminoState {
				cache := cache.NewMockCacher[ids.ShortID, *multisig.AliasWithNonce](c)
				cache.EXPECT().Get(multisigAlias.ID).Return(nil, true)
				return &caminoState{
					multisigAliasesCache: cache,
					caminoDiff:           &caminoDiff{},
				}
			},
			multisigAliasID: multisigAlias.ID,
			expectedCaminoState: func(actualCaminoState *caminoState) *caminoState {
				return &caminoState{
					multisigAliasesCache: actualCaminoState.multisigAliasesCache,
					caminoDiff:           &caminoDiff{},
				}
			},
			expectedErr: database.ErrNotFound,
		},
		"Fail: not found in db": {
			caminoState: func(c *gomock.Controller) *caminoState {
				cache := cache.NewMockCacher[ids.ShortID, *multisig.AliasWithNonce](c)
				cache.EXPECT().Get(multisigAlias.ID).Return(nil, false)
				cache.EXPECT().Put(multisigAlias.ID, nil)
				db := database.NewMockDatabase(c)
				db.EXPECT().Get(multisigAlias.ID[:]).Return(nil, database.ErrNotFound)
				return &caminoState{
					multisigAliasesDB:    db,
					multisigAliasesCache: cache,
					caminoDiff:           &caminoDiff{},
				}
			},
			multisigAliasID: multisigAlias.ID,
			expectedCaminoState: func(actualCaminoState *caminoState) *caminoState {
				return &caminoState{
					multisigAliasesDB:    actualCaminoState.multisigAliasesDB,
					multisigAliasesCache: actualCaminoState.multisigAliasesCache,
					caminoDiff:           &caminoDiff{},
				}
			},
			expectedErr: database.ErrNotFound,
		},
		"OK: msig alias added or modified": {
			caminoState: func(c *gomock.Controller) *caminoState {
				return &caminoState{
					caminoDiff: &caminoDiff{
						modifiedMultisigAliases: map[ids.ShortID]*multisig.AliasWithNonce{multisigAlias.ID: multisigAlias},
					},
				}
			},
			multisigAliasID: multisigAlias.ID,
			expectedCaminoState: func(actualCaminoState *caminoState) *caminoState {
				return &caminoState{
					caminoDiff: &caminoDiff{
						modifiedMultisigAliases: map[ids.ShortID]*multisig.AliasWithNonce{multisigAlias.ID: multisigAlias},
					},
				}
			},
			expectedMultisigAlias: multisigAlias,
		},
		"OK: msig alias in cache": {
			caminoState: func(c *gomock.Controller) *caminoState {
				cache := cache.NewMockCacher[ids.ShortID, *multisig.AliasWithNonce](c)
				cache.EXPECT().Get(multisigAlias.ID).Return(multisigAlias, true)
				return &caminoState{
					multisigAliasesCache: cache,
					caminoDiff:           &caminoDiff{},
				}
			},
			multisigAliasID: multisigAlias.ID,
			expectedCaminoState: func(actualCaminoState *caminoState) *caminoState {
				return &caminoState{
					multisigAliasesCache: actualCaminoState.multisigAliasesCache,
					caminoDiff:           &caminoDiff{},
				}
			},
			expectedMultisigAlias: multisigAlias,
		},
		"OK: msig alias in db": {
			caminoState: func(c *gomock.Controller) *caminoState {
				cache := cache.NewMockCacher[ids.ShortID, *multisig.AliasWithNonce](c)
				cache.EXPECT().Get(multisigAlias.ID).Return(nil, false)
				cache.EXPECT().Put(multisigAlias.ID, multisigAlias)
				db := database.NewMockDatabase(c)
				db.EXPECT().Get(multisigAlias.ID[:]).Return(multisigAliasBytes, nil)
				return &caminoState{
					multisigAliasesDB:    db,
					multisigAliasesCache: cache,
					caminoDiff:           &caminoDiff{},
				}
			},
			multisigAliasID: multisigAlias.ID,
			expectedCaminoState: func(actualCaminoState *caminoState) *caminoState {
				return &caminoState{
					multisigAliasesDB:    actualCaminoState.multisigAliasesDB,
					multisigAliasesCache: actualCaminoState.multisigAliasesCache,
					caminoDiff:           &caminoDiff{},
				}
			},
			expectedMultisigAlias: multisigAlias,
		},
		"Fail: db error": {
			caminoState: func(c *gomock.Controller) *caminoState {
				cache := cache.NewMockCacher[ids.ShortID, *multisig.AliasWithNonce](c)
				cache.EXPECT().Get(multisigAlias.ID).Return(nil, false)
				db := database.NewMockDatabase(c)
				db.EXPECT().Get(multisigAlias.ID[:]).Return(nil, testError)
				return &caminoState{
					multisigAliasesDB:    db,
					multisigAliasesCache: cache,
					caminoDiff:           &caminoDiff{},
				}
			},
			multisigAliasID: multisigAlias.ID,
			expectedCaminoState: func(actualCaminoState *caminoState) *caminoState {
				return &caminoState{
					multisigAliasesDB:    actualCaminoState.multisigAliasesDB,
					multisigAliasesCache: actualCaminoState.multisigAliasesCache,
					caminoDiff:           &caminoDiff{},
				}
			},
			expectedErr: testError,
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			caminoState := tt.caminoState(ctrl)
			multisigAlias, err := caminoState.GetMultisigAlias(tt.multisigAliasID)
			require.ErrorIs(t, err, tt.expectedErr)
			require.Equal(t, tt.expectedMultisigAlias, multisigAlias)
			require.Equal(t, tt.expectedCaminoState(caminoState), caminoState)
		})
	}
}

func TestSetMultisigAlias(t *testing.T) {
	multisigAlias := &multisig.AliasWithNonce{Alias: multisig.Alias{ID: ids.ShortID{1}}}

	tests := map[string]struct {
		caminoState         func(*gomock.Controller) *caminoState
		multisigAlias       *multisig.AliasWithNonce
		expectedCaminoState func(*caminoState) *caminoState
	}{
		"OK": {
			caminoState: func(c *gomock.Controller) *caminoState {
				cache := cache.NewMockCacher[ids.ShortID, *multisig.AliasWithNonce](c)
				cache.EXPECT().Evict(multisigAlias.ID)
				return &caminoState{
					multisigAliasesCache: cache,
					caminoDiff:           &caminoDiff{modifiedMultisigAliases: map[ids.ShortID]*multisig.AliasWithNonce{}},
				}
			},
			multisigAlias: multisigAlias,
			expectedCaminoState: func(actualCaminoState *caminoState) *caminoState {
				return &caminoState{
					multisigAliasesCache: actualCaminoState.multisigAliasesCache,
					caminoDiff: &caminoDiff{
						modifiedMultisigAliases: map[ids.ShortID]*multisig.AliasWithNonce{multisigAlias.ID: multisigAlias},
					},
				}
			},
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			caminoState := tt.caminoState(ctrl)
			caminoState.SetMultisigAlias(tt.multisigAlias)
			require.Equal(t, tt.expectedCaminoState(caminoState), caminoState)
		})
	}
}

func TestWriteMultisigAliases(t *testing.T) {
	multisigAlias1 := &multisig.AliasWithNonce{Alias: multisig.Alias{ID: ids.ShortID{1}, Owners: &secp256k1fx.OutputOwners{}}}
	multisigAlias2 := &multisig.AliasWithNonce{Alias: multisig.Alias{ID: ids.ShortID{2}, Owners: &secp256k1fx.OutputOwners{}}}
	multisigAliasBytes1, err := blocks.GenesisCodec.Marshal(blocks.Version, &msigAlias{Owners: multisigAlias1.Owners})
	require.NoError(t, err)
	testError := errors.New("test error")

	tests := map[string]struct {
		caminoState         func(*gomock.Controller) *caminoState
		expectedCaminoState func(*caminoState) *caminoState
		expectedErr         error
	}{
		"Fail: db errored on modifiedMultisigAliases Put": {
			caminoState: func(c *gomock.Controller) *caminoState {
				multisigAliasesDB := database.NewMockDatabase(c)
				multisigAliasesDB.EXPECT().Put(multisigAlias1.ID[:], multisigAliasBytes1).Return(testError)
				return &caminoState{
					multisigAliasesDB: multisigAliasesDB,
					caminoDiff: &caminoDiff{
						modifiedMultisigAliases: map[ids.ShortID]*multisig.AliasWithNonce{
							multisigAlias1.ID: multisigAlias1,
						},
					},
				}
			},
			expectedCaminoState: func(actualState *caminoState) *caminoState {
				return &caminoState{
					multisigAliasesDB: actualState.multisigAliasesDB,
					caminoDiff: &caminoDiff{
						modifiedMultisigAliases: map[ids.ShortID]*multisig.AliasWithNonce{},
					},
				}
			},
			expectedErr: testError,
		},
		"Fail: db errored on modifiedMultisigAliases Delete": {
			caminoState: func(c *gomock.Controller) *caminoState {
				multisigAliasesDB := database.NewMockDatabase(c)
				multisigAliasesDB.EXPECT().Delete(multisigAlias1.ID[:]).Return(testError)
				return &caminoState{
					caminoDiff: &caminoDiff{
						modifiedMultisigAliases: map[ids.ShortID]*multisig.AliasWithNonce{
							multisigAlias1.ID: nil,
						},
					},
					multisigAliasesDB: multisigAliasesDB,
				}
			},
			expectedCaminoState: func(actualState *caminoState) *caminoState {
				return &caminoState{
					caminoDiff: &caminoDiff{
						modifiedMultisigAliases: map[ids.ShortID]*multisig.AliasWithNonce{},
					},
					multisigAliasesDB: actualState.multisigAliasesDB,
				}
			},
			expectedErr: testError,
		},
		"OK": {
			caminoState: func(c *gomock.Controller) *caminoState {
				multisigAliasesDB := database.NewMockDatabase(c)
				multisigAliasesDB.EXPECT().Put(multisigAlias1.ID[:], multisigAliasBytes1).Return(nil)
				multisigAliasesDB.EXPECT().Delete(multisigAlias2.ID[:]).Return(nil)
				return &caminoState{
					multisigAliasesDB: multisigAliasesDB,
					caminoDiff: &caminoDiff{
						modifiedMultisigAliases: map[ids.ShortID]*multisig.AliasWithNonce{
							multisigAlias1.ID: multisigAlias1,
							multisigAlias2.ID: nil,
						},
					},
				}
			},
			expectedCaminoState: func(actualState *caminoState) *caminoState {
				return &caminoState{
					multisigAliasesDB: actualState.multisigAliasesDB,
					caminoDiff: &caminoDiff{
						modifiedMultisigAliases: map[ids.ShortID]*multisig.AliasWithNonce{},
					},
				}
			},
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			actualCaminoState := tt.caminoState(ctrl)
			require.ErrorIs(t, actualCaminoState.writeMultisigAliases(), tt.expectedErr)
			require.Equal(t, tt.expectedCaminoState(actualCaminoState), actualCaminoState)
		})
	}
}

// Copyright (C) 2022-2024, Chain4Travel AG. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"encoding/binary"
	"errors"
	"testing"

	"github.com/ava-labs/avalanchego/cache"
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	as "github.com/ava-labs/avalanchego/vms/platformvm/addrstate"
	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/require"
)

func TestGetAddressStates(t *testing.T) {
	address := ids.ShortID{1}
	addressStates := as.AddressState(12345)
	addressStatesBytes := make([]byte, 8)
	binary.LittleEndian.PutUint64(addressStatesBytes, uint64(addressStates))
	testError := errors.New("test error")

	tests := map[string]struct {
		caminoState           func(*gomock.Controller) *caminoState
		address               ids.ShortID
		expectedCaminoState   func(*caminoState) *caminoState
		expectedAddressStates as.AddressState
		expectedErr           error
	}{
		"OK: address states added or modified": {
			caminoState: func(c *gomock.Controller) *caminoState {
				return &caminoState{
					caminoDiff: &caminoDiff{
						modifiedAddressStates: map[ids.ShortID]as.AddressState{address: addressStates},
					},
				}
			},
			address: address,
			expectedCaminoState: func(actualCaminoState *caminoState) *caminoState {
				return &caminoState{
					addressStateCache: actualCaminoState.addressStateCache,
					caminoDiff: &caminoDiff{
						modifiedAddressStates: map[ids.ShortID]as.AddressState{address: addressStates},
					},
				}
			},
			expectedAddressStates: addressStates,
		},
		"OK: address states in cache": {
			caminoState: func(c *gomock.Controller) *caminoState {
				cache := cache.NewMockCacher[ids.ShortID, as.AddressState](c)
				cache.EXPECT().Get(address).Return(addressStates, true)
				return &caminoState{
					addressStateCache: cache,
					caminoDiff:        &caminoDiff{},
				}
			},
			address: address,
			expectedCaminoState: func(actualCaminoState *caminoState) *caminoState {
				return &caminoState{
					addressStateCache: actualCaminoState.addressStateCache,
					caminoDiff:        &caminoDiff{},
				}
			},
			expectedAddressStates: addressStates,
		},
		"OK: address states in db": {
			caminoState: func(c *gomock.Controller) *caminoState {
				cache := cache.NewMockCacher[ids.ShortID, as.AddressState](c)
				cache.EXPECT().Get(address).Return(as.AddressStateEmpty, false)
				cache.EXPECT().Put(address, addressStates)
				db := database.NewMockDatabase(c)
				db.EXPECT().Get(address[:]).Return(addressStatesBytes, nil)
				return &caminoState{
					addressStateDB:    db,
					addressStateCache: cache,
					caminoDiff:        &caminoDiff{},
				}
			},
			address: address,
			expectedCaminoState: func(actualCaminoState *caminoState) *caminoState {
				return &caminoState{
					addressStateDB:    actualCaminoState.addressStateDB,
					addressStateCache: actualCaminoState.addressStateCache,
					caminoDiff:        &caminoDiff{},
				}
			},
			expectedAddressStates: addressStates,
		},
		"OK: not found in db": {
			caminoState: func(c *gomock.Controller) *caminoState {
				cache := cache.NewMockCacher[ids.ShortID, as.AddressState](c)
				cache.EXPECT().Get(address).Return(as.AddressStateEmpty, false)
				cache.EXPECT().Put(address, as.AddressStateEmpty)
				db := database.NewMockDatabase(c)
				db.EXPECT().Get(address[:]).Return(nil, database.ErrNotFound)
				return &caminoState{
					addressStateDB:    db,
					addressStateCache: cache,
					caminoDiff:        &caminoDiff{},
				}
			},
			address: address,
			expectedCaminoState: func(actualCaminoState *caminoState) *caminoState {
				return &caminoState{
					addressStateDB:    actualCaminoState.addressStateDB,
					addressStateCache: actualCaminoState.addressStateCache,
					caminoDiff:        &caminoDiff{},
				}
			},
		},
		"Fail: db error": {
			caminoState: func(c *gomock.Controller) *caminoState {
				cache := cache.NewMockCacher[ids.ShortID, as.AddressState](c)
				cache.EXPECT().Get(address).Return(as.AddressStateEmpty, false)
				db := database.NewMockDatabase(c)
				db.EXPECT().Get(address[:]).Return(nil, testError)
				return &caminoState{
					addressStateDB:    db,
					addressStateCache: cache,
					caminoDiff:        &caminoDiff{},
				}
			},
			address: address,
			expectedCaminoState: func(actualCaminoState *caminoState) *caminoState {
				return &caminoState{
					addressStateDB:    actualCaminoState.addressStateDB,
					addressStateCache: actualCaminoState.addressStateCache,
					caminoDiff:        &caminoDiff{},
				}
			},
			expectedErr: testError,
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			actualCaminoState := tt.caminoState(ctrl)
			addressStates, err := actualCaminoState.GetAddressStates(tt.address)
			require.ErrorIs(t, err, tt.expectedErr)
			require.Equal(t, tt.expectedAddressStates, addressStates)
			require.Equal(t, tt.expectedCaminoState(actualCaminoState), actualCaminoState)
		})
	}
}

func TestSetAddressStates(t *testing.T) {
	address := ids.ShortID{1}
	addressStates := as.AddressState(12345)

	tests := map[string]struct {
		caminoState         func(*gomock.Controller) *caminoState
		address             ids.ShortID
		addressStates       as.AddressState
		expectedCaminoState func(*caminoState) *caminoState
	}{
		"OK": {
			caminoState: func(c *gomock.Controller) *caminoState {
				cache := cache.NewMockCacher[ids.ShortID, as.AddressState](c)
				cache.EXPECT().Evict(address)
				return &caminoState{
					addressStateCache: cache,
					caminoDiff:        &caminoDiff{modifiedAddressStates: map[ids.ShortID]as.AddressState{}},
				}
			},
			address:       address,
			addressStates: addressStates,
			expectedCaminoState: func(actualCaminoState *caminoState) *caminoState {
				return &caminoState{
					addressStateCache: actualCaminoState.addressStateCache,
					caminoDiff: &caminoDiff{
						modifiedAddressStates: map[ids.ShortID]as.AddressState{address: addressStates},
					},
				}
			},
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			caminoState := tt.caminoState(ctrl)
			caminoState.SetAddressStates(tt.address, tt.addressStates)
			require.Equal(t, tt.expectedCaminoState(caminoState), caminoState)
		})
	}
}

func TestWriteAddressStates(t *testing.T) {
	testError := errors.New("test error")
	address1 := ids.ShortID{1}
	address2 := ids.ShortID{2}
	addressStates1 := as.AddressState(12345)
	addressStatesBytes1 := make([]byte, 8)
	binary.LittleEndian.PutUint64(addressStatesBytes1, uint64(addressStates1))

	tests := map[string]struct {
		caminoState         func(*gomock.Controller) *caminoState
		expectedCaminoState func(*caminoState) *caminoState
		expectedErr         error
	}{
		"Fail: db errored on modifiedAddressStates Put": {
			caminoState: func(c *gomock.Controller) *caminoState {
				addressStateDB := database.NewMockDatabase(c)
				addressStateDB.EXPECT().Put(address1[:], addressStatesBytes1).Return(testError)
				return &caminoState{
					addressStateDB: addressStateDB,
					caminoDiff: &caminoDiff{
						modifiedAddressStates: map[ids.ShortID]as.AddressState{
							address1: addressStates1,
						},
					},
				}
			},
			expectedCaminoState: func(actualState *caminoState) *caminoState {
				return &caminoState{
					addressStateDB: actualState.addressStateDB,
					caminoDiff: &caminoDiff{
						modifiedAddressStates: map[ids.ShortID]as.AddressState{},
					},
				}
			},
			expectedErr: testError,
		},
		"Fail: db errored on modifiedAddressStates Delete": {
			caminoState: func(c *gomock.Controller) *caminoState {
				addressStateDB := database.NewMockDatabase(c)
				addressStateDB.EXPECT().Delete(address1[:]).Return(testError)
				return &caminoState{
					caminoDiff: &caminoDiff{
						modifiedAddressStates: map[ids.ShortID]as.AddressState{
							address1: 0,
						},
					},
					addressStateDB: addressStateDB,
				}
			},
			expectedCaminoState: func(actualState *caminoState) *caminoState {
				return &caminoState{
					caminoDiff: &caminoDiff{
						modifiedAddressStates: map[ids.ShortID]as.AddressState{},
					},
					addressStateDB: actualState.addressStateDB,
				}
			},
			expectedErr: testError,
		},
		"OK": {
			caminoState: func(c *gomock.Controller) *caminoState {
				addressStateDB := database.NewMockDatabase(c)
				addressStateDB.EXPECT().Put(address1[:], addressStatesBytes1).Return(nil)
				addressStateDB.EXPECT().Delete(address2[:]).Return(nil)
				return &caminoState{
					addressStateDB: addressStateDB,
					caminoDiff: &caminoDiff{
						modifiedAddressStates: map[ids.ShortID]as.AddressState{
							address1: addressStates1,
							address2: 0,
						},
					},
				}
			},
			expectedCaminoState: func(actualState *caminoState) *caminoState {
				return &caminoState{
					addressStateDB: actualState.addressStateDB,
					caminoDiff: &caminoDiff{
						modifiedAddressStates: map[ids.ShortID]as.AddressState{},
					},
				}
			},
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			actualCaminoState := tt.caminoState(ctrl)
			require.ErrorIs(t, actualCaminoState.writeAddressStates(), tt.expectedErr)
			require.Equal(t, tt.expectedCaminoState(actualCaminoState), actualCaminoState)
		})
	}
}

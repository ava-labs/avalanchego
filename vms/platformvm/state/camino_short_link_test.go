// Copyright (C) 2022-2024, Chain4Travel AG. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"errors"
	"testing"

	"github.com/ava-labs/avalanchego/cache"
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/require"
)

func TestGetShortIDLink(t *testing.T) {
	shortID1 := ids.ShortID{1}
	shortID2 := ids.ShortID{2}
	linkKey := toShortLinkKey(shortID1, ShortLinkKeyRegisterNode)
	testError := errors.New("test error")

	tests := map[string]struct {
		caminoState           func(*gomock.Controller) *caminoState
		shortID               ids.ShortID
		shortLinkKey          ShortLinkKey
		expectedCaminoState   func(*caminoState) *caminoState
		expectedLinkedShortID ids.ShortID
		expectedErr           error
	}{
		"Fail: shortID link removed": {
			caminoState: func(c *gomock.Controller) *caminoState {
				return &caminoState{
					caminoDiff: &caminoDiff{
						modifiedShortLinks: map[ids.ID]*ids.ShortID{linkKey: nil},
					},
				}
			},
			shortID:      shortID1,
			shortLinkKey: ShortLinkKeyRegisterNode,
			expectedCaminoState: func(*caminoState) *caminoState {
				return &caminoState{
					caminoDiff: &caminoDiff{
						modifiedShortLinks: map[ids.ID]*ids.ShortID{linkKey: nil},
					},
				}
			},
			expectedErr: database.ErrNotFound,
		},
		"Fail: shortID link in cache, but removed": {
			caminoState: func(c *gomock.Controller) *caminoState {
				cache := cache.NewMockCacher[ids.ID, *ids.ShortID](c)
				cache.EXPECT().Get(linkKey).Return(nil, true)
				return &caminoState{
					shortLinksCache: cache,
					caminoDiff:      &caminoDiff{},
				}
			},
			shortID:      shortID1,
			shortLinkKey: ShortLinkKeyRegisterNode,
			expectedCaminoState: func(actualCaminoState *caminoState) *caminoState {
				return &caminoState{
					shortLinksCache: actualCaminoState.shortLinksCache,
					caminoDiff:      &caminoDiff{},
				}
			},
			expectedErr: database.ErrNotFound,
		},
		"Fail: not found in db": {
			caminoState: func(c *gomock.Controller) *caminoState {
				cache := cache.NewMockCacher[ids.ID, *ids.ShortID](c)
				cache.EXPECT().Get(linkKey).Return(nil, false)
				cache.EXPECT().Put(linkKey, nil)
				db := database.NewMockDatabase(c)
				db.EXPECT().Get(linkKey[:]).Return(nil, database.ErrNotFound)
				return &caminoState{
					shortLinksDB:    db,
					shortLinksCache: cache,
					caminoDiff:      &caminoDiff{},
				}
			},
			shortID:      shortID1,
			shortLinkKey: ShortLinkKeyRegisterNode,
			expectedCaminoState: func(actualCaminoState *caminoState) *caminoState {
				return &caminoState{
					shortLinksDB:    actualCaminoState.shortLinksDB,
					shortLinksCache: actualCaminoState.shortLinksCache,
					caminoDiff:      &caminoDiff{},
				}
			},
			expectedErr: database.ErrNotFound,
		},
		"OK: shortID link added or modified": {
			caminoState: func(c *gomock.Controller) *caminoState {
				return &caminoState{
					caminoDiff: &caminoDiff{
						modifiedShortLinks: map[ids.ID]*ids.ShortID{linkKey: &shortID2},
					},
				}
			},
			shortID:      shortID1,
			shortLinkKey: ShortLinkKeyRegisterNode,
			expectedCaminoState: func(actualCaminoState *caminoState) *caminoState {
				return &caminoState{
					caminoDiff: &caminoDiff{
						modifiedShortLinks: map[ids.ID]*ids.ShortID{linkKey: &shortID2},
					},
				}
			},
			expectedLinkedShortID: shortID2,
		},
		"OK: shortID link in cache": {
			caminoState: func(c *gomock.Controller) *caminoState {
				cache := cache.NewMockCacher[ids.ID, *ids.ShortID](c)
				cache.EXPECT().Get(linkKey).Return(&shortID2, true)
				return &caminoState{
					shortLinksCache: cache,
					caminoDiff:      &caminoDiff{},
				}
			},
			shortID:      shortID1,
			shortLinkKey: ShortLinkKeyRegisterNode,
			expectedCaminoState: func(actualCaminoState *caminoState) *caminoState {
				return &caminoState{
					shortLinksCache: actualCaminoState.shortLinksCache,
					caminoDiff:      &caminoDiff{},
				}
			},
			expectedLinkedShortID: shortID2,
		},
		"OK: shortID link in db": {
			caminoState: func(c *gomock.Controller) *caminoState {
				cache := cache.NewMockCacher[ids.ID, *ids.ShortID](c)
				cache.EXPECT().Get(linkKey).Return(nil, false)
				cache.EXPECT().Put(linkKey, &shortID2)
				db := database.NewMockDatabase(c)
				db.EXPECT().Get(linkKey[:]).Return(shortID2[:], nil)
				return &caminoState{
					shortLinksDB:    db,
					shortLinksCache: cache,
					caminoDiff:      &caminoDiff{},
				}
			},
			shortID:      shortID1,
			shortLinkKey: ShortLinkKeyRegisterNode,
			expectedCaminoState: func(actualCaminoState *caminoState) *caminoState {
				return &caminoState{
					shortLinksDB:    actualCaminoState.shortLinksDB,
					shortLinksCache: actualCaminoState.shortLinksCache,
					caminoDiff:      &caminoDiff{},
				}
			},
			expectedLinkedShortID: shortID2,
		},
		"Fail: db error": {
			caminoState: func(c *gomock.Controller) *caminoState {
				cache := cache.NewMockCacher[ids.ID, *ids.ShortID](c)
				cache.EXPECT().Get(linkKey).Return(nil, false)
				db := database.NewMockDatabase(c)
				db.EXPECT().Get(linkKey[:]).Return(nil, testError)
				return &caminoState{
					shortLinksDB:    db,
					shortLinksCache: cache,
					caminoDiff:      &caminoDiff{},
				}
			},
			shortID:      shortID1,
			shortLinkKey: ShortLinkKeyRegisterNode,
			expectedCaminoState: func(actualCaminoState *caminoState) *caminoState {
				return &caminoState{
					shortLinksDB:    actualCaminoState.shortLinksDB,
					shortLinksCache: actualCaminoState.shortLinksCache,
					caminoDiff:      &caminoDiff{},
				}
			},
			expectedErr: testError,
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			caminoState := tt.caminoState(ctrl)
			linkedShortID, err := caminoState.GetShortIDLink(tt.shortID, tt.shortLinkKey)
			require.ErrorIs(t, err, tt.expectedErr)
			require.Equal(t, tt.expectedLinkedShortID, linkedShortID)
			require.Equal(t, tt.expectedCaminoState(caminoState), caminoState)
		})
	}
}

func TestSetShortIDLink(t *testing.T) {
	shortID1 := ids.ShortID{1}
	shortID2 := ids.ShortID{2}
	linkKey := toShortLinkKey(shortID1, ShortLinkKeyRegisterNode)

	tests := map[string]struct {
		caminoState         func(*gomock.Controller) *caminoState
		shortID             ids.ShortID
		shortLinkKey        ShortLinkKey
		linkedShortID       *ids.ShortID
		expectedCaminoState func(*caminoState) *caminoState
	}{
		"OK: set link": {
			caminoState: func(c *gomock.Controller) *caminoState {
				cache := cache.NewMockCacher[ids.ID, *ids.ShortID](c)
				cache.EXPECT().Evict(linkKey)
				return &caminoState{
					shortLinksCache: cache,
					caminoDiff:      &caminoDiff{modifiedShortLinks: map[ids.ID]*ids.ShortID{}},
				}
			},
			shortID:       shortID1,
			shortLinkKey:  ShortLinkKeyRegisterNode,
			linkedShortID: &shortID2,
			expectedCaminoState: func(actualCaminoState *caminoState) *caminoState {
				return &caminoState{
					shortLinksCache: actualCaminoState.shortLinksCache,
					caminoDiff: &caminoDiff{
						modifiedShortLinks: map[ids.ID]*ids.ShortID{linkKey: &shortID2},
					},
				}
			},
		},
		"OK: remove link": {
			caminoState: func(c *gomock.Controller) *caminoState {
				cache := cache.NewMockCacher[ids.ID, *ids.ShortID](c)
				cache.EXPECT().Evict(linkKey)
				return &caminoState{
					shortLinksCache: cache,
					caminoDiff:      &caminoDiff{modifiedShortLinks: map[ids.ID]*ids.ShortID{}},
				}
			},
			shortID:       shortID1,
			shortLinkKey:  ShortLinkKeyRegisterNode,
			linkedShortID: nil,
			expectedCaminoState: func(actualCaminoState *caminoState) *caminoState {
				return &caminoState{
					shortLinksCache: actualCaminoState.shortLinksCache,
					caminoDiff: &caminoDiff{
						modifiedShortLinks: map[ids.ID]*ids.ShortID{linkKey: nil},
					},
				}
			},
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			caminoState := tt.caminoState(ctrl)
			caminoState.SetShortIDLink(tt.shortID, tt.shortLinkKey, tt.linkedShortID)
			require.Equal(t, tt.expectedCaminoState(caminoState), caminoState)
		})
	}
}

func TestWriteShortLinks(t *testing.T) {
	shortID1 := ids.ShortID{1}
	shortID2 := ids.ShortID{2}
	linkedShortID1 := ids.ShortID{11}
	linkKey1 := toShortLinkKey(shortID1, ShortLinkKeyRegisterNode)
	linkKey2 := toShortLinkKey(shortID2, ShortLinkKeyRegisterNode)
	testError := errors.New("test error")

	tests := map[string]struct {
		caminoState         func(*gomock.Controller) *caminoState
		expectedCaminoState func(*caminoState) *caminoState
		expectedErr         error
	}{
		"Fail: db errored on modifiedShortLinks Put": {
			caminoState: func(c *gomock.Controller) *caminoState {
				shortLinksDB := database.NewMockDatabase(c)
				shortLinksDB.EXPECT().Put(linkKey1[:], linkedShortID1[:]).Return(testError)
				return &caminoState{
					shortLinksDB: shortLinksDB,
					caminoDiff: &caminoDiff{
						modifiedShortLinks: map[ids.ID]*ids.ShortID{
							linkKey1: &linkedShortID1,
						},
					},
				}
			},
			expectedCaminoState: func(actualState *caminoState) *caminoState {
				return &caminoState{
					shortLinksDB: actualState.shortLinksDB,
					caminoDiff: &caminoDiff{
						modifiedShortLinks: map[ids.ID]*ids.ShortID{},
					},
				}
			},
			expectedErr: testError,
		},
		"Fail: db errored on modifiedShortLinks Delete": {
			caminoState: func(c *gomock.Controller) *caminoState {
				shortLinksDB := database.NewMockDatabase(c)
				shortLinksDB.EXPECT().Delete(linkKey1[:]).Return(testError)
				return &caminoState{
					caminoDiff: &caminoDiff{
						modifiedShortLinks: map[ids.ID]*ids.ShortID{
							linkKey1: nil,
						},
					},
					shortLinksDB: shortLinksDB,
				}
			},
			expectedCaminoState: func(actualState *caminoState) *caminoState {
				return &caminoState{
					caminoDiff: &caminoDiff{
						modifiedShortLinks: map[ids.ID]*ids.ShortID{},
					},
					shortLinksDB: actualState.shortLinksDB,
				}
			},
			expectedErr: testError,
		},
		"OK": {
			caminoState: func(c *gomock.Controller) *caminoState {
				shortLinksDB := database.NewMockDatabase(c)
				shortLinksDB.EXPECT().Put(linkKey1[:], linkedShortID1[:]).Return(nil)
				shortLinksDB.EXPECT().Delete(linkKey2[:]).Return(nil)
				return &caminoState{
					shortLinksDB: shortLinksDB,
					caminoDiff: &caminoDiff{
						modifiedShortLinks: map[ids.ID]*ids.ShortID{
							linkKey1: &linkedShortID1,
							linkKey2: nil,
						},
					},
				}
			},
			expectedCaminoState: func(actualState *caminoState) *caminoState {
				return &caminoState{
					shortLinksDB: actualState.shortLinksDB,
					caminoDiff: &caminoDiff{
						modifiedShortLinks: map[ids.ID]*ids.ShortID{},
					},
				}
			},
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			actualCaminoState := tt.caminoState(ctrl)
			require.ErrorIs(t, actualCaminoState.writeShortLinks(), tt.expectedErr)
			require.Equal(t, tt.expectedCaminoState(actualCaminoState), actualCaminoState)
		})
	}
}

// Copyright (C) 2022-2023, Chain4Travel AG. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"errors"
	"testing"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/vms/platformvm/blocks"
	"github.com/ava-labs/avalanchego/vms/platformvm/deposit"
	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/require"
)

func TestGetDepositOffer(t *testing.T) {
	depositOffer1 := &deposit.Offer{ID: ids.ID{1}}
	depositOffer2 := &deposit.Offer{ID: ids.ID{2}}

	tests := map[string]struct {
		caminoState          func(*gomock.Controller) *caminoState
		depositOfferID       ids.ID
		expectedCaminoState  func(*caminoState) *caminoState
		expectedDepositOffer *deposit.Offer
		expectedErr          error
	}{
		"Fail: offer removed": {
			caminoState: func(c *gomock.Controller) *caminoState {
				return &caminoState{
					depositOffers: map[ids.ID]*deposit.Offer{
						depositOffer1.ID: depositOffer1,
						depositOffer2.ID: depositOffer2,
					},
					caminoDiff: &caminoDiff{
						modifiedDepositOffers: map[ids.ID]*deposit.Offer{
							depositOffer1.ID: nil,
						},
					},
				}
			},
			expectedCaminoState: func(actualCaminoState *caminoState) *caminoState {
				return &caminoState{
					depositOffers: map[ids.ID]*deposit.Offer{
						depositOffer1.ID: depositOffer1,
						depositOffer2.ID: depositOffer2,
					},
					caminoDiff: &caminoDiff{
						modifiedDepositOffers: map[ids.ID]*deposit.Offer{
							depositOffer1.ID: nil,
						},
					},
				}
			},
			depositOfferID: depositOffer1.ID,
			expectedErr:    database.ErrNotFound,
		},
		"Fail: offer doesn't exist": {
			caminoState: func(c *gomock.Controller) *caminoState {
				return &caminoState{
					depositOffers: map[ids.ID]*deposit.Offer{
						depositOffer2.ID: depositOffer2,
					},
					caminoDiff: &caminoDiff{},
				}
			},
			expectedCaminoState: func(actualCaminoState *caminoState) *caminoState {
				return &caminoState{
					depositOffers: map[ids.ID]*deposit.Offer{
						depositOffer2.ID: depositOffer2,
					},
					caminoDiff: &caminoDiff{},
				}
			},
			depositOfferID: depositOffer1.ID,
			expectedErr:    database.ErrNotFound,
		},
		"OK: offer added": {
			caminoState: func(c *gomock.Controller) *caminoState {
				return &caminoState{
					depositOffers: map[ids.ID]*deposit.Offer{
						depositOffer1.ID: depositOffer1,
					},
					caminoDiff: &caminoDiff{
						modifiedDepositOffers: map[ids.ID]*deposit.Offer{
							depositOffer2.ID: depositOffer2,
						},
					},
				}
			},
			expectedCaminoState: func(actualCaminoState *caminoState) *caminoState {
				return &caminoState{
					depositOffers: map[ids.ID]*deposit.Offer{
						depositOffer1.ID: depositOffer1,
					},
					caminoDiff: &caminoDiff{
						modifiedDepositOffers: map[ids.ID]*deposit.Offer{
							depositOffer2.ID: depositOffer2,
						},
					},
				}
			},
			depositOfferID:       depositOffer2.ID,
			expectedDepositOffer: depositOffer2,
		},
		"OK: offer modified": {
			caminoState: func(c *gomock.Controller) *caminoState {
				return &caminoState{
					depositOffers: map[ids.ID]*deposit.Offer{
						depositOffer1.ID: depositOffer1,
						depositOffer2.ID: depositOffer2,
					},
					caminoDiff: &caminoDiff{
						modifiedDepositOffers: map[ids.ID]*deposit.Offer{
							depositOffer2.ID: {ID: depositOffer2.ID, MinAmount: 1},
						},
					},
				}
			},
			expectedCaminoState: func(actualCaminoState *caminoState) *caminoState {
				return &caminoState{
					depositOffers: map[ids.ID]*deposit.Offer{
						depositOffer1.ID: depositOffer1,
						depositOffer2.ID: depositOffer2,
					},
					caminoDiff: &caminoDiff{
						modifiedDepositOffers: map[ids.ID]*deposit.Offer{
							depositOffer2.ID: {ID: depositOffer2.ID, MinAmount: 1},
						},
					},
				}
			},
			depositOfferID:       depositOffer2.ID,
			expectedDepositOffer: &deposit.Offer{ID: depositOffer2.ID, MinAmount: 1},
		},
		"OK": {
			caminoState: func(c *gomock.Controller) *caminoState {
				return &caminoState{
					depositOffers: map[ids.ID]*deposit.Offer{
						depositOffer1.ID: depositOffer1,
						depositOffer2.ID: depositOffer2,
					},
					caminoDiff: &caminoDiff{},
				}
			},
			expectedCaminoState: func(actualCaminoState *caminoState) *caminoState {
				return &caminoState{
					depositOffers: map[ids.ID]*deposit.Offer{
						depositOffer1.ID: depositOffer1,
						depositOffer2.ID: depositOffer2,
					},
					caminoDiff: &caminoDiff{},
				}
			},
			depositOfferID:       depositOffer1.ID,
			expectedDepositOffer: depositOffer1,
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()
			caminoState := tt.caminoState(ctrl)
			actualDepositOffer, err := caminoState.GetDepositOffer(tt.depositOfferID)
			require.ErrorIs(t, err, tt.expectedErr)
			require.Equal(t, tt.expectedDepositOffer, actualDepositOffer)
			require.Equal(t, tt.expectedCaminoState(caminoState), caminoState)
		})
	}
}

func TestSetDepositOffer(t *testing.T) {
	depositOffer1 := &deposit.Offer{ID: ids.ID{1}}
	depositOffer2 := &deposit.Offer{ID: ids.ID{2}}
	depositOffer3 := &deposit.Offer{ID: ids.ID{3}}

	tests := map[string]struct {
		caminoState         *caminoState
		depositOffer        *deposit.Offer
		expectedCaminoState *caminoState
	}{
		"OK": {
			caminoState: &caminoState{
				depositOffers: map[ids.ID]*deposit.Offer{
					depositOffer1.ID: depositOffer1,
				},
				caminoDiff: &caminoDiff{
					modifiedDepositOffers: map[ids.ID]*deposit.Offer{
						depositOffer2.ID: depositOffer2,
					},
				},
			},
			depositOffer: depositOffer3,
			expectedCaminoState: &caminoState{
				depositOffers: map[ids.ID]*deposit.Offer{
					depositOffer1.ID: depositOffer1,
				},
				caminoDiff: &caminoDiff{
					modifiedDepositOffers: map[ids.ID]*deposit.Offer{
						depositOffer2.ID: depositOffer2,
						depositOffer3.ID: depositOffer3,
					},
				},
			},
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			tt.caminoState.SetDepositOffer(tt.depositOffer)
			require.Equal(t, tt.expectedCaminoState, tt.caminoState)
		})
	}
}

func TestGetAllDepositOffers(t *testing.T) {
	depositOffer1 := &deposit.Offer{ID: ids.ID{1}}
	depositOffer2 := &deposit.Offer{ID: ids.ID{2}}
	depositOffer3 := &deposit.Offer{ID: ids.ID{3}}
	depositOffer4 := &deposit.Offer{ID: ids.ID{4}}

	tests := map[string]struct {
		caminoState           func(c *gomock.Controller) *caminoState
		expectedCaminoState   func(*caminoState) *caminoState
		expectedDepositOffers []*deposit.Offer
		expectedErr           error
	}{
		"OK": {
			caminoState: func(c *gomock.Controller) *caminoState {
				return &caminoState{
					depositOffers: map[ids.ID]*deposit.Offer{
						depositOffer1.ID: depositOffer1,
						depositOffer2.ID: depositOffer2,
						depositOffer4.ID: depositOffer4,
					},
					caminoDiff: &caminoDiff{
						modifiedDepositOffers: map[ids.ID]*deposit.Offer{
							depositOffer2.ID: {ID: depositOffer2.ID, MinAmount: 1},
							depositOffer3.ID: depositOffer3,
							depositOffer4.ID: nil,
						},
					},
				}
			},
			expectedCaminoState: func(actualCaminoState *caminoState) *caminoState {
				return &caminoState{
					depositOffers: map[ids.ID]*deposit.Offer{
						depositOffer1.ID: depositOffer1,
						depositOffer2.ID: depositOffer2,
						depositOffer4.ID: depositOffer4,
					},
					caminoDiff: &caminoDiff{
						modifiedDepositOffers: map[ids.ID]*deposit.Offer{
							depositOffer2.ID: {ID: depositOffer2.ID, MinAmount: 1},
							depositOffer3.ID: depositOffer3,
							depositOffer4.ID: nil,
						},
					},
				}
			},
			expectedDepositOffers: []*deposit.Offer{
				depositOffer1,
				{ID: depositOffer2.ID, MinAmount: 1},
				depositOffer3,
			},
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()
			caminoState := tt.caminoState(ctrl)
			depositOffers, err := caminoState.GetAllDepositOffers()
			require.ErrorIs(t, err, tt.expectedErr)
			require.ElementsMatch(t, tt.expectedDepositOffers, depositOffers)
			require.Equal(t, tt.expectedCaminoState(caminoState), caminoState)
		})
	}
}

func TestWriteDepositOffers(t *testing.T) {
	depositOffer1 := &deposit.Offer{ID: ids.ID{1}}
	depositOffer2 := &deposit.Offer{ID: ids.ID{2}}
	depositOffer2modified := &deposit.Offer{ID: ids.ID{2}, MinAmount: 1}
	depositOffer3 := &deposit.Offer{ID: ids.ID{3}}
	depositOffer4 := &deposit.Offer{ID: ids.ID{4}}
	depositOffer2modifiedBytes, err := blocks.GenesisCodec.Marshal(blocks.Version, depositOffer2modified)
	require.NoError(t, err)
	depositOffer2Bytes, err := blocks.GenesisCodec.Marshal(blocks.Version, depositOffer2)
	require.NoError(t, err)
	depositOffer3Bytes, err := blocks.GenesisCodec.Marshal(blocks.Version, depositOffer3)
	require.NoError(t, err)
	testError := errors.New("test error")

	tests := map[string]struct {
		caminoState         func(*gomock.Controller) *caminoState
		expectedCaminoState func(*caminoState) *caminoState
		expectedErr         error
	}{
		"Fail: db errored on Put": {
			caminoState: func(c *gomock.Controller) *caminoState {
				depositOffersDB := database.NewMockDatabase(c)
				depositOffersDB.EXPECT().Put(depositOffer2.ID[:], depositOffer2Bytes).Return(testError)
				return &caminoState{
					caminoDiff: &caminoDiff{
						modifiedDepositOffers: map[ids.ID]*deposit.Offer{
							depositOffer2.ID: depositOffer2,
						},
					},
					depositOffersDB: depositOffersDB,
				}
			},
			expectedCaminoState: func(actualCaminoState *caminoState) *caminoState {
				return &caminoState{
					caminoDiff: &caminoDiff{
						modifiedDepositOffers: map[ids.ID]*deposit.Offer{},
					},
					depositOffersDB: actualCaminoState.depositOffersDB,
				}
			},
			expectedErr: testError,
		},
		"Fail: db errored on Delete": {
			caminoState: func(c *gomock.Controller) *caminoState {
				depositOffersDB := database.NewMockDatabase(c)
				depositOffersDB.EXPECT().Delete(depositOffer1.ID[:]).Return(testError)
				return &caminoState{
					caminoDiff: &caminoDiff{
						modifiedDepositOffers: map[ids.ID]*deposit.Offer{
							depositOffer1.ID: nil,
						},
					},
					depositOffersDB: depositOffersDB,
				}
			},
			expectedCaminoState: func(actualCaminoState *caminoState) *caminoState {
				return &caminoState{
					caminoDiff: &caminoDiff{
						modifiedDepositOffers: map[ids.ID]*deposit.Offer{},
					},
					depositOffersDB: actualCaminoState.depositOffersDB,
				}
			},
			expectedErr: testError,
		},
		"OK": {
			caminoState: func(c *gomock.Controller) *caminoState {
				depositOffersDB := database.NewMockDatabase(c)
				depositOffersDB.EXPECT().Put(depositOffer2.ID[:], depositOffer2modifiedBytes).Return(nil)
				depositOffersDB.EXPECT().Put(depositOffer3.ID[:], depositOffer3Bytes).Return(nil)
				depositOffersDB.EXPECT().Delete(depositOffer4.ID[:]).Return(nil)
				return &caminoState{
					depositOffers: map[ids.ID]*deposit.Offer{
						depositOffer1.ID: depositOffer1,
						depositOffer2.ID: depositOffer2,
						depositOffer4.ID: depositOffer4,
					},
					caminoDiff: &caminoDiff{
						modifiedDepositOffers: map[ids.ID]*deposit.Offer{
							depositOffer2.ID: depositOffer2modified,
							depositOffer3.ID: depositOffer3,
							depositOffer4.ID: nil,
						},
					},
					depositOffersDB: depositOffersDB,
				}
			},
			expectedCaminoState: func(actualCaminoState *caminoState) *caminoState {
				return &caminoState{
					depositOffers: map[ids.ID]*deposit.Offer{
						depositOffer1.ID: depositOffer1,
						depositOffer2.ID: depositOffer2modified,
						depositOffer3.ID: depositOffer3,
					},
					caminoDiff: &caminoDiff{
						modifiedDepositOffers: map[ids.ID]*deposit.Offer{},
					},
					depositOffersDB: actualCaminoState.depositOffersDB,
				}
			},
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()
			actualCaminoState := tt.caminoState(ctrl)
			require.ErrorIs(t, actualCaminoState.writeDepositOffers(), tt.expectedErr)
			require.Equal(t, tt.expectedCaminoState(actualCaminoState), actualCaminoState)
		})
	}
}

func TestLoadDepositOffers(t *testing.T) {
	depositOffer1 := &deposit.Offer{ID: ids.ID{1}, Memo: []byte("1")}
	depositOffer2 := &deposit.Offer{ID: ids.ID{2}, Memo: []byte("2")}
	depositOffer3 := &deposit.Offer{ID: ids.ID{3}, Memo: []byte("3")}
	depositOffer1Bytes, err := blocks.GenesisCodec.Marshal(blocks.Version, depositOffer1)
	require.NoError(t, err)
	depositOffer2Bytes, err := blocks.GenesisCodec.Marshal(blocks.Version, depositOffer2)
	require.NoError(t, err)
	depositOffer3Bytes, err := blocks.GenesisCodec.Marshal(blocks.Version, depositOffer3)
	require.NoError(t, err)

	tests := map[string]struct {
		caminoState         func(*gomock.Controller) *caminoState
		expectedCaminoState func(*caminoState) *caminoState
		expectedErr         error
	}{
		"OK": {
			caminoState: func(c *gomock.Controller) *caminoState {
				offersIterator := database.NewMockIterator(c)
				offersIterator.EXPECT().Next().Return(true).Times(3)
				offersIterator.EXPECT().Key().Return(depositOffer1.ID[:])
				offersIterator.EXPECT().Value().Return(depositOffer1Bytes)
				offersIterator.EXPECT().Key().Return(depositOffer2.ID[:])
				offersIterator.EXPECT().Value().Return(depositOffer2Bytes)
				offersIterator.EXPECT().Key().Return(depositOffer3.ID[:])
				offersIterator.EXPECT().Value().Return(depositOffer3Bytes)
				offersIterator.EXPECT().Error().Return(nil)
				offersIterator.EXPECT().Next().Return(false)
				offersIterator.EXPECT().Release()

				depositOffersDB := database.NewMockDatabase(c)
				depositOffersDB.EXPECT().NewIterator().Return(offersIterator)
				return &caminoState{
					depositOffers:   map[ids.ID]*deposit.Offer{},
					depositOffersDB: depositOffersDB,
				}
			},
			expectedCaminoState: func(actualCaminoState *caminoState) *caminoState {
				return &caminoState{
					depositOffersDB: actualCaminoState.depositOffersDB,
					depositOffers: map[ids.ID]*deposit.Offer{
						depositOffer1.ID: depositOffer1,
						depositOffer2.ID: depositOffer2,
						depositOffer3.ID: depositOffer3,
					},
				}
			},
		},
		"OK: no deposits": {
			caminoState: func(c *gomock.Controller) *caminoState {
				offersIterator := database.NewMockIterator(c)
				offersIterator.EXPECT().Next().Return(false)
				offersIterator.EXPECT().Error().Return(nil)
				offersIterator.EXPECT().Release()

				depositOffersDB := database.NewMockDatabase(c)
				depositOffersDB.EXPECT().NewIterator().Return(offersIterator)
				return &caminoState{depositOffersDB: depositOffersDB}
			},
			expectedCaminoState: func(actualCaminoState *caminoState) *caminoState {
				return &caminoState{
					depositOffersDB: actualCaminoState.depositOffersDB,
				}
			},
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()
			actualCaminoState := tt.caminoState(ctrl)
			require.ErrorIs(t, actualCaminoState.loadDepositOffers(), tt.expectedErr)
			require.Equal(t, tt.expectedCaminoState(actualCaminoState), actualCaminoState)
		})
	}
}

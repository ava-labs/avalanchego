// Copyright (C) 2022-2024, Chain4Travel AG. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"fmt"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/vms/platformvm/blocks"
	"github.com/ava-labs/avalanchego/vms/platformvm/deposit"
)

func (cs *caminoState) SetDepositOffer(offer *deposit.Offer) {
	cs.modifiedDepositOffers[offer.ID] = offer
}

func (cs *caminoState) GetDepositOffer(offerID ids.ID) (*deposit.Offer, error) {
	// Try to get from modified state
	offer, ok := cs.modifiedDepositOffers[offerID]
	// offer was deleted
	if ok && offer == nil {
		return nil, database.ErrNotFound
	}
	// Try to get it from state
	if !ok {
		if offer, ok = cs.depositOffers[offerID]; !ok {
			return nil, database.ErrNotFound
		}
	}
	return offer, nil
}

func (cs *caminoState) GetAllDepositOffers() ([]*deposit.Offer, error) {
	var offers []*deposit.Offer

	for _, offer := range cs.modifiedDepositOffers {
		if offer != nil {
			offers = append(offers, offer)
		}
	}

	for offerID, offer := range cs.depositOffers {
		if _, ok := cs.modifiedDepositOffers[offerID]; !ok {
			offers = append(offers, offer)
		}
	}

	return offers, nil
}

func (cs *caminoState) loadDepositOffers() error {
	depositOffersIt := cs.depositOffersDB.NewIterator()
	defer depositOffersIt.Release()
	for depositOffersIt.Next() {
		depositOfferIDBytes := depositOffersIt.Key()
		depositOfferID, err := ids.ToID(depositOfferIDBytes)
		if err != nil {
			return err
		}

		depositOfferBytes := depositOffersIt.Value()
		depositOffer := &deposit.Offer{ID: depositOfferID}
		if _, err := blocks.GenesisCodec.Unmarshal(depositOfferBytes, depositOffer); err != nil {
			return err
		}

		cs.depositOffers[depositOfferID] = depositOffer
	}

	if err := depositOffersIt.Error(); err != nil {
		return err
	}

	return nil
}

func (cs *caminoState) writeDepositOffers() error {
	for offerID, offer := range cs.modifiedDepositOffers {
		delete(cs.modifiedDepositOffers, offerID)
		if offer == nil {
			if err := cs.depositOffersDB.Delete(offerID[:]); err != nil {
				return err
			}
			delete(cs.depositOffers, offerID)
		} else {
			offerBytes, err := blocks.GenesisCodec.Marshal(blocks.Version, offer)
			if err != nil {
				return fmt.Errorf("failed to serialize deposit offer: %w", err)
			}
			if err := cs.depositOffersDB.Put(offerID[:], offerBytes); err != nil {
				return err
			}
			cs.depositOffers[offerID] = offer
		}
	}
	return nil
}

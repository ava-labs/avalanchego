// Copyright (C) 2022, Chain4Travel AG. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"github.com/ava-labs/avalanchego/cache"
	"github.com/ava-labs/avalanchego/cache/metercacher"
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/linkeddb"
	"github.com/ava-labs/avalanchego/database/prefixdb"
	"github.com/ava-labs/avalanchego/database/versiondb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/platformvm/genesis"
	"github.com/ava-labs/avalanchego/vms/platformvm/locked"
	"github.com/ava-labs/avalanchego/vms/platformvm/status"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/prometheus/client_golang/prometheus"
)

const addressStateCacheSize = 1024

var (
	_ CaminoState = (*caminoState)(nil)

	addressStatePrefix  = []byte("addressState")
	depositOffersPrefix = []byte("depositOffers")
)

type CaminoApply interface {
	ApplyCaminoState(State)
}

type CaminoDiff interface {
	// Address State

	SetAddressStates(ids.ShortID, uint64)
	GetAddressStates(ids.ShortID) (uint64, error)

	// Deposit offers state

	// precondition: offer.SetID() must be called and return no error
	AddDepositOffer(offer *DepositOffer)
	GetDepositOffer(offerID ids.ID) (*DepositOffer, error)
	GetAllDepositOffers() ([]*DepositOffer, error)
}

// For state and diff
type Camino interface {
	CaminoDiff

	LockedUTXOs(ids.Set, ids.ShortSet, locked.State) ([]*avax.UTXO, error)
	CaminoGenesisState() (*genesis.Camino, error)
}

// For state only
type CaminoState interface {
	CaminoDiff

	GenesisState() *genesis.Camino
	SyncGenesis(*state, *genesis.State) error
	Load() error
	Write() error
}

type caminoDiff struct {
	modifiedAddressStates map[ids.ShortID]uint64
	modifiedDepositOffers map[ids.ID]*DepositOffer
}

type caminoState struct {
	caminoDiff

	verifyNodeSignature bool
	lockModeBondDeposit bool

	// Address State
	addressStateCache cache.Cacher
	addressStateDB    database.Database

	// Deposit offers
	depositOffers     map[ids.ID]*DepositOffer
	depositOffersList linkeddb.LinkedDB
	depositOffersDB   database.Database
}

func newCaminoState(baseDB *versiondb.Database, metricsReg prometheus.Registerer) (*caminoState, error) {
	addressStateCache, err := metercacher.New(
		"address_state_cache",
		metricsReg,
		&cache.LRU{Size: addressStateCacheSize},
	)
	if err != nil {
		return nil, err
	}

	if err != nil {
		return nil, err
	}

	depositOffersDB := prefixdb.New(depositOffersPrefix, baseDB)

	return &caminoState{
		addressStateDB:    prefixdb.New(addressStatePrefix, baseDB),
		addressStateCache: addressStateCache,

		depositOffers:     make(map[ids.ID]*DepositOffer),
		depositOffersDB:   depositOffersDB,
		depositOffersList: linkeddb.NewDefault(depositOffersDB),

		caminoDiff: caminoDiff{
			modifiedAddressStates: make(map[ids.ShortID]uint64),
			modifiedDepositOffers: make(map[ids.ID]*DepositOffer),
		},
	}, nil
}

// Return current genesis args
func (cs *caminoState) GenesisState() *genesis.Camino {
	return &genesis.Camino{
		VerifyNodeSignature: cs.verifyNodeSignature,
		LockModeBondDeposit: cs.lockModeBondDeposit,
	}
}

// Extract camino tag from genesis
func (cs *caminoState) SyncGenesis(s *state, g *genesis.State) error {
	cs.lockModeBondDeposit = g.Camino.LockModeBondDeposit
	cs.verifyNodeSignature = g.Camino.VerifyNodeSignature

	tx := &txs.AddAddressStateTx{
		Address: g.Camino.InitialAdmin,
		State:   txs.AddressStateRoleAdmin,
		Remove:  false,
	}
	s.AddTx(&txs.Tx{Unsigned: tx}, status.Committed)
	cs.SetAddressStates(g.Camino.InitialAdmin, txs.AddressStateRoleAdminBit)

	for _, genesisOffer := range g.Camino.DepositOffers {
		offer := &DepositOffer{
			UnlockHalfPeriodDuration: genesisOffer.UnlockHalfPeriodDuration,
			InterestRateNominator:    genesisOffer.InterestRateNominator,
			Start:                    genesisOffer.Start,
			End:                      genesisOffer.End,
			MinAmount:                genesisOffer.MinAmount,
			MinDuration:              genesisOffer.MinDuration,
			MaxDuration:              genesisOffer.MaxDuration,
		}
		if err := offer.SetID(); err != nil {
			return err
		}

		cs.AddDepositOffer(offer)
	}

	return nil
}

func (cs *caminoState) Load() error {
	return cs.loadDepositOffers()
}

func (cs *caminoState) Write() error {
	if err := cs.writeAddressStates(); err != nil {
		return err
	}
	return cs.writeDepositOffers()
}

// Copyright (C) 2022, Chain4Travel AG. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"errors"
	"fmt"

	"github.com/ava-labs/avalanchego/cache"
	"github.com/ava-labs/avalanchego/cache/metercacher"
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/linkeddb"
	"github.com/ava-labs/avalanchego/database/prefixdb"
	"github.com/ava-labs/avalanchego/database/versiondb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/math"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/utils/wrappers"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/platformvm/deposit"
	"github.com/ava-labs/avalanchego/vms/platformvm/genesis"
	"github.com/ava-labs/avalanchego/vms/platformvm/locked"
	"github.com/ava-labs/avalanchego/vms/platformvm/status"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/prometheus/client_golang/prometheus"
)

const (
	addressStateCacheSize          = 1024
	depositsCacheSize              = 1024
	consortiumMemberNodesCacheSize = 1024
)

var (
	_ CaminoState = (*caminoState)(nil)

	caminoPrefix                = []byte("camino")
	addressStatePrefix          = []byte("addressState")
	depositOffersPrefix         = []byte("depositOffers")
	depositsPrefix              = []byte("deposits")
	multisigOwnersPrefix        = []byte("multisigOwners")
	ConsortiumMemberNodesPrefix = []byte("consortiumMemberNodes")

	nodeSignatureKey   = []byte("nodeSignature")
	depositBondModeKey = []byte("depositBondMode")

	errWrongTxType      = errors.New("unexpected tx type")
	errNonExistingOffer = errors.New("deposit offer doesn't exist")
)

type CaminoApply interface {
	ApplyCaminoState(State)
}

type CaminoDiff interface {
	// Address State

	SetAddressStates(ids.ShortID, uint64)
	GetAddressStates(ids.ShortID) (uint64, error)

	// Deposit offers

	// precondition: offer.SetID() must be called and return no error
	AddDepositOffer(offer *deposit.Offer)
	GetDepositOffer(offerID ids.ID) (*deposit.Offer, error)
	GetAllDepositOffers() ([]*deposit.Offer, error)

	// Deposits

	UpdateDeposit(depositTxID ids.ID, deposit *deposit.Deposit)
	GetDeposit(depositTxID ids.ID) (*deposit.Deposit, error)

	// Multisig Owners

	GetMultisigOwner(ids.ShortID) (*MultisigOwner, error)
	SetMultisigOwner(*MultisigOwner)

	// Consortium member nodes

	SetNodeConsortiumMember(nodeID ids.NodeID, addr ids.ShortID)
	GetNodeConsortiumMember(nodeID ids.NodeID) (ids.ShortID, error)
}

// For state and diff
type Camino interface {
	CaminoDiff

	LockedUTXOs(set.Set[ids.ID], set.Set[ids.ShortID], locked.State) ([]*avax.UTXO, error)
	CaminoConfig() (*CaminoConfig, error)
}

// For state only
type CaminoState interface {
	CaminoDiff

	CaminoConfig() *CaminoConfig
	SyncGenesis(*state, *genesis.State) error
	Load() error
	Write() error
	Close() error
}

type CaminoConfig struct {
	VerifyNodeSignature bool
	LockModeBondDeposit bool
}

type caminoDiff struct {
	modifiedAddressStates         map[ids.ShortID]uint64
	modifiedDepositOffers         map[ids.ID]*deposit.Offer
	modifiedDeposits              map[ids.ID]*deposit.Deposit
	modifiedMultisigOwners        map[ids.ShortID]*MultisigOwner
	modifiedConsortiumMemberNodes map[ids.NodeID]ids.ShortID
}

type caminoState struct {
	*caminoDiff

	caminoDB            database.Database
	genesisSynced       bool
	verifyNodeSignature bool
	lockModeBondDeposit bool

	// Address State
	addressStateCache cache.Cacher
	addressStateDB    database.Database

	// Deposit offers
	depositOffers     map[ids.ID]*deposit.Offer
	depositOffersList linkeddb.LinkedDB
	depositOffersDB   database.Database

	// Deposits
	depositsCache cache.Cacher
	depositsDB    database.Database

	// MSIG aliases
	multisigOwnersDB database.Database

	// Consortium member nodes
	consortiumMemberNodesCache cache.Cacher
	consortiumMemberNodesDB    database.Database
}

func newCaminoDiff() *caminoDiff {
	return &caminoDiff{
		modifiedAddressStates:         make(map[ids.ShortID]uint64),
		modifiedDepositOffers:         make(map[ids.ID]*deposit.Offer),
		modifiedDeposits:              make(map[ids.ID]*deposit.Deposit),
		modifiedMultisigOwners:        make(map[ids.ShortID]*MultisigOwner),
		modifiedConsortiumMemberNodes: make(map[ids.NodeID]ids.ShortID),
	}
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

	depositsCache, err := metercacher.New(
		"deposits_cache",
		metricsReg,
		&cache.LRU{Size: depositsCacheSize},
	)
	if err != nil {
		return nil, err
	}

	consortiumMemberNodesCache, err := metercacher.New(
		"consortium_member_nodes_cache",
		metricsReg,
		&cache.LRU{Size: consortiumMemberNodesCacheSize},
	)
	if err != nil {
		return nil, err
	}

	depositOffersDB := prefixdb.New(depositOffersPrefix, baseDB)

	return &caminoState{
		addressStateDB:    prefixdb.New(addressStatePrefix, baseDB),
		addressStateCache: addressStateCache,

		depositOffers:     make(map[ids.ID]*deposit.Offer),
		depositOffersDB:   depositOffersDB,
		depositOffersList: linkeddb.NewDefault(depositOffersDB),

		depositsCache: depositsCache,
		depositsDB:    prefixdb.New(depositsPrefix, baseDB),

		multisigOwnersDB: prefixdb.New(multisigOwnersPrefix, baseDB),

		consortiumMemberNodesCache: consortiumMemberNodesCache,
		consortiumMemberNodesDB:    prefixdb.New(ConsortiumMemberNodesPrefix, baseDB),

		caminoDB: prefixdb.New(caminoPrefix, baseDB),

		caminoDiff: newCaminoDiff(),
	}, nil
}

// Return current genesis args
func (cs *caminoState) CaminoConfig() *CaminoConfig {
	return &CaminoConfig{
		VerifyNodeSignature: cs.verifyNodeSignature,
		LockModeBondDeposit: cs.lockModeBondDeposit,
	}
}

// Extract camino tag from genesis
func (cs *caminoState) SyncGenesis(s *state, g *genesis.State) error {
	cs.genesisSynced = true
	cs.lockModeBondDeposit = g.Camino.LockModeBondDeposit
	cs.verifyNodeSignature = g.Camino.VerifyNodeSignature

	if cs.lockModeBondDeposit {
		// overwriting initial supply because state.SyncGenesis
		// added potential avax validator rewards to it
		s.SetCurrentSupply(constants.PrimaryNetworkID, g.InitialSupply)
	}

	// adding address states

	for _, addrState := range g.Camino.AddressStates {
		cs.SetAddressStates(addrState.Address, addrState.State)
	}

	initalAdminAddressState, err := cs.GetAddressStates(g.Camino.InitialAdmin)
	if err != nil {
		return err
	}
	cs.SetAddressStates(g.Camino.InitialAdmin,
		initalAdminAddressState|txs.AddressStateRoleAdminBit)

	tx := &txs.AddAddressStateTx{
		Address: g.Camino.InitialAdmin,
		State:   txs.AddressStateRoleAdmin,
		Remove:  false,
	}
	s.AddTx(&txs.Tx{Unsigned: tx}, status.Committed)

	// adding consortium member nodes

	for _, conMem := range g.Camino.ConsortiumMembersNodeIDs {
		cs.SetNodeConsortiumMember(conMem.NodeID, conMem.ConsortiumMemberAddress)
	}

	// adding deposit offers

	for _, genesisOffer := range g.Camino.DepositOffers {
		genesisOffer := genesisOffer
		offer, err := ParseDepositOfferFromGenesisOffer(&genesisOffer)
		if err != nil {
			return err
		}

		cs.AddDepositOffer(offer)
	}

	// adding deposits

	currentTimestamp := uint64(s.GetTimestamp().Unix())

	for _, tx := range g.Camino.Deposits {
		depositTx, ok := tx.Unsigned.(*txs.DepositTx)
		if !ok {
			return errWrongTxType
		}
		depositAmount := uint64(0)
		for _, out := range depositTx.Outs {
			newAmount, err := math.Add64(depositAmount, out.Out.Amount())
			if err != nil {
				return err
			}
			depositAmount = newAmount
		}

		deposit := &deposit.Deposit{
			DepositOfferID: depositTx.DepositOfferID,
			Start:          currentTimestamp,
			Duration:       depositTx.DepositDuration,
			Amount:         depositAmount,
		}

		currentSupply, err := s.GetCurrentSupply(constants.PrimaryNetworkID)
		if err != nil {
			return err
		}

		offer, ok := cs.modifiedDepositOffers[deposit.DepositOfferID]
		if !ok {
			return errNonExistingOffer
		}

		newCurrentSupply, err := math.Add64(currentSupply, deposit.TotalReward(offer))
		if err != nil {
			return err
		}

		s.SetCurrentSupply(constants.PrimaryNetworkID, newCurrentSupply)
		cs.UpdateDeposit(tx.ID(), deposit)
		s.AddTx(tx, status.Committed)
	}

	for _, gma := range g.Camino.InitialMultisigAddresses {
		owner := FromGenesisMultisigAlias(gma)
		cs.SetMultisigOwner(owner)
	}

	return s.write(false, 0)
}

func (cs *caminoState) Load() error {
	// Read the singletons
	nodeSig, err := database.GetBool(cs.caminoDB, nodeSignatureKey)
	if err != nil {
		return err
	}
	cs.verifyNodeSignature = nodeSig

	mode, err := database.GetBool(cs.caminoDB, depositBondModeKey)
	if err != nil {
		return err
	}
	cs.lockModeBondDeposit = mode

	return cs.loadDepositOffers()
}

func (cs *caminoState) Write() error {
	// Write the singletons (only once after sync)
	if cs.genesisSynced {
		if err := database.PutBool(cs.caminoDB, nodeSignatureKey, cs.verifyNodeSignature); err != nil {
			return fmt.Errorf("failed to write verifyNodeSignature: %w", err)
		}
		if err := database.PutBool(cs.caminoDB, depositBondModeKey, cs.lockModeBondDeposit); err != nil {
			return fmt.Errorf("failed to write lockModeBondDeposit: %w", err)
		}
	}

	if err := cs.writeAddressStates(); err != nil {
		return err
	}
	if err := cs.writeDepositOffers(); err != nil {
		return err
	}
	if err := cs.writeDeposits(); err != nil {
		return err
	}
	if err := cs.writeMultisigOwners(); err != nil {
		return err
	}
	if err := cs.writeNodeConsortiumMembers(); err != nil {
		return err
	}

	return nil
}

func (cs *caminoState) Close() error {
	errs := wrappers.Errs{}
	errs.Add(
		cs.caminoDB.Close(),
		cs.addressStateDB.Close(),
		cs.depositOffersDB.Close(),
		cs.depositsDB.Close(),
		cs.multisigOwnersDB.Close(),
		cs.consortiumMemberNodesDB.Close(),
	)
	return errs.Err
}

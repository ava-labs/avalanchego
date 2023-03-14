// Copyright (C) 2022-2023, Chain4Travel AG. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"errors"
	"fmt"
	"time"

	"github.com/ava-labs/avalanchego/cache"
	"github.com/ava-labs/avalanchego/cache/metercacher"
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/linkeddb"
	"github.com/ava-labs/avalanchego/database/prefixdb"
	"github.com/ava-labs/avalanchego/ids"
	choices "github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/math"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/utils/wrappers"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/components/multisig"
	"github.com/ava-labs/avalanchego/vms/platformvm/blocks"
	"github.com/ava-labs/avalanchego/vms/platformvm/config"
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
	msigOwnersCacheSize            = 16_384
)

var (
	_ CaminoState = (*caminoState)(nil)

	caminoPrefix                = []byte("camino")
	addressStatePrefix          = []byte("addressState")
	depositOffersPrefix         = []byte("depositOffers")
	depositsPrefix              = []byte("deposits")
	depositIDsByEndtimePrefix   = []byte("depositIDsByEndtime")
	multisigOwnersPrefix        = []byte("multisigOwners")
	consortiumMemberNodesPrefix = []byte("consortiumMemberNodes")
	claimablesPrefix            = []byte("claimables")

	// Used for prefixing the validatorsDB
	deferredPrefix = []byte("deferred")

	nodeSignatureKey                 = []byte("nodeSignature")
	depositBondModeKey               = []byte("depositBondMode")
	notDistributedValidatorRewardKey = []byte("notDistributedValidatorReward")

	errWrongTxType      = errors.New("unexpected tx type")
	errNonExistingOffer = errors.New("deposit offer doesn't exist")
	errNotUniqueTx      = errors.New("not unique genesis tx")
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

	// deposit start and duration should never be modified
	SetDeposit(depositTxID ids.ID, deposit *deposit.Deposit)
	RemoveDeposit(depositTxID ids.ID, deposit *deposit.Deposit)
	GetDeposit(depositTxID ids.ID) (*deposit.Deposit, error)
	GetNextToUnlockDepositTime() (time.Time, error)
	GetNextToUnlockDepositIDsAndTime() ([]ids.ID, time.Time, error)

	// Multisig Owners

	GetMultisigAlias(ids.ShortID) (*multisig.Alias, error)
	SetMultisigAlias(*multisig.Alias)

	// ShortIDsLink

	SetShortIDLink(id ids.ShortID, key ShortLinkKey, link *ids.ShortID)
	GetShortIDLink(id ids.ShortID, key ShortLinkKey) (ids.ShortID, error)

	// Claimable & rewards

	SetClaimable(ownerID ids.ID, claimable *Claimable)
	GetClaimable(ownerID ids.ID) (*Claimable, error)
	SetNotDistributedValidatorReward(reward uint64)
	GetNotDistributedValidatorReward() (uint64, error)

	// Deferred validator set

	GetDeferredValidator(subnetID ids.ID, nodeID ids.NodeID) (*Staker, error)
	PutDeferredValidator(staker *Staker)
	DeleteDeferredValidator(staker *Staker)
	GetDeferredStakerIterator() (StakerIterator, error)
}

// For state and diff
type Camino interface {
	CaminoDiff

	LockedUTXOs(set.Set[ids.ID], set.Set[ids.ShortID], locked.State) ([]*avax.UTXO, error)
	CaminoConfig() (*CaminoConfig, error)
	Config() (*config.Config, error)
}

// For state only
type CaminoState interface {
	CaminoDiff

	CaminoConfig() *CaminoConfig
	SyncGenesis(*state, *genesis.State) error
	Load(*state) error
	Write() error
	Close() error
}

type CaminoConfig struct {
	VerifyNodeSignature bool
	LockModeBondDeposit bool
}

type caminoDiff struct {
	deferredStakerDiffs                   diffStakers
	modifiedAddressStates                 map[ids.ShortID]uint64
	modifiedDepositOffers                 map[ids.ID]*deposit.Offer
	modifiedDeposits                      map[ids.ID]*deposit.Deposit
	removedDeposits                       map[ids.ID]*deposit.Deposit
	modifiedMultisigOwners                map[ids.ShortID]*multisig.Alias
	modifiedShortLinks                    map[ids.ID]*ids.ShortID
	modifiedClaimables                    map[ids.ID]*Claimable
	modifiedNotDistributedValidatorReward *uint64
}

type caminoState struct {
	*caminoDiff

	caminoDB            database.Database
	genesisSynced       bool
	verifyNodeSignature bool
	lockModeBondDeposit bool

	// Deferred Stakers
	deferredStakers       *baseStakers
	deferredValidatorsDB  database.Database
	deferredValidatorList linkeddb.LinkedDB

	// Address State
	addressStateCache cache.Cacher
	addressStateDB    database.Database

	// Deposit offers
	depositOffers     map[ids.ID]*deposit.Offer
	depositOffersList linkeddb.LinkedDB
	depositOffersDB   database.Database

	// Deposits
	depositsNextToUnlockTime *time.Time
	depositsNextToUnlockIDs  []ids.ID
	depositsCache            cache.Cacher
	depositsDB               database.Database
	depositIDsByEndtimeDB    database.Database

	// MSIG aliases
	multisigOwnersCache cache.Cacher
	multisigOwnersDB    database.Database

	// shortIDs link
	shortLinksCache cache.Cacher
	shortLinksDB    database.Database

	//  Claimable & rewards
	claimableDB database.Database
}

func newCaminoDiff() *caminoDiff {
	return &caminoDiff{
		modifiedAddressStates:  make(map[ids.ShortID]uint64),
		modifiedDepositOffers:  make(map[ids.ID]*deposit.Offer),
		modifiedDeposits:       make(map[ids.ID]*deposit.Deposit),
		removedDeposits:        make(map[ids.ID]*deposit.Deposit),
		modifiedMultisigOwners: make(map[ids.ShortID]*multisig.Alias),
		modifiedShortLinks:     make(map[ids.ID]*ids.ShortID),
		modifiedClaimables:     make(map[ids.ID]*Claimable),
	}
}

func newCaminoState(baseDB, validatorsDB database.Database, metricsReg prometheus.Registerer) (*caminoState, error) {
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
	depositsDB := prefixdb.New(depositsPrefix, baseDB)
	deferredValidatorsDB := prefixdb.New(deferredPrefix, validatorsDB)

	multisigOwnersCache, err := metercacher.New(
		"msig_owners_cache",
		metricsReg,
		&cache.LRU{Size: msigOwnersCacheSize},
	)
	if err != nil {
		return nil, err
	}
	return &caminoState{
		// Address State
		addressStateDB:    prefixdb.New(addressStatePrefix, baseDB),
		addressStateCache: addressStateCache,

		// Deposit offers
		depositOffers:     make(map[ids.ID]*deposit.Offer),
		depositOffersDB:   depositOffersDB,
		depositOffersList: linkeddb.NewDefault(depositOffersDB),

		// Deposits
		depositsCache:         depositsCache,
		depositsDB:            depositsDB,
		depositIDsByEndtimeDB: prefixdb.New(depositIDsByEndtimePrefix, baseDB), // TODO@ prefix

		// Multisig Owners
		multisigOwnersCache: multisigOwnersCache,
		multisigOwnersDB:    prefixdb.New(multisigOwnersPrefix, baseDB),

		// Consortium member nodes
		shortLinksCache: consortiumMemberNodesCache,
		shortLinksDB:    prefixdb.New(consortiumMemberNodesPrefix, baseDB),

		//  Claimable & rewards
		claimableDB: prefixdb.New(claimablesPrefix, baseDB),

		// Deferred Stakers
		deferredStakers:       newBaseStakers(),
		deferredValidatorsDB:  deferredValidatorsDB,
		deferredValidatorList: linkeddb.NewDefault(deferredValidatorsDB),

		caminoDB:   prefixdb.New(caminoPrefix, baseDB),
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

	txIDs := set.Set[ids.ID]{}

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

	addrStateTx, err := txs.NewSigned(&txs.AddressStateTx{
		Address: g.Camino.InitialAdmin,
		State:   txs.AddressStateRoleAdmin,
		Remove:  false,
	}, txs.Codec, nil)
	if err != nil {
		return err
	}

	s.AddTx(addrStateTx, status.Committed)
	txIDs.Add(addrStateTx.ID())

	// adding consortium member nodes

	for _, consortiumMemberNode := range g.Camino.ConsortiumMembersNodeIDs {
		consortiumMemberNode := consortiumMemberNode
		cs.SetShortIDLink(
			ids.ShortID(consortiumMemberNode.NodeID),
			ShortLinkKeyRegisterNode,
			&consortiumMemberNode.ConsortiumMemberAddress,
		)
		backLink := ids.ShortID(consortiumMemberNode.NodeID)
		cs.SetShortIDLink(
			consortiumMemberNode.ConsortiumMemberAddress,
			ShortLinkKeyRegisterNode,
			&backLink,
		)
	}

	// adding deposit offers

	depositOffers := make(map[ids.ID]*deposit.Offer, len(g.Camino.DepositOffers))
	for _, offer := range g.Camino.DepositOffers {
		depositOffers[offer.ID] = offer
		cs.AddDepositOffer(offer)
	}

	// adding msig aliases

	for _, multisigAlias := range g.Camino.MultisigAliases {
		cs.SetMultisigAlias(multisigAlias)
	}

	// adding blocks (validators and deposits)

	for blockIndex, block := range g.Camino.Blocks {
		// add unlocked utxos txs
		for _, tx := range block.UnlockedUTXOsTxs {
			if txIDs.Contains(tx.ID()) {
				return errNotUniqueTx
			}
			txIDs.Add(tx.ID())

			s.AddTx(tx, status.Committed)
		}

		// add validators
		for _, tx := range block.Validators {
			if txIDs.Contains(tx.ID()) {
				return errNotUniqueTx
			}
			txIDs.Add(tx.ID())

			validatorTx, ok := tx.Unsigned.(txs.ValidatorTx)
			if !ok {
				return fmt.Errorf("expected tx type txs.ValidatorTx but got %T", tx.Unsigned)
			}

			staker, err := NewCurrentStaker(tx.ID(), validatorTx, 0)
			if err != nil {
				return err
			}

			s.PutCurrentValidator(staker)
			s.AddTx(tx, status.Committed)
		}

		// add deposits
		for _, tx := range block.Deposits {
			depositTxID := tx.ID()
			if txIDs.Contains(depositTxID) {
				return errNotUniqueTx
			}
			txIDs.Add(depositTxID)

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
				Start:          block.Timestamp,
				Duration:       depositTx.DepositDuration,
				Amount:         depositAmount,
			}

			currentSupply, err := s.GetCurrentSupply(constants.PrimaryNetworkID)
			if err != nil {
				return err
			}

			offer, ok := depositOffers[deposit.DepositOfferID]
			if !ok {
				return errNonExistingOffer
			}

			newCurrentSupply, err := math.Add64(currentSupply, deposit.TotalReward(offer))
			if err != nil {
				return err
			}

			s.SetCurrentSupply(constants.PrimaryNetworkID, newCurrentSupply)
			cs.SetDeposit(depositTxID, deposit)
			s.AddTx(tx, status.Committed)
		}

		height := uint64(blockIndex) + 1 // +1 because 0-block is commit block from avax syncGenesis
		genesisBlock, err := blocks.NewBanffStandardBlock(
			block.Time(),
			s.GetLastAccepted(), // must be not empty
			height,
			block.Txs(),
		)
		if err != nil {
			return err
		}

		s.AddStatelessBlock(genesisBlock, choices.Accepted)
		s.SetLastAccepted(genesisBlock.ID())
		s.SetHeight(height)

		if err := s.write(false, height); err != nil {
			return err
		}
	}

	return nil
}

func (cs *caminoState) Load(s *state) error {
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

	errs := wrappers.Errs{}
	errs.Add(
		cs.loadDepositOffers(),
		cs.loadDeposits(),
		cs.loadDeferredValidators(s),
	)
	return errs.Err
}

func (cs *caminoState) Write() error {
	errs := wrappers.Errs{}
	// Write the singletons (only once after sync)
	if cs.genesisSynced {
		errs.Add(
			database.PutBool(cs.caminoDB, nodeSignatureKey, cs.verifyNodeSignature),
			database.PutBool(cs.caminoDB, depositBondModeKey, cs.lockModeBondDeposit),
		)
	}
	errs.Add(
		cs.writeAddressStates(),
		cs.writeDepositOffers(),
		cs.writeDeposits(),
		cs.writeMultisigOwners(),
		cs.writeShortLinks(),
		cs.writeClaimableAndValidatorRewards(),
		cs.writeDeferredStakers(),
	)
	return errs.Err
}

func (cs *caminoState) Close() error {
	errs := wrappers.Errs{}
	errs.Add(
		cs.caminoDB.Close(),
		cs.addressStateDB.Close(),
		cs.depositOffersDB.Close(),
		cs.depositsDB.Close(),
		cs.multisigOwnersDB.Close(),
		cs.shortLinksDB.Close(),
		cs.claimableDB.Close(),
		cs.deferredValidatorsDB.Close(),
	)
	return errs.Err
}

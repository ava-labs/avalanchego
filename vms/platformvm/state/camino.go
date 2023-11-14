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
	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/math"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/utils/wrappers"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/components/multisig"
	as "github.com/ava-labs/avalanchego/vms/platformvm/addrstate"
	"github.com/ava-labs/avalanchego/vms/platformvm/blocks"
	"github.com/ava-labs/avalanchego/vms/platformvm/config"
	"github.com/ava-labs/avalanchego/vms/platformvm/dac"
	"github.com/ava-labs/avalanchego/vms/platformvm/deposit"
	"github.com/ava-labs/avalanchego/vms/platformvm/genesis"
	"github.com/ava-labs/avalanchego/vms/platformvm/locked"
	"github.com/ava-labs/avalanchego/vms/platformvm/status"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/prometheus/client_golang/prometheus"
)

const (
	addressStateCacheSize = 1024
	depositsCacheSize     = 1024
	shortLinksCacheSize   = 1024
	msigOwnersCacheSize   = 16_384
	claimablesCacheSize   = 1024
	proposalsCacheSize    = 1024
)

var (
	_ CaminoState = (*caminoState)(nil)

	caminoPrefix               = []byte("camino")
	addressStatePrefix         = []byte("addressState")
	depositOffersPrefix        = []byte("depositOffers")
	depositsPrefix             = []byte("deposits")
	depositIDsByEndtimePrefix  = []byte("depositIDsByEndtime")
	multisigOwnersPrefix       = []byte("multisigOwners")
	shortLinksPrefix           = []byte("shortLinks")
	claimablesPrefix           = []byte("claimables")
	proposalsPrefix            = []byte("proposals")
	proposalIDsByEndtimePrefix = []byte("proposalIDsByEndtime")
	proposalIDsToFinishPrefix  = []byte("proposalIDsToFinish")

	// Used for prefixing the validatorsDB
	deferredPrefix = []byte("deferred")

	nodeSignatureKey                 = []byte("nodeSignature")
	depositBondModeKey               = []byte("depositBondMode")
	notDistributedValidatorRewardKey = []byte("notDistributedValidatorReward")
	baseFeeKey                       = []byte("baseFee")

	errWrongTxType      = errors.New("unexpected tx type")
	errNonExistingOffer = errors.New("deposit offer doesn't exist")
	errNotUniqueTx      = errors.New("not unique genesis tx")
)

type CaminoApply interface {
	ApplyCaminoState(State)
}

type CaminoDiff interface {
	// Singletones
	GetBaseFee() (uint64, error)
	SetBaseFee(uint64)

	// Address State

	SetAddressStates(ids.ShortID, as.AddressState)
	GetAddressStates(ids.ShortID) (as.AddressState, error)

	// Deposit offers

	SetDepositOffer(offer *deposit.Offer)
	GetDepositOffer(offerID ids.ID) (*deposit.Offer, error)
	GetAllDepositOffers() ([]*deposit.Offer, error)

	// Deposits

	// deposit should never be nil
	AddDeposit(depositTxID ids.ID, deposit *deposit.Deposit)
	// deposit start and duration should never be modified, deposit should never be nil
	ModifyDeposit(depositTxID ids.ID, deposit *deposit.Deposit)
	// deposit start and duration should never be modified, deposit should never be nil
	RemoveDeposit(depositTxID ids.ID, deposit *deposit.Deposit)
	GetDeposit(depositTxID ids.ID) (*deposit.Deposit, error)
	GetNextToUnlockDepositTime(removedDepositIDs set.Set[ids.ID]) (time.Time, error)
	GetNextToUnlockDepositIDsAndTime(removedDepositIDs set.Set[ids.ID]) ([]ids.ID, time.Time, error)

	// Multisig Owners

	GetMultisigAlias(ids.ShortID) (*multisig.AliasWithNonce, error)
	SetMultisigAlias(*multisig.AliasWithNonce)

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

	// DAC proposals and votes

	// proposal should never be nil
	AddProposal(proposalID ids.ID, proposal dac.ProposalState)
	// proposal start and end time should never be modified, proposal should never be nil
	ModifyProposal(proposalID ids.ID, proposal dac.ProposalState)
	// proposal start and end time should never be modified, proposal should never be nil
	RemoveProposal(proposalID ids.ID, proposal dac.ProposalState)
	GetProposal(proposalID ids.ID) (dac.ProposalState, error)
	GetProposalIterator() (ProposalsIterator, error)
	AddProposalIDToFinish(proposalID ids.ID)
	GetProposalIDsToFinish() ([]ids.ID, error)
	RemoveProposalIDToFinish(ids.ID)
	GetNextProposalExpirationTime(removedProposalIDs set.Set[ids.ID]) (time.Time, error)
	GetNextToExpireProposalIDsAndTime(removedProposalIDs set.Set[ids.ID]) ([]ids.ID, time.Time, error)
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
	modifiedAddressStates                 map[ids.ShortID]as.AddressState
	modifiedDepositOffers                 map[ids.ID]*deposit.Offer
	modifiedDeposits                      map[ids.ID]*depositDiff
	modifiedMultisigAliases               map[ids.ShortID]*multisig.AliasWithNonce
	modifiedShortLinks                    map[ids.ID]*ids.ShortID
	modifiedClaimables                    map[ids.ID]*Claimable
	modifiedProposals                     map[ids.ID]*proposalDiff
	modifiedProposalIDsToFinish           map[ids.ID]bool
	modifiedNotDistributedValidatorReward *uint64
	modifiedBaseFee                       *uint64
}

type caminoState struct {
	*caminoDiff

	caminoDB            database.Database
	genesisSynced       bool
	verifyNodeSignature bool
	lockModeBondDeposit bool
	baseFee             uint64

	// Deferred Stakers
	deferredStakers       *baseStakers
	deferredValidatorsDB  database.Database
	deferredValidatorList linkeddb.LinkedDB

	// Address State
	addressStateCache cache.Cacher[ids.ShortID, as.AddressState]
	addressStateDB    database.Database

	// Deposit offers
	depositOffers   map[ids.ID]*deposit.Offer
	depositOffersDB database.Database

	// Deposits
	depositsNextToUnlockTime *time.Time
	depositsNextToUnlockIDs  []ids.ID
	depositsCache            cache.Cacher[ids.ID, *deposit.Deposit]
	depositsDB               database.Database
	depositIDsByEndtimeDB    database.Database

	// MSIG aliases
	multisigAliasesCache cache.Cacher[ids.ShortID, *multisig.AliasWithNonce]
	multisigAliasesDB    database.Database

	// ShortIDs link
	shortLinksCache cache.Cacher[ids.ID, *ids.ShortID]
	shortLinksDB    database.Database

	//  Claimables
	notDistributedValidatorReward uint64
	claimablesDB                  database.Database
	claimablesCache               cache.Cacher[ids.ID, *Claimable]

	proposalsNextExpirationTime *time.Time
	proposalsNextToExpireIDs    []ids.ID
	proposalIDsToFinish         []ids.ID
	proposalsCache              cache.Cacher[ids.ID, dac.ProposalState]
	proposalsDB                 database.Database
	proposalIDsByEndtimeDB      database.Database
	proposalIDsToFinishDB       database.Database
}

func newCaminoDiff() *caminoDiff {
	return &caminoDiff{
		modifiedAddressStates:       make(map[ids.ShortID]as.AddressState),
		modifiedDepositOffers:       make(map[ids.ID]*deposit.Offer),
		modifiedDeposits:            make(map[ids.ID]*depositDiff),
		modifiedMultisigAliases:     make(map[ids.ShortID]*multisig.AliasWithNonce),
		modifiedShortLinks:          make(map[ids.ID]*ids.ShortID),
		modifiedClaimables:          make(map[ids.ID]*Claimable),
		modifiedProposals:           make(map[ids.ID]*proposalDiff),
		modifiedProposalIDsToFinish: make(map[ids.ID]bool),
	}
}

func newCaminoState(baseDB, validatorsDB database.Database, metricsReg prometheus.Registerer) (*caminoState, error) {
	addressStateCache, err := metercacher.New[ids.ShortID, as.AddressState](
		"address_state_cache",
		metricsReg,
		&cache.LRU[ids.ShortID, as.AddressState]{Size: addressStateCacheSize},
	)
	if err != nil {
		return nil, err
	}

	depositsCache, err := metercacher.New[ids.ID, *deposit.Deposit](
		"deposits_cache",
		metricsReg,
		&cache.LRU[ids.ID, *deposit.Deposit]{Size: depositsCacheSize},
	)
	if err != nil {
		return nil, err
	}

	shortLinksCache, err := metercacher.New[ids.ID, *ids.ShortID](
		"short_links_cache",
		metricsReg,
		&cache.LRU[ids.ID, *ids.ShortID]{Size: shortLinksCacheSize},
	)
	if err != nil {
		return nil, err
	}

	multisigOwnersCache, err := metercacher.New[ids.ShortID, *multisig.AliasWithNonce](
		"msig_owners_cache",
		metricsReg,
		&cache.LRU[ids.ShortID, *multisig.AliasWithNonce]{Size: msigOwnersCacheSize},
	)
	if err != nil {
		return nil, err
	}

	claimablesCache, err := metercacher.New[ids.ID, *Claimable](
		"claimables_cache",
		metricsReg,
		&cache.LRU[ids.ID, *Claimable]{Size: claimablesCacheSize},
	)
	if err != nil {
		return nil, err
	}

	proposalsCache, err := metercacher.New[ids.ID, dac.ProposalState](
		"proposals_cache",
		metricsReg,
		&cache.LRU[ids.ID, dac.ProposalState]{Size: proposalsCacheSize},
	)
	if err != nil {
		return nil, err
	}

	deferredValidatorsDB := prefixdb.New(deferredPrefix, validatorsDB)

	return &caminoState{
		// Address State
		addressStateDB:    prefixdb.New(addressStatePrefix, baseDB),
		addressStateCache: addressStateCache,

		// Deposit offers
		depositOffers:   make(map[ids.ID]*deposit.Offer),
		depositOffersDB: prefixdb.New(depositOffersPrefix, baseDB),

		// Deposits
		depositsCache:         depositsCache,
		depositsDB:            prefixdb.New(depositsPrefix, baseDB),
		depositIDsByEndtimeDB: prefixdb.New(depositIDsByEndtimePrefix, baseDB),

		// Multisig Owners
		multisigAliasesCache: multisigOwnersCache,
		multisigAliasesDB:    prefixdb.New(multisigOwnersPrefix, baseDB),

		// Short links
		shortLinksCache: shortLinksCache,
		shortLinksDB:    prefixdb.New(shortLinksPrefix, baseDB),

		//  Claimable & rewards
		claimablesCache: claimablesCache,
		claimablesDB:    prefixdb.New(claimablesPrefix, baseDB),

		// Deferred Stakers
		deferredStakers:       newBaseStakers(),
		deferredValidatorsDB:  deferredValidatorsDB,
		deferredValidatorList: linkeddb.NewDefault(deferredValidatorsDB),

		proposalsCache:         proposalsCache,
		proposalsDB:            prefixdb.New(proposalsPrefix, baseDB),
		proposalIDsByEndtimeDB: prefixdb.New(proposalIDsByEndtimePrefix, baseDB),
		proposalIDsToFinishDB:  prefixdb.New(proposalIDsToFinishPrefix, baseDB),

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
		initalAdminAddressState|as.AddressStateRoleAdmin)

	addrStateTx, err := txs.NewSigned(&txs.AddressStateTx{
		Address: g.Camino.InitialAdmin,
		State:   as.AddressStateBitRoleAdmin,
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
		cs.SetDepositOffer(offer)
	}

	// adding msig aliases

	for _, multisigAlias := range g.Camino.MultisigAliases {
		cs.SetMultisigAlias(&multisig.AliasWithNonce{Alias: *multisigAlias})
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
				RewardOwner:    depositTx.RewardsOwner,
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
			cs.AddDeposit(depositTxID, deposit)
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

	baseFee, err := database.GetUInt64(cs.caminoDB, baseFeeKey)
	if err == database.ErrNotFound {
		// if baseFee is not in db yet, than its first time when we access it
		// and it should be equal to config base fee
		config, err := s.Config()
		if err != nil {
			return err
		}
		baseFee = config.TxFee
	} else if err != nil {
		return err
	}
	cs.baseFee = baseFee

	errs := wrappers.Errs{}
	errs.Add(
		cs.loadDepositOffers(),
		cs.loadDeposits(),
		cs.loadValidatorRewards(),
		cs.loadDeferredValidators(s),
		cs.loadProposals(),
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
		database.PutUInt64(cs.caminoDB, baseFeeKey, cs.baseFee),
		cs.writeAddressStates(),
		cs.writeDepositOffers(),
		cs.writeDeposits(),
		cs.writeMultisigAliases(),
		cs.writeShortLinks(),
		cs.writeClaimableAndValidatorRewards(),
		cs.writeDeferredStakers(),
		cs.writeProposals(),
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
		cs.depositIDsByEndtimeDB.Close(),
		cs.multisigAliasesDB.Close(),
		cs.shortLinksDB.Close(),
		cs.claimablesDB.Close(),
		cs.deferredValidatorsDB.Close(),
		cs.proposalsDB.Close(),
		cs.proposalIDsByEndtimeDB.Close(),
		cs.proposalIDsToFinishDB.Close(),
	)
	return errs.Err
}

func (cs *caminoState) GetBaseFee() (uint64, error) {
	return cs.baseFee, nil
}

func (cs *caminoState) SetBaseFee(baseFee uint64) {
	cs.baseFee = baseFee
}

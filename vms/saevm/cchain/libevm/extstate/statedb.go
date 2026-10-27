// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package extstate extends [state.StateDB] with the C-Chain's imported X-Chain
// asset balances and access-list predicates.
package extstate

import (
	"math/big"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/state"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/libevm"
	"github.com/ava-labs/libevm/libevm/stateconf"
	"github.com/holiman/uint256"

	"github.com/ava-labs/avalanchego/graft/coreth/params"
	"github.com/ava-labs/avalanchego/graft/coreth/plugin/evm/customtypes"
	"github.com/ava-labs/avalanchego/graft/coreth/precompile/contract"
	"github.com/ava-labs/avalanchego/vms/evm/predicate"
)

// RegisterExtras registers the libevm hook that partitions state slots into
// storage and imported X-Chain asset balances.
//
// Callers SHOULD NOT call it directly, and SHOULD instead use
// `vms/saevm/cchain/libevm.RegisterExtras()`, which registers all
// C-Chain extras together.
func RegisterExtras() {
	state.RegisterExtras(partitionStateKeysHook{})
}

// WithTempRegisteredExtras runs fn with the state partition hook temporarily
// registered. Any previous hook is restored after fn returns.
//
// Callers SHOULD NOT call it directly, and SHOULD instead use
// `vms/saevm/cchain/libevm.WithTempRegisteredExtras()`, which registers all
// C-Chain extras together.
func WithTempRegisteredExtras(lock libevm.ExtrasLock, fn func() error) error {
	return state.WithTempRegisteredExtras(lock, partitionStateKeysHook{}, fn)
}

type partitionStateKeysHook struct{}

// TransformStateKey moves key into the contract storage partition.
func (partitionStateKeysHook) TransformStateKey(_ common.Address, key common.Hash) common.Hash {
	return storageKey(key)
}

// storageKey returns key moved into the contract storage partition.
func storageKey(key common.Hash) common.Hash {
	key[0] &^= 0x01
	return key
}

// assetBalanceKey returns the key of assetID's balance in the asset partition.
func assetBalanceKey(assetID common.Hash) common.Hash {
	assetID[0] |= 0x01
	return assetID
}

var _ contract.StateDB = (*StateDB)(nil)

// StateDB wraps a [state.StateDB] with asset balances and the predicates of the
// current transaction's access list.
//
// TODO(#6113): Share with Subnet-EVM rather than duplicating it.
type StateDB struct {
	*state.StateDB

	// predicates is set by [StateDB.Prepare] and not modified during execution.
	predicates map[common.Address][]predicate.Predicate
}

// New returns a [StateDB] that wraps db.
func New(db *state.StateDB) *StateDB {
	return &StateDB{StateDB: db}
}

// Prepare calls [state.StateDB.Prepare] and records the predicates in list for
// [StateDB.GetPredicate].
func (s *StateDB) Prepare(
	rules params.Rules,
	sender common.Address,
	coinbase common.Address,
	dst *common.Address,
	precompiles []common.Address,
	list types.AccessList,
) {
	s.predicates = predicate.FromAccessList(params.GetRulesExtra(rules), list)
	s.StateDB.Prepare(rules, sender, coinbase, dst, precompiles, list)
}

// GetPredicate returns address's index-th predicate in the access list passed
// to [StateDB.Prepare]. It returns false if address has no predicate at index.
func (s *StateDB) GetPredicate(address common.Address, index int) (predicate.Predicate, bool) {
	predicates := s.predicates[address]
	if index < 0 || index >= len(predicates) {
		return nil, false
	}
	return predicates[index], true
}

// GetAssetBalance returns the balance of assetID held by addr.
func (s *StateDB) GetAssetBalance(addr common.Address, assetID common.Hash) *big.Int {
	return s.GetState(addr, assetBalanceKey(assetID), stateconf.SkipStateKeyTransformation()).Big()
}

// AddAssetBalance adds amount of assetID to the balance of addr. A non-zero
// amount marks addr as having held an X-chain asset.
//
// amount MUST NOT be negative.
func (s *StateDB) AddAssetBalance(addr common.Address, assetID common.Hash, amount *big.Int) {
	// TODO(JonathanOppenheimer): Investigate whether any historical block ever
	// passed a zero amount to this method or [StateDB.SubAssetBalance] in a way
	// that affected the state root, and if not, remove both special cases.
	// NativeAssetCall is disabled at tip, so only past blocks matter.
	if amount.Sign() == 0 {
		// Like [state.StateDB.AddBalance], adding zero still touches the
		// account, which can make it eligible for EIP-161 deletion.
		s.AddBalance(addr, new(uint256.Int))
		return
	}

	if !customtypes.HeldXChainAsset(s.StateDB, addr) {
		customtypes.MarkHeldXChainAsset(s.StateDB, addr)
	}
	balance := s.GetAssetBalance(addr, assetID)
	balance.Add(balance, amount)
	s.setAssetBalance(addr, assetID, balance)
}

// SubAssetBalance subtracts amount of assetID from the balance of addr.
//
// amount MUST NOT be negative and MUST NOT exceed the balance.
func (s *StateDB) SubAssetBalance(addr common.Address, assetID common.Hash, amount *big.Int) {
	if amount.Sign() == 0 {
		// Like [state.stateObject.SubBalance], subtracting zero is a no-op.
		return
	}
	// The balance is positive, so [StateDB.AddAssetBalance] has already
	// marked addr as having held an X-chain asset.
	balance := s.GetAssetBalance(addr, assetID)
	balance.Sub(balance, amount)
	s.setAssetBalance(addr, assetID, balance)
}

func (s *StateDB) setAssetBalance(addr common.Address, assetID common.Hash, amount *big.Int) {
	s.SetState(addr, assetBalanceKey(assetID), common.BigToHash(amount), stateconf.SkipStateKeyTransformation())
}

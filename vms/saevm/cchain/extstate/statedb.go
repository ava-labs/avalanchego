// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package extstate extends [state.StateDB] with the C-Chain's multicoin
// balances and access-list predicates.
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
	"github.com/ava-labs/avalanchego/vms/evm/predicate"
)

// RegisterExtras registers the libevm hook that normalizes state keys, so that
// they never collide with multicoin balances. Keys are normalized unless
// [stateconf.SkipStateKeyTransformation] is used.
//
// It MUST NOT be called more than once and is therefore only allowed in tests
// and `package main`, to avoid polluting other packages that transitively
// depend on this one but don't need registration.
func RegisterExtras() {
	state.RegisterExtras(normalizeStateKeysHook{})
}

// WithTempRegisteredExtras runs fn with the state-key normalization hook
// temporarily registered. The previous hook is restored after fn returns.
//
// It MUST NOT be used on a live chain. Callers SHOULD NOT call it directly, and
// SHOULD instead use `vms/saevm/cchain/libevm.WithTempRegisteredExtras()`,
// which registers all C-Chain extras together.
func WithTempRegisteredExtras(lock libevm.ExtrasLock, fn func() error) error {
	return state.WithTempRegisteredExtras(lock, normalizeStateKeysHook{}, fn)
}

type normalizeStateKeysHook struct{}

// TransformStateKey maps normal storage keys away from the multicoin partition.
func (normalizeStateKeysHook) TransformStateKey(_ common.Address, key common.Hash) common.Hash {
	normalizeStateKey(&key)
	return key
}

// normalizeStateKey sets the 0th bit of the first byte in `key` to 0.
// This partitions normal state storage from multicoin storage.
func normalizeStateKey(key *common.Hash) {
	key[0] &^= 0x01
}

// normalizeCoinID sets the 0th bit of the first byte in `coinID` to 1.
// This partitions multicoin storage from normal state storage.
func normalizeCoinID(coinID *common.Hash) {
	coinID[0] |= 0x01
}

// TODO(JonathanOppenheimer): Share the predicate handling with Subnet-EVM's
// `core/extstate`.

// StateDB wraps a [state.StateDB] with multicoin balances and the predicates of
// the current transaction's access list.
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

// GetPredicate returns the predicate of the index-th access tuple for address
// in the list passed to [StateDB.Prepare]. It returns false if there is no such
// predicate.
func (s *StateDB) GetPredicate(address common.Address, index int) (predicate.Predicate, bool) {
	predicates := s.predicates[address]
	if index < 0 || index >= len(predicates) {
		return nil, false
	}
	return predicates[index], true
}

// GetBalanceMultiCoin returns the balance of coinID held by addr.
func (s *StateDB) GetBalanceMultiCoin(addr common.Address, coinID common.Hash) *big.Int {
	normalizeCoinID(&coinID)
	return s.GetState(addr, coinID, stateconf.SkipStateKeyTransformation()).Big()
}

// AddBalanceMultiCoin adds amount of coinID to the balance of addr. A non-zero
// amount marks addr as a multicoin account.
//
// amount MUST NOT be negative.
func (s *StateDB) AddBalanceMultiCoin(addr common.Address, coinID common.Hash, amount *big.Int) {
	if amount.Sign() == 0 {
		// Like [state.StateDB.AddBalance], adding zero still touches the
		// account, which can make it eligible for EIP-161 deletion.
		s.AddBalance(addr, new(uint256.Int))
		return
	}

	if !customtypes.IsMultiCoin(s.StateDB, addr) {
		customtypes.SetMultiCoin(s.StateDB, addr, true)
	}
	s.setBalanceMultiCoin(addr, coinID, new(big.Int).Add(s.GetBalanceMultiCoin(addr, coinID), amount))
}

// SubBalanceMultiCoin subtracts amount of coinID from the balance of addr.
//
// amount MUST NOT be negative and MUST NOT exceed the balance.
func (s *StateDB) SubBalanceMultiCoin(addr common.Address, coinID common.Hash, amount *big.Int) {
	if amount.Sign() == 0 {
		return
	}
	// The balance is positive, so [StateDB.AddBalanceMultiCoin] has already
	// marked addr as a multicoin account.
	s.setBalanceMultiCoin(addr, coinID, new(big.Int).Sub(s.GetBalanceMultiCoin(addr, coinID), amount))
}

func (s *StateDB) setBalanceMultiCoin(addr common.Address, coinID common.Hash, amount *big.Int) {
	normalizeCoinID(&coinID)
	s.SetState(addr, coinID, common.BigToHash(amount), stateconf.SkipStateKeyTransformation())
}

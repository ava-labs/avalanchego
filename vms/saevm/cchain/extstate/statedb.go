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
	"github.com/ava-labs/libevm/libevm/stateconf"
	"github.com/holiman/uint256"

	"github.com/ava-labs/avalanchego/graft/coreth/params"
	"github.com/ava-labs/avalanchego/vms/evm/predicate"
	"github.com/ava-labs/avalanchego/vms/saevm/cchain/libevm/customtypes"
)

// StateDBHooks normalizes state keys so that they never collide with multicoin
// balances. Keys are normalized unless [stateconf.SkipStateKeyTransformation]
// is used.
//
// It MUST only be registered by `vms/saevm/cchain/libevm`, which registers it
// together with the C-Chain's other libevm extras.
type StateDBHooks struct{}

// TransformStateKey maps normal storage keys away from the multicoin partition.
func (StateDBHooks) TransformStateKey(_ common.Address, key common.Hash) common.Hash {
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

// StateDB wraps a [state.StateDB] with multicoin balances and the predicates of
// the current transaction's access list.
//
// TODO(#6113): Use the predicates in SAE and share them with Subnet-EVM.
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

// GetPredicate returns the predicate at index for address, from the access list
// passed to [StateDB.Prepare]. If address is a precompile that uses predicates,
// each of its entries in the list is one predicate, in list order. It returns
// false if there is none.
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
		// Like [state.StateDB.SubBalance], subtracting zero doesn't touch the
		// account.
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

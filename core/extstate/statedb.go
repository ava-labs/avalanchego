// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package extstate

import (
	"math/big"

	"github.com/ava-labs/avalanchego/vms/evm/predicate"
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/state"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/libevm/stateconf"
	"github.com/holiman/uint256"

	"github.com/ava-labs/coreth/params"
	"github.com/ava-labs/coreth/plugin/evm/customtypes"
)

// RegisterExtras registers hooks with libevm to achieve Avalanche state
// management. It MUST NOT be called more than once and therefore is only
// allowed to be used in tests and `package main`, to avoid polluting other
// packages that transitively depend on this one but don't need registration.
//
// Of note, a call to RegisterExtras will result in state-key normalization
// unless [stateconf.SkipStateKeyTransformation] is used.
func RegisterExtras() {
	state.RegisterExtras(normalizeStateKeysHook{})
}

type normalizeStateKeysHook struct{}

// TransformStateKey transforms all keys with [normalizeStateKey].
func (normalizeStateKeysHook) TransformStateKey(_ common.Address, key common.Hash) common.Hash {
	normalizeStateKey(&key)
	return key
}

type StateDB struct {
	*state.StateDB

	// Ordered storage slots to be used in predicate verification as set in the tx access list.
	// Only set in [StateDB.Prepare], and un-modified through execution.
	predicates map[common.Address][]predicate.Predicate
}

// New creates a new [StateDB] with the given [state.StateDB], wrapping it with
// additional functionality.
func New(vm *state.StateDB) *StateDB {
	return &StateDB{
		StateDB:    vm,
		predicates: make(map[common.Address][]predicate.Predicate),
	}
}

func (s *StateDB) Prepare(rules params.Rules, sender, coinbase common.Address, dst *common.Address, precompiles []common.Address, list types.AccessList) {
	rulesExtra := params.GetRulesExtra(rules)
	s.predicates = predicate.FromAccessList(rulesExtra, list)
	s.StateDB.Prepare(rules, sender, coinbase, dst, precompiles, list)
}

// GetPredicate returns the storage slots associated with the address, index pair.
// A list of access tuples can be included within transaction types post EIP-2930. The address
// is declared directly on the access tuple and the index is the i'th occurrence of an access
// tuple with the specified address.
//
// Ex. AccessList[[AddrA, Predicate1], [AddrB, Predicate2], [AddrA, Predicate3]]
// In this case, the caller could retrieve predicates 1-3 with the following calls:
// GetPredicate(AddrA, 0) -> Predicate1
// GetPredicate(AddrB, 0) -> Predicate2
// GetPredicate(AddrA, 1) -> Predicate3
func (s *StateDB) GetPredicate(address common.Address, index int) (predicate.Predicate, bool) {
	predicates, exists := s.predicates[address]
	if !exists || index >= len(predicates) {
		return nil, false
	}
	return predicates[index], true
}

// Retrieve the balance from the given address or 0 if object not found
func (s *StateDB) GetBalanceMultiCoin(addr common.Address, coinID common.Hash) *big.Int {
	normalizeCoinID(&coinID)
	return s.GetState(addr, coinID, stateconf.SkipStateKeyTransformation()).Big()
}

// AddBalance adds amount to the account associated with addr.
func (s *StateDB) AddBalanceMultiCoin(addr common.Address, coinID common.Hash, amount *big.Int) {
	if amount.Sign() == 0 {
		s.AddBalance(addr, new(uint256.Int)) // used to cause touch
		return
	}

	if !customtypes.IsMultiCoin(s.StateDB, addr) {
		customtypes.SetMultiCoin(s.StateDB, addr, true)
	}

	newAmount := new(big.Int).Add(s.GetBalanceMultiCoin(addr, coinID), amount)
	normalizeCoinID(&coinID)
	s.SetState(addr, coinID, common.BigToHash(newAmount), stateconf.SkipStateKeyTransformation())
}

// SubBalance subtracts amount from the account associated with addr.
func (s *StateDB) SubBalanceMultiCoin(addr common.Address, coinID common.Hash, amount *big.Int) {
	if amount.Sign() == 0 {
		return
	}
	// Note: It's not needed to set the IsMultiCoin (extras) flag here, as this
	// call would always be preceded by a call to AddBalanceMultiCoin, which would
	// set the extra flag. Seems we should remove the redundant code.
	if !customtypes.IsMultiCoin(s.StateDB, addr) {
		customtypes.SetMultiCoin(s.StateDB, addr, true)
	}
	newAmount := new(big.Int).Sub(s.GetBalanceMultiCoin(addr, coinID), amount)
	normalizeCoinID(&coinID)
	s.SetState(addr, coinID, common.BigToHash(newAmount), stateconf.SkipStateKeyTransformation())
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

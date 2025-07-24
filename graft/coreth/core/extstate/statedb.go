// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package extstate

import (
	"math/big"

	"github.com/ava-labs/coreth/params"
	"github.com/ava-labs/coreth/plugin/evm/customtypes"
	"github.com/ava-labs/coreth/predicate"
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/state"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/libevm/stateconf"
	"github.com/holiman/uint256"
)

// Register the state key normalization to libevm's [state.StateDB]. This will
// normalize all state keys passing through the implementation unless
// [stateconf.SkipStateKeyTransformation] is provided as an option.
func init() {
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
	predicateStorageSlots map[common.Address][][]byte
}

// New creates a new [StateDB] with the given [state.StateDB], wrapping it with
// additional functionality.
func New(vm *state.StateDB) *StateDB {
	return &StateDB{
		StateDB:               vm,
		predicateStorageSlots: make(map[common.Address][][]byte),
	}
}

func (s *StateDB) Prepare(rules params.Rules, sender, coinbase common.Address, dst *common.Address, precompiles []common.Address, list types.AccessList) {
	rulesExtra := params.GetRulesExtra(rules)
	s.predicateStorageSlots = predicate.PreparePredicateStorageSlots(rulesExtra, list)
	s.StateDB.Prepare(rules, sender, coinbase, dst, precompiles, list)
}

// GetPredicateStorageSlots returns the storage slots associated with the address, index pair.
// A list of access tuples can be included within transaction types post EIP-2930. The address
// is declared directly on the access tuple and the index is the i'th occurrence of an access
// tuple with the specified address.
//
// Ex. AccessList[[AddrA, Predicate1], [AddrB, Predicate2], [AddrA, Predicate3]]
// In this case, the caller could retrieve predicates 1-3 with the following calls:
// GetPredicateStorageSlots(AddrA, 0) -> Predicate1
// GetPredicateStorageSlots(AddrB, 0) -> Predicate2
// GetPredicateStorageSlots(AddrA, 1) -> Predicate3
func (s *StateDB) GetPredicateStorageSlots(address common.Address, index int) ([]byte, bool) {
	predicates, exists := s.predicateStorageSlots[address]
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

	if !state.GetExtra(s.StateDB, customtypes.IsMultiCoinPayloads, addr) {
		state.SetExtra(s.StateDB, customtypes.IsMultiCoinPayloads, addr, true)
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
	if !state.GetExtra(s.StateDB, customtypes.IsMultiCoinPayloads, addr) {
		state.SetExtra(s.StateDB, customtypes.IsMultiCoinPayloads, addr, true)
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

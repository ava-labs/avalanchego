// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package extstate

import (
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/state"
	"github.com/ava-labs/libevm/core/types"

	"github.com/ava-labs/subnet-evm/params"
	"github.com/ava-labs/subnet-evm/predicate"
)

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

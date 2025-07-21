// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package extstate

import (
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/core/vm"
	"github.com/ava-labs/subnet-evm/params"
	"github.com/ava-labs/subnet-evm/predicate"
)

type VmStateDB interface {
	vm.StateDB
	Logs() []*types.Log
	GetTxHash() common.Hash
}

type vmStateDB = VmStateDB

type StateDB struct {
	vmStateDB

	// Ordered storage slots to be used in predicate verification as set in the tx access list.
	// Only set in [StateDB.Prepare], and un-modified through execution.
	predicateStorageSlots map[common.Address][][]byte
}

// New creates a new [*StateDB] with the given [VmStateDB], effectively wrapping it
// with additional functionality.
func New(vm VmStateDB) *StateDB {
	return &StateDB{
		vmStateDB:             vm,
		predicateStorageSlots: make(map[common.Address][][]byte),
	}
}

func (s *StateDB) Prepare(rules params.Rules, sender, coinbase common.Address, dst *common.Address, precompiles []common.Address, list types.AccessList) {
	rulesExtra := params.GetRulesExtra(rules)
	s.predicateStorageSlots = predicate.PreparePredicateStorageSlots(rulesExtra, list)
	s.vmStateDB.Prepare(rules, sender, coinbase, dst, precompiles, list)
}

// GetLogData returns the underlying topics and data from each log included in the [StateDB].
// Test helper function.
func (s *StateDB) GetLogData() (topics [][]common.Hash, data [][]byte) {
	for _, log := range s.Logs() {
		topics = append(topics, log.Topics)
		data = append(data, common.CopyBytes(log.Data))
	}
	return topics, data
}

// GetPredicateStorageSlots returns the storage slots associated with the address+index pair as
// a byte slice as well as a boolean indicating if the address+index pair exists.
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

// SetPredicateStorageSlots sets the predicate storage slots for the given address
func (s *StateDB) SetPredicateStorageSlots(address common.Address, predicates [][]byte) {
	s.predicateStorageSlots[address] = predicates
}

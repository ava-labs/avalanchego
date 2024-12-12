// (c) 2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package extstate

import (
	"github.com/ava-labs/subnet-evm/core/types"
	"github.com/ava-labs/subnet-evm/params"
	"github.com/ava-labs/subnet-evm/predicate"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/vm"
)

type VmStateDB interface {
	vm.StateDB
	GetTxHash() common.Hash
	GetLogData() (topics [][]common.Hash, data [][]byte)
}

type StateDB struct {
	VmStateDB

	// Ordered storage slots to be used in predicate verification as set in the tx access list.
	// Only set in PrepareAccessList, and un-modified through execution.
	predicateStorageSlots map[common.Address][][]byte
}

func (s *StateDB) Prepare(rules params.Rules, sender, coinbase common.Address, dst *common.Address, precompiles []common.Address, list types.AccessList) {
	rulesExtra := params.GetRulesExtra(rules)
	s.predicateStorageSlots = predicate.PreparePredicateStorageSlots(rulesExtra, list)
	s.VmStateDB.Prepare(rules, sender, coinbase, dst, precompiles, list)
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
	if !exists {
		return nil, false
	}
	if index >= len(predicates) {
		return nil, false
	}
	return predicates[index], true
}

// SetPredicateStorageSlots sets the predicate storage slots for the given address
func (s *StateDB) SetPredicateStorageSlots(address common.Address, predicates [][]byte) {
	if s.predicateStorageSlots == nil {
		s.predicateStorageSlots = make(map[common.Address][][]byte)
	}
	s.predicateStorageSlots[address] = predicates
}

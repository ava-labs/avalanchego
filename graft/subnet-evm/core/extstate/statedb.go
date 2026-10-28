// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package extstate

import (
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/state"
	"github.com/ava-labs/libevm/core/types"

	"github.com/ava-labs/avalanchego/graft/subnet-evm/params"
	"github.com/ava-labs/avalanchego/vms/evm/predicate"
)

type StateDB struct {
	*state.StateDB

	// Ordered Warp messages as set in the tx access list.
	// Only set in [StateDB.Prepare], and un-modified through execution.
	warpMessages []predicate.Predicate
}

// New creates a new [StateDB] with the given [state.StateDB], wrapping it with
// additional functionality.
func New(vm *state.StateDB) *StateDB {
	return &StateDB{
		StateDB: vm,
	}
}

func (s *StateDB) Prepare(rules params.Rules, sender, coinbase common.Address, dst *common.Address, precompiles []common.Address, list types.AccessList) {
	s.warpMessages = params.GetRulesExtra(rules).WarpMessages(list)
	s.StateDB.Prepare(rules, sender, coinbase, dst, precompiles, list)
}

// GetWarpMessage returns the index-th Warp message in the access list passed
// to [StateDB.Prepare]. It returns false if there is no message at index.
func (s *StateDB) GetWarpMessage(index int) (predicate.Predicate, bool) {
	if index < 0 || index >= len(s.warpMessages) {
		return nil, false
	}
	return s.warpMessages[index], true
}

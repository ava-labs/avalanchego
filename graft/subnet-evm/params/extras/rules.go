// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package extras

import (
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/types"

	"github.com/ava-labs/avalanchego/graft/subnet-evm/precompile/contracts/warp"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/precompile/precompileconfig"
	"github.com/ava-labs/avalanchego/vms/evm/predicate"
)

type Rules struct {
	// Rules for Avalanche releases
	AvalancheRules

	// Precompiles maps addresses to stateful precompiled contracts that are enabled
	// for this rule set.
	// Note: none of these addresses should conflict with the address space used by
	// any existing precompiles.
	Precompiles map[common.Address]precompileconfig.Config
	// WarpConfig is the active Warp precompile config, or nil if Warp is not
	// enabled for this rule set.
	WarpConfig *warp.Config
	// AccepterPrecompiles map addresses to stateful precompile accepter functions
	// that are enabled for this rule set.
	AccepterPrecompiles map[common.Address]precompileconfig.Accepter
}

// WarpMessages returns the storage keys of each Warp precompile entry in list,
// in order. Each entry is one Warp message, including entries without storage
// keys. It returns nil if Warp is not enabled.
func (r *Rules) WarpMessages(list types.AccessList) []predicate.Predicate {
	if r.WarpConfig == nil {
		return nil
	}
	var messages []predicate.Predicate
	for _, tuple := range list {
		if tuple.Address == warp.ContractAddress {
			messages = append(messages, tuple.StorageKeys)
		}
	}
	return messages
}

// IsPrecompileEnabled returns true if the precompile at `addr` is enabled for this rule set.
func (r *Rules) IsPrecompileEnabled(addr common.Address) bool {
	_, ok := r.Precompiles[addr]
	return ok
}

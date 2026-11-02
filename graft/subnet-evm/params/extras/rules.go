// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package extras

import (
	"github.com/ava-labs/libevm/common"

	"github.com/ava-labs/avalanchego/graft/subnet-evm/precompile/contracts/warp"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/precompile/precompileconfig"
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

// IsPrecompileEnabled returns true if the precompile at `addr` is enabled for this rule set.
func (r *Rules) IsPrecompileEnabled(addr common.Address) bool {
	_, ok := r.Precompiles[addr]
	return ok
}

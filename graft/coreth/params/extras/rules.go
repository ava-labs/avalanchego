// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package extras

import (
	"github.com/ava-labs/libevm/common"

	"github.com/ava-labs/avalanchego/graft/coreth/precompile/precompileconfig"
)

type Rules struct {
	// Rules for Avalanche releases
	AvalancheRules

	// Precompiles maps addresses to stateful precompiled contracts that are enabled
	// for this rule set.
	// Note: none of these addresses should conflict with the address space used by
	// any existing precompiles.
	Precompiles map[common.Address]precompileconfig.Config
	// Predicaters maps addresses to stateful precompile Predicaters
	// that are enabled for this rule set.
	Predicaters map[common.Address]precompileconfig.Predicater
}

func (r *Rules) PredicatersExist() bool {
	return len(r.Predicaters) > 0
}

// HasPredicate implements the avalanchego predicate.Predicates interface.
func (r *Rules) HasPredicate(addr common.Address) bool {
	_, ok := r.Predicaters[addr]
	return ok
}

// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package extras

import (
	"github.com/ava-labs/libevm/common"

	"github.com/ava-labs/avalanchego/graft/evm/precompile/precompileconfig"
)

var _ precompileconfig.Rules = AvalancheRules{}

// AvalancheRules tracks which Avalanche network upgrades are active at a given
// block timestamp.
type AvalancheRules struct {
	IsApricotPhase1     bool
	IsApricotPhase2     bool
	IsApricotPhase3     bool
	IsApricotPhase4     bool
	IsApricotPhase5     bool
	IsApricotPhasePre6  bool
	IsApricotPhase6     bool
	IsApricotPhasePost6 bool
	IsBanff             bool
	// IsCortina also corresponds to IsSubnetEVM for Subnet-EVM chains.
	IsCortina bool

	// Shared upgrades
	IsDurango bool
	IsEtna    bool
	IsFortuna bool
	IsGranite bool
	IsHelicon bool
}

// IsGraniteActivated implements precompileconfig.Rules.
func (a AvalancheRules) IsGraniteActivated() bool {
	return a.IsGranite
}

// IsDurangoActivated implements precompileconfig.Rules.
func (a AvalancheRules) IsDurangoActivated() bool {
	return a.IsDurango
}

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
	// AccepterPrecompiles map addresses to stateful precompile accepter functions
	// that are enabled for this rule set.
	AccepterPrecompiles map[common.Address]precompileconfig.Accepter
}

func (r *Rules) PredicatersExist() bool {
	return len(r.Predicaters) > 0
}

// HasPredicate implements the avalanchego predicate.Predicates interface.
func (r *Rules) HasPredicate(addr common.Address) bool {
	_, ok := r.Predicaters[addr]
	return ok
}

// IsPrecompileEnabled returns true if the precompile at `addr` is enabled for this rule set.
func (r *Rules) IsPrecompileEnabled(addr common.Address) bool {
	_, ok := r.Precompiles[addr]
	return ok
}

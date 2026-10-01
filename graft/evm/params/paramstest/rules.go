// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package paramstest provides test helpers for the shared EVM params.
package paramstest

import (
	"github.com/ava-labs/avalanchego/graft/evm/params"
	"github.com/ava-labs/avalanchego/upgrade/upgradetest"
)

// ForkToAvalancheRules returns the rules with every upgrade up to and including
// `fork` active.
func ForkToAvalancheRules(fork upgradetest.Fork) params.AvalancheRules {
	var rules params.AvalancheRules
	switch fork {
	case upgradetest.Igloo:
		rules.IsIgloo = true
		fallthrough
	case upgradetest.Helicon:
		rules.IsHelicon = true
		fallthrough
	case upgradetest.Granite:
		rules.IsGranite = true
		fallthrough
	case upgradetest.Fortuna:
		rules.IsFortuna = true
		fallthrough
	case upgradetest.Etna:
		rules.IsEtna = true
		fallthrough
	case upgradetest.Durango:
		rules.IsDurango = true
		fallthrough
	case upgradetest.Cortina:
		rules.IsCortina = true
		fallthrough
	case upgradetest.Banff:
		rules.IsBanff = true
		fallthrough
	case upgradetest.ApricotPhasePost6:
		rules.IsApricotPhasePost6 = true
		fallthrough
	case upgradetest.ApricotPhase6:
		rules.IsApricotPhase6 = true
		fallthrough
	case upgradetest.ApricotPhasePre6:
		rules.IsApricotPhasePre6 = true
		fallthrough
	case upgradetest.ApricotPhase5:
		rules.IsApricotPhase5 = true
		fallthrough
	case upgradetest.ApricotPhase4:
		rules.IsApricotPhase4 = true
		fallthrough
	case upgradetest.ApricotPhase3:
		rules.IsApricotPhase3 = true
		fallthrough
	case upgradetest.ApricotPhase2:
		rules.IsApricotPhase2 = true
		fallthrough
	case upgradetest.ApricotPhase1:
		rules.IsApricotPhase1 = true
	}
	return rules
}

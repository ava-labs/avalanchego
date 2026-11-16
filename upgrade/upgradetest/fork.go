// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package upgradetest

import "github.com/ava-labs/avalanchego/upgrade"

// Fork is an alias of [upgrade.Upgrade] to reduce the diff when introducing the
// latter.
//
// TODO(arr4n): remove in a dedicated follow-up PR.
type Fork = upgrade.Upgrade

const (
	NoUpgrades        = upgrade.NoUpgrades
	ApricotPhase1     = upgrade.ApricotPhase1
	ApricotPhase2     = upgrade.ApricotPhase2
	ApricotPhase3     = upgrade.ApricotPhase3
	ApricotPhase4     = upgrade.ApricotPhase4
	ApricotPhase5     = upgrade.ApricotPhase5
	ApricotPhasePre6  = upgrade.ApricotPhasePre6
	ApricotPhase6     = upgrade.ApricotPhase6
	ApricotPhasePost6 = upgrade.ApricotPhasePost6
	Banff             = upgrade.Banff
	Cortina           = upgrade.Cortina
	Durango           = upgrade.Durango
	Etna              = upgrade.Etna
	Fortuna           = upgrade.Fortuna
	Granite           = upgrade.Granite
	Helicon           = upgrade.Helicon
	Igloo             = upgrade.Igloo
	Latest            = upgrade.Latest
)

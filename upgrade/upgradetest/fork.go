// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package upgradetest

const (
	NoUpgrades Fork = iota
	ApricotPhase1
	ApricotPhase2
	ApricotPhase3
	ApricotPhase4
	ApricotPhase5
	ApricotPhasePre6
	ApricotPhase6
	ApricotPhasePost6
	Banff
	Cortina
	Durango
	Etna

	Latest = Etna
)

type Fork int

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

// Fork is an enum of all the major network upgrades.
type Fork int

func (f Fork) String() string {
	switch fork {
	case Etna:
		c.EtnaTime = upgradeTime
		fallthrough
	case Durango:
		c.DurangoTime = upgradeTime
		fallthrough
	case Cortina:
		c.CortinaTime = upgradeTime
		fallthrough
	case Banff:
		c.BanffTime = upgradeTime
		fallthrough
	case ApricotPhasePost6:
		c.ApricotPhasePost6Time = upgradeTime
		fallthrough
	case ApricotPhase6:
		c.ApricotPhase6Time = upgradeTime
		fallthrough
	case ApricotPhasePre6:
		c.ApricotPhasePre6Time = upgradeTime
		fallthrough
	case ApricotPhase5:
		c.ApricotPhase5Time = upgradeTime
		fallthrough
	case ApricotPhase4:
		c.ApricotPhase4Time = upgradeTime
		fallthrough
	case ApricotPhase3:
		c.ApricotPhase3Time = upgradeTime
		fallthrough
	case ApricotPhase2:
		c.ApricotPhase2Time = upgradeTime
		fallthrough
	case ApricotPhase1:
		c.ApricotPhase1Time = upgradeTime
	}
}

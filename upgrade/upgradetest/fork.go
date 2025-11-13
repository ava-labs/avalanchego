// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package upgradetest

import (
	"time"

	"github.com/ava-labs/avalanchego/upgrade"
)

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
	Fortuna
	Granite
	Helicon

	Latest = iota - 1
)

// Fork is an enum of all the major network upgrades.
type Fork int

func (f Fork) String() string {
	switch f {
	case Helicon:
		return "Helicon"
	case Granite:
		return "Granite"
	case Fortuna:
		return "Fortuna"
	case Etna:
		return "Etna"
	case Durango:
		return "Durango"
	case Cortina:
		return "Cortina"
	case Banff:
		return "Banff"
	case ApricotPhasePost6:
		return "ApricotPhasePost6"
	case ApricotPhase6:
		return "ApricotPhase6"
	case ApricotPhasePre6:
		return "ApricotPhasePre6"
	case ApricotPhase5:
		return "ApricotPhase5"
	case ApricotPhase4:
		return "ApricotPhase4"
	case ApricotPhase3:
		return "ApricotPhase3"
	case ApricotPhase2:
		return "ApricotPhase2"
	case ApricotPhase1:
		return "ApricotPhase1"
	case NoUpgrades:
		return "NoUpgrades"
	default:
		return "Unknown"
	}
}

// FromString returns the Fork constant for the given name, or -1 if not found.
func FromString(name string) Fork {
	switch name {
	case "Helicon":
		return Helicon
	case "Granite":
		return Granite
	case "Fortuna":
		return Fortuna
	case "Etna":
		return Etna
	case "Durango":
		return Durango
	case "Cortina":
		return Cortina
	case "Banff":
		return Banff
	case "ApricotPhasePost6":
		return ApricotPhasePost6
	case "ApricotPhase6":
		return ApricotPhase6
	case "ApricotPhasePre6":
		return ApricotPhasePre6
	case "ApricotPhase5":
		return ApricotPhase5
	case "ApricotPhase4":
		return ApricotPhase4
	case "ApricotPhase3":
		return ApricotPhase3
	case "ApricotPhase2":
		return ApricotPhase2
	case "ApricotPhase1":
		return ApricotPhase1
	case "NoUpgrades":
		return NoUpgrades
	default:
		return -1
	}
}

func GetActivationTime(fork Fork, networkID uint32) time.Time {
	switch fork {
	case Helicon:
		return upgrade.Configs[networkID].HeliconTime
	case Granite:
		return upgrade.Configs[networkID].GraniteTime
	case Fortuna:
		return upgrade.Configs[networkID].FortunaTime
	case Etna:
		return upgrade.Configs[networkID].EtnaTime
	case Durango:
		return upgrade.Configs[networkID].DurangoTime
	case Cortina:
		return upgrade.Configs[networkID].CortinaTime
	case Banff:
		return upgrade.Configs[networkID].BanffTime
	case ApricotPhasePost6:
		return upgrade.Configs[networkID].ApricotPhasePost6Time
	case ApricotPhase6:
		return upgrade.Configs[networkID].ApricotPhase6Time
	case ApricotPhasePre6:
		return upgrade.Configs[networkID].ApricotPhasePre6Time
	case ApricotPhase5:
		return upgrade.Configs[networkID].ApricotPhase5Time
	case ApricotPhase4:
		return upgrade.Configs[networkID].ApricotPhase4Time
	case ApricotPhase3:
		return upgrade.Configs[networkID].ApricotPhase3Time
	case ApricotPhase2:
		return upgrade.Configs[networkID].ApricotPhase2Time
	case ApricotPhase1:
		return upgrade.Configs[networkID].ApricotPhase1Time
	default:
		return time.Time{}
	}
}

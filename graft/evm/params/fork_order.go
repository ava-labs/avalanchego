// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package params

import (
	"errors"
	"fmt"
	"slices"
)

// ErrUnsupportedForkOrdering is returned by [NetworkUpgrades.CheckForkOrder].
var ErrUnsupportedForkOrdering = errors.New("unsupported fork ordering")

// An Upgrade names one of the [NetworkUpgrades].
type Upgrade string

const (
	ApricotPhase1     Upgrade = "ApricotPhase1"
	ApricotPhase2     Upgrade = "ApricotPhase2"
	ApricotPhase3     Upgrade = "ApricotPhase3"
	ApricotPhase4     Upgrade = "ApricotPhase4"
	ApricotPhase5     Upgrade = "ApricotPhase5"
	ApricotPhasePre6  Upgrade = "ApricotPhasePre6"
	ApricotPhase6     Upgrade = "ApricotPhase6"
	ApricotPhasePost6 Upgrade = "ApricotPhasePost6"
	Banff             Upgrade = "Banff"
	Cortina           Upgrade = "Cortina"
	Durango           Upgrade = "Durango"
	Etna              Upgrade = "Etna"
	Fortuna           Upgrade = "Fortuna"
	Granite           Upgrade = "Granite"
	Helicon           Upgrade = "Helicon"
	Igloo             Upgrade = "Igloo"
)

type fork struct {
	name      Upgrade
	timestamp *uint64
}

func (n *NetworkUpgrades) forkOrder() []fork {
	return []fork{
		{ApricotPhase1, n.ApricotPhase1BlockTimestamp},
		{ApricotPhase2, n.ApricotPhase2BlockTimestamp},
		{ApricotPhase3, n.ApricotPhase3BlockTimestamp},
		{ApricotPhase4, n.ApricotPhase4BlockTimestamp},
		{ApricotPhase5, n.ApricotPhase5BlockTimestamp},
		{ApricotPhasePre6, n.ApricotPhasePre6BlockTimestamp},
		{ApricotPhase6, n.ApricotPhase6BlockTimestamp},
		{ApricotPhasePost6, n.ApricotPhasePost6BlockTimestamp},
		{Banff, n.BanffBlockTimestamp},
		{Cortina, n.CortinaBlockTimestamp},
		{Durango, n.DurangoBlockTimestamp},
		{Etna, n.EtnaTimestamp},
		{Fortuna, n.FortunaTimestamp},
		{Granite, n.GraniteTimestamp},
		{Helicon, n.HeliconTimestamp},
		{Igloo, n.IglooTimestamp},
	}
}

// CheckForkOrder checks that the upgrades are scheduled in order. Every
// upgrade before the last scheduled one MUST also be scheduled, unless it is
// listed in `optional`, in which case it is ignored when unscheduled.
func (n *NetworkUpgrades) CheckForkOrder(optional ...Upgrade) error {
	var last *fork
	for _, cur := range n.forkOrder() {
		if cur.timestamp == nil && slices.Contains(optional, cur.name) {
			continue
		}
		if last != nil {
			switch {
			case last.timestamp == nil && cur.timestamp != nil:
				return fmt.Errorf("%w: %s not enabled, but %s enabled at timestamp %d",
					ErrUnsupportedForkOrdering, last.name, cur.name, *cur.timestamp)
			case last.timestamp != nil && cur.timestamp != nil && *last.timestamp > *cur.timestamp:
				return fmt.Errorf("%w: %s enabled at timestamp %d, but %s enabled at timestamp %d",
					ErrUnsupportedForkOrdering, last.name, *last.timestamp, cur.name, *cur.timestamp)
			}
		}
		last = &cur
	}
	return nil
}

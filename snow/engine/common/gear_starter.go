// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package common

import (
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils/math"
)

var _ GearStarter = &gearStarter{}

type GearStarter interface {
	AddWeightForNode(nodeID ids.ShortID) error
	RemoveWeightForNode(nodeID ids.ShortID) error
	CanStart() bool
	MarkStart()
}

func NewGearStarter(beacons validators.Set, startupAlpha uint64) GearStarter {
	return &gearStarter{
		beacons:      beacons,
		startupAlpha: startupAlpha,
	}
}

type gearStarter struct {
	beacons      validators.Set
	startupAlpha uint64
	weight       uint64
	canStart     bool
}

func (gs *gearStarter) AddWeightForNode(nodeID ids.ShortID) error {
	if gs.canStart {
		return nil
	}
	weight, ok := gs.beacons.GetWeight(nodeID)
	if !ok {
		return nil
	}
	weight, err := math.Add64(weight, gs.weight)
	if err != nil {
		return err
	}
	gs.weight = weight
	if gs.weight >= gs.startupAlpha {
		gs.canStart = true
	}
	return nil
}

func (gs *gearStarter) RemoveWeightForNode(nodeID ids.ShortID) error {
	if weight, ok := gs.beacons.GetWeight(nodeID); ok {
		// TODO: Account for weight changes in a more robust manner.

		// Sub64 should rarely error since only validators that have added their
		// weight can become disconnected. Because it is possible that there are
		// changes to the validators set, we utilize that Sub64 returns 0 on
		// error.
		gs.weight, _ = math.Sub64(gs.weight, weight)

		// TODO: shouldn't this be done?
		if gs.weight < gs.startupAlpha {
			gs.canStart = false
		}
	}
	return nil
}

func (gs *gearStarter) CanStart() bool { return gs.canStart }
func (gs *gearStarter) MarkStart()     { gs.canStart = true }

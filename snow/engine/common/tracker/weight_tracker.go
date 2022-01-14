// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package tracker

import (
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils/math"
)

var _ WeightTracker = &weightTracker{}

type WeightTracker interface {
	AddWeightForNode(nodeID ids.ShortID) error
	RemoveWeightForNode(nodeID ids.ShortID) error
	EnoughConnectedWeight() bool
}

func NewWeightTracker(beacons validators.Set, startupAlpha uint64) WeightTracker {
	return &weightTracker{
		beacons:      beacons,
		startupAlpha: startupAlpha,
	}
}

type weightTracker struct {
	beacons               validators.Set
	startupAlpha          uint64
	weight                uint64
	enoughConnectedWeight bool
}

func (wt *weightTracker) AddWeightForNode(nodeID ids.ShortID) error {
	if wt.enoughConnectedWeight {
		return nil
	}
	weight, ok := wt.beacons.GetWeight(nodeID)
	if !ok {
		return nil
	}
	weight, err := math.Add64(weight, wt.weight)
	if err != nil {
		return err
	}
	wt.weight = weight
	if wt.weight >= wt.startupAlpha {
		wt.enoughConnectedWeight = true
	}
	return nil
}

func (wt *weightTracker) RemoveWeightForNode(nodeID ids.ShortID) error {
	if weight, ok := wt.beacons.GetWeight(nodeID); ok {
		// TODO: Account for weight changes in a more robust manner.

		// Sub64 should rarely error since only validators that have added their
		// weight can become disconnected. Because it is possible that there are
		// changes to the validators set, we utilize that Sub64 returns 0 on
		// error.
		wt.weight, _ = math.Sub64(wt.weight, weight)

		// Note: enoughConnectedWeight is currently not set to false
		// if weight goes below startupAlpha upon node disconnection.
	}
	return nil
}

func (wt *weightTracker) EnoughConnectedWeight() bool { return wt.enoughConnectedWeight }

// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package tracker

import (
	"math"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/validators"
)

var _ Targeter = (*targeter)(nil)

type Targeter interface {
	// Returns the target usage of the given node.
	TargetUsage(nodeID ids.NodeID) float64
}

type TargeterConfig struct {
	// VdrAlloc is the amount of the resource to split over validators, weighted
	// by stake.
	VdrAlloc float64 `json:"vdrAlloc"`

	// MaxNonVdrUsage is the amount of the resource which, if utilized, will
	// result in allocations being based only on the stake weighted allocation.
	MaxNonVdrUsage float64 `json:"maxNonVdrUsage"`

	// MaxNonVdrNodeUsage is the amount of the resource to allocate to a node
	// before adding the stake weighted allocation.
	MaxNonVdrNodeUsage float64 `json:"maxNonVdrNodeUsage"`
}

func NewTargeter(
	config *TargeterConfig,
	vdrs validators.Set,
	tracker Tracker,
) Targeter {
	return &targeter{
		vdrs:               vdrs,
		tracker:            tracker,
		vdrAlloc:           config.VdrAlloc,
		maxNonVdrUsage:     config.MaxNonVdrUsage,
		maxNonVdrNodeUsage: config.MaxNonVdrNodeUsage,
	}
}

type targeter struct {
	vdrs               validators.Set
	tracker            Tracker
	vdrAlloc           float64
	maxNonVdrUsage     float64
	maxNonVdrNodeUsage float64
}

func (t *targeter) TargetUsage(nodeID ids.NodeID) float64 {
	// This node's at-large allocation is min([remaining at large], [max at large for a given peer])
	usage := t.tracker.TotalUsage()
	baseAlloc := math.Max(0, t.maxNonVdrUsage-usage)
	baseAlloc = math.Min(baseAlloc, t.maxNonVdrNodeUsage)

	// This node gets a stake-weighted portion of the validator allocation.
	weight := t.vdrs.GetWeight(nodeID)
	vdrAlloc := t.vdrAlloc * float64(weight) / float64(t.vdrs.Weight())
	return vdrAlloc + baseAlloc
}

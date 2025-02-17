// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package tracker

import (
	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/logging"
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
	logger logging.Logger,
	config *TargeterConfig,
	vdrs validators.Manager,
	tracker Tracker,
) Targeter {
	return &targeter{
		log:                logger,
		vdrs:               vdrs,
		tracker:            tracker,
		vdrAlloc:           config.VdrAlloc,
		maxNonVdrUsage:     config.MaxNonVdrUsage,
		maxNonVdrNodeUsage: config.MaxNonVdrNodeUsage,
	}
}

type targeter struct {
	vdrs               validators.Manager
	log                logging.Logger
	tracker            Tracker
	vdrAlloc           float64
	maxNonVdrUsage     float64
	maxNonVdrNodeUsage float64
}

func (t *targeter) TargetUsage(nodeID ids.NodeID) float64 {
	// This node's at-large allocation is min([remaining at large], [max at large for a given peer])
	usage := t.tracker.TotalUsage()
	baseAlloc := max(0, t.maxNonVdrUsage-usage)
	baseAlloc = min(baseAlloc, t.maxNonVdrNodeUsage)

	// This node gets a stake-weighted portion of the validator allocation.
	weight := t.vdrs.GetWeight(constants.PrimaryNetworkID, nodeID)
	if weight == 0 {
		return baseAlloc
	}

	totalWeight, err := t.vdrs.TotalWeight(constants.PrimaryNetworkID)
	if err != nil {
		t.log.Error("couldn't get total weight of primary network",
			zap.Error(err),
		)
		return baseAlloc
	}

	vdrAlloc := t.vdrAlloc * float64(weight) / float64(totalWeight)
	return vdrAlloc + baseAlloc
}

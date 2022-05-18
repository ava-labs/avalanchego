// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package tracker

import (
	"math"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils/timer/mockable"
)

var _ CPUTargeter = &cpuTargeter{}

type CPUTargeter interface {
	// Returns the target CPU usage of the given node. The returned value is >= 0.
	TargetCPUUsage(nodeID ids.NodeID) float64
}

type CPUTargeterConfig struct {
	Clock mockable.Clock `json:"-"`

	// VdrCPUAlloc is the number of CPU cores to split over validators, weighted
	// by stake.
	VdrCPUAlloc float64 `json:"vdrCPUAlloc"`

	// MaxNonVdrUsage is the number of CPU cores which, if utilized, will result
	// in allocations being based only on the stake weighted allocation.
	MaxNonVdrUsage float64 `json:"maxNonVdrUsage"`

	// MaxNonVdrNodeUsage is the number of CPU cores to allocate to a node
	// before adding the stake weighted allocation.
	MaxNonVdrNodeUsage float64 `json:"maxNonVdrNodeUsage"`
}

func NewCPUTargeter(
	config *CPUTargeterConfig,
	vdrs validators.Set,
	cpuTracker TimeTracker,
) CPUTargeter {
	return &cpuTargeter{
		clock:              config.Clock,
		vdrs:               vdrs,
		cpuTracker:         cpuTracker,
		vdrCPUAlloc:        config.VdrCPUAlloc,
		maxNonVdrUsage:     config.MaxNonVdrUsage,
		maxNonVdrNodeUsage: config.MaxNonVdrNodeUsage,
	}
}

type cpuTargeter struct {
	clock              mockable.Clock
	vdrs               validators.Set
	cpuTracker         TimeTracker
	vdrCPUAlloc        float64
	maxNonVdrUsage     float64
	maxNonVdrNodeUsage float64
}

func (ct *cpuTargeter) TargetCPUUsage(nodeID ids.NodeID) float64 {
	// This node's at-large allocation is min([remaining at large], [max at large for a given peer])
	cpuUsed := ct.cpuTracker.CumulativeUtilization()
	baseCPUAlloc := math.Max(0, ct.maxNonVdrUsage-cpuUsed)
	baseCPUAlloc = math.Min(baseCPUAlloc, ct.maxNonVdrNodeUsage)

	// This node gets a stake-weighted portion of the validator allocation.
	weight, _ := ct.vdrs.GetWeight(nodeID)
	vdrCPUAlloc := ct.vdrCPUAlloc * float64(weight) / float64(ct.vdrs.Weight())
	return vdrCPUAlloc + baseCPUAlloc
}

// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package tracker

import (
	"math"
	"runtime"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils/timer/mockable"
)

var (
	_ CPUTargeter = &cpuTargeter{}

	validatorCPUAlloc = float64(runtime.NumCPU())
)

type CPUTargeter interface {
	// Returns the target CPU usage of the given node. The returned value is >= 0.
	TargetCPUUsage(nodeID ids.NodeID) float64
}

type CPUTargeterConfig struct {
	Clock                 mockable.Clock `json:"-"`
	AtLargeCPUAlloc       float64        `json:"atLargeCPUAlloc"`
	PeerMaxAtLargePortion float64        `json:"peerMaxAtLargeAlloc"`
}

func NewCPUTargeter(
	config *CPUTargeterConfig,
	vdrs validators.Set,
	cpuTracker TimeTracker,
) CPUTargeter {
	return &cpuTargeter{
		clock:           config.Clock,
		vdrs:            vdrs,
		cpuTracker:      cpuTracker,
		atLargeCPUAlloc: config.AtLargeCPUAlloc,
		atLargeMaxCPU:   config.AtLargeCPUAlloc * config.PeerMaxAtLargePortion,
	}
}

type cpuTargeter struct {
	clock           mockable.Clock
	vdrs            validators.Set
	cpuTracker      TimeTracker
	vdrCPUAlloc     float64
	atLargeCPUAlloc float64
	atLargeMaxCPU   float64
}

func (ct *cpuTargeter) TargetCPUUsage(nodeID ids.NodeID) float64 {
	// This node's at-large allocation is min([remaining at large], [max at large for a given peer])
	atLargeCPUUsed := ct.cpuTracker.CumulativeUtilization()
	atLargeCPUAlloc := math.Max(0, ct.atLargeCPUAlloc-atLargeCPUUsed)
	atLargeCPUAlloc = math.Min(atLargeCPUAlloc, ct.atLargeMaxCPU)

	// This node gets a stake-weighted portion of the validator allocation.
	weight, _ := ct.vdrs.GetWeight(nodeID)
	vdrCPUAlloc := validatorCPUAlloc * float64(weight) / float64(ct.vdrs.Weight())
	totalAlloc := vdrCPUAlloc + atLargeCPUAlloc
	return totalAlloc
}

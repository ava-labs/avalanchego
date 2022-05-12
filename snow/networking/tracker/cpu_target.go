// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package tracker

import (
	"fmt"
	"math"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils/timer/mockable"
)

var _ CPUTargeter = &cpuTargeter{}

type CPUTargeter interface {
	// Returns the target CPU usage of the given node from:
	// 1. The validator CPU allocation.
	// 2. The at-large CPU allocation.
	// Also returns the portion of CPU usage that should be attributed
	// to the at-large allocation.
	// All returned values are >= 0.
	TargetCPUUsage(nodeID ids.NodeID) (float64, float64, float64)
}

type CPUTargeterConfig struct {
	Clock                 mockable.Clock `json:"-"`
	VdrCPUAlloc           float64        `json:"vdrCPUAlloc"`
	AtLargeCPUAlloc       float64        `json:"atLargeCPUAlloc"`
	PeerMaxAtLargePortion float64        `json:"peerMaxAtLargeAlloc"`
}

func NewCPUTargeter(
	reg prometheus.Registerer,
	config *CPUTargeterConfig,
	vdrs validators.Set,
	cpuTracker TimeTracker,
) (targeter CPUTargeter, err error) {
	t := &cpuTargeter{
		clock:           config.Clock,
		vdrs:            vdrs,
		cpuTracker:      cpuTracker,
		vdrCPUAlloc:     config.VdrCPUAlloc,
		atLargeCPUAlloc: config.AtLargeCPUAlloc,
		atLargeMaxCPU:   config.AtLargeCPUAlloc * config.PeerMaxAtLargePortion,
	}
	t.metrics, err = newCPUTargeterMetrics("cpu_targeter", reg)
	if err != nil {
		return nil, fmt.Errorf("initializing cpuTracker metrics errored with: %w", err)
	}
	return t, nil
}

type cpuTargeter struct {
	clock           mockable.Clock
	vdrs            validators.Set
	cpuTracker      TimeTracker
	metrics         *targeterMetrics
	vdrCPUAlloc     float64
	atLargeCPUAlloc float64
	atLargeMaxCPU   float64
}

func (ct *cpuTargeter) TargetCPUUsage(nodeID ids.NodeID) (float64, float64, float64) {
	// This node's at-large allocation is min([remaining at large], [max at large for a given peer])
	atLargeCPUUsed := ct.cpuTracker.CumulativeAtLargeUtilization(ct.clock.Time())
	atLargeCPUAlloc := math.Max(0, ct.atLargeCPUAlloc-atLargeCPUUsed)
	atLargeCPUAlloc = math.Min(atLargeCPUAlloc, ct.atLargeMaxCPU)

	// This node gets a stake-weighted portion of the validator allocation.
	weight, _ := ct.vdrs.GetWeight(nodeID)
	if weight == 0 {
		return 0, atLargeCPUAlloc, 1
	}
	vdrCPUAlloc := ct.vdrCPUAlloc * float64(weight) / float64(ct.vdrs.Weight())
	totalAlloc := vdrCPUAlloc + atLargeCPUAlloc
	// Note that [atLargeCPUPortion] is in [0,1]
	var atLargeCPUPortion float64
	if totalAlloc == 0 {
		// Note this can only happen if [ct.vdrCPUAlloc] == 0.
		atLargeCPUPortion = 1
	} else {
		atLargeCPUPortion = atLargeCPUAlloc / totalAlloc
	}
	return vdrCPUAlloc, atLargeCPUAlloc, atLargeCPUPortion
}

type targeterMetrics struct {
	scaledTargetMetric prometheus.Gauge
}

func newCPUTargeterMetrics(namespace string, reg prometheus.Registerer) (*targeterMetrics, error) {
	m := &targeterMetrics{
		scaledTargetMetric: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "scaled_target",
			Help:      "the cpu target scaled by current usage. range:[0, cpu-target * cpu-target-max-scaling] ",
		}),
	}
	return m, reg.Register(m.scaledTargetMetric)
}

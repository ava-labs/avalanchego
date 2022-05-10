// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package tracker

import (
	"fmt"
	"math"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/validators"
)

var _ CPUTargeter = &cpuTargeter{}

type CPUTargeter interface {
	// Return the target CPU usage of the given node.
	TargetCPUUsage(nodeID ids.NodeID) float64
}

type CPUTargeterConfig struct {
	CPUTarget                    float64 `json:"cpuTarget"`
	VdrCPUPercentage             float64 `json:"vdrCPUPercentage"`
	SinglePeerMaxUsagePercentage float64 `json:"singlePeerMaxUsagePercentage"`
	MaxScaling                   float64 `json:"maxScaling"`
}

func NewCPUTargeter(reg prometheus.Registerer, config *CPUTargeterConfig, vdrs validators.Set, cpuTracker TimeTracker) (targeter CPUTargeter, err error) {
	t := &cpuTargeter{
		vdrs:                vdrs,
		config:              config,
		cpuTracker:          cpuTracker,
		nonVdrCPUPercentage: 1 - config.VdrCPUPercentage,
		minActivePeers:      1 / config.SinglePeerMaxUsagePercentage,
	}
	t.metrics, err = newCPUTargeterMetrics("cpu_targeter", reg)
	if err != nil {
		return nil, fmt.Errorf("initializing cpuTracker metrics errored with: %w", err)
	}
	return t, nil
}

type cpuTargeter struct {
	config              *CPUTargeterConfig
	vdrs                validators.Set
	cpuTracker          TimeTracker
	metrics             *targeterMetrics
	nonVdrCPUPercentage float64
	minActivePeers      float64
}

func (ct *cpuTargeter) TargetCPUUsage(nodeID ids.NodeID) float64 {
	// scale the cpu target based on how far off the current usage is from the
	// target
	scalingFactor := ct.config.CPUTarget / ct.cpuTracker.CumulativeUtilization(time.Now())
	// the scaling factor needs to be capped by [MaxScaling]
	scalingFactor = math.Min(scalingFactor, ct.config.MaxScaling)
	scaledCPUTarget := ct.config.CPUTarget * scalingFactor

	ct.metrics.scaledTargetMetric.Set(scaledCPUTarget)

	// calculate the cpu allocation from the validator portion of the scaled cpu
	// target
	weight, _ := ct.vdrs.GetWeight(nodeID)
	vdrCPUPortion := scaledCPUTarget * ct.config.VdrCPUPercentage
	activeWeight := math.Max(float64(ct.cpuTracker.ActiveWeight()), 1)
	perWeightCPUUsage := vdrCPUPortion / activeWeight
	vdrCPUUsage := perWeightCPUUsage * float64(weight)

	// calculate the per peer allocation from the non-validator portion of the
	// scaled cpu target
	nonVdrCPUPortion := scaledCPUTarget * ct.nonVdrCPUPercentage

	// per peer usage is capped by behaving as if there are always at least a
	// minimum number of peers.
	activePeers := math.Max(float64(ct.cpuTracker.Len()), ct.minActivePeers)
	perPeerCPUUsage := nonVdrCPUPortion / activePeers
	return vdrCPUUsage + perPeerCPUUsage
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

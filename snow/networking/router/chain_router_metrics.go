// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package router

import (
	"github.com/prometheus/client_golang/prometheus"

	"github.com/ava-labs/avalanchego/utils"
)

// routerMetrics about router messages
type routerMetrics struct {
	outstandingRequests   prometheus.Gauge
	longestRunningRequest prometheus.Gauge
	droppedRequests       prometheus.Counter
}

func newRouterMetrics(registerer prometheus.Registerer) (*routerMetrics, error) {
	rMetrics := &routerMetrics{}
	rMetrics.outstandingRequests = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "outstanding",
			Help: "Number of outstanding requests (all types)",
		},
	)
	rMetrics.longestRunningRequest = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "longest_running",
			Help: "Time (in ns) the longest request took",
		},
	)
	rMetrics.droppedRequests = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "dropped",
			Help: "Number of dropped requests (all types)",
		},
	)

	err := utils.Err(
		registerer.Register(rMetrics.outstandingRequests),
		registerer.Register(rMetrics.longestRunningRequest),
		registerer.Register(rMetrics.droppedRequests),
	)
	return rMetrics, err
}

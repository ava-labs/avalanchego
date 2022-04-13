// Copyright (C) 2022, Chain4Travel AG. All rights reserved.
//
// This file is a derived work, based on ava-labs code whose
// original notices appear below.
//
// It is distributed under the same license conditions as the
// original code from which it is derived.
//
// Much love to the original authors for their work.
// **********************************************************

// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package router

import (
	"github.com/chain4travel/caminogo/utils/wrappers"
	"github.com/prometheus/client_golang/prometheus"
)

// routerMetrics about router messages
type routerMetrics struct {
	outstandingRequests   prometheus.Gauge
	longestRunningRequest prometheus.Gauge
	droppedRequests       prometheus.Counter
}

func newRouterMetrics(namespace string, registerer prometheus.Registerer) (*routerMetrics, error) {
	rMetrics := &routerMetrics{}
	rMetrics.outstandingRequests = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "outstanding",
			Help:      "Number of outstanding requests (all types)",
		},
	)
	rMetrics.longestRunningRequest = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "longest_running",
			Help:      "Time (in ns) the longest request took",
		},
	)
	rMetrics.droppedRequests = prometheus.NewCounter(
		prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "dropped",
			Help:      "Number of dropped requests (all types)",
		},
	)

	errs := wrappers.Errs{}
	errs.Add(
		registerer.Register(rMetrics.outstandingRequests),
		registerer.Register(rMetrics.longestRunningRequest),
		registerer.Register(rMetrics.droppedRequests),
	)
	return rMetrics, errs.Err
}

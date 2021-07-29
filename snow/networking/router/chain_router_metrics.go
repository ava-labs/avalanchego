// (c) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package router

import (
	"github.com/ava-labs/avalanchego/utils/wrappers"
	"github.com/prometheus/client_golang/prometheus"
)

// routerMetrics about router messages
type routerMetrics struct {
	outstandingRequests   prometheus.Gauge
	longestRunningRequest prometheus.Gauge
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

	errs := wrappers.Errs{}
	errs.Add(
		registerer.Register(rMetrics.outstandingRequests),
		registerer.Register(rMetrics.longestRunningRequest),
	)
	return rMetrics, errs.Err
}

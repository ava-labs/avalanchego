// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package snowman

import (
	"github.com/prometheus/client_golang/prometheus"

	"github.com/ava-labs/gecko/utils/logging"
)

type metrics struct {
	numPendingRequests, numBlocked prometheus.Gauge
	numBootstrapped, numDropped    prometheus.Counter

	numBlkRequests, numBlockedBlk prometheus.Gauge
}

// Initialize implements the Engine interface
func (m *metrics) Initialize(log logging.Logger, namespace string, registerer prometheus.Registerer) {
	m.numPendingRequests = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "sm_bs_requests",
			Help:      "Number of pending bootstrap requests",
		})
	m.numBlocked = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "sm_bs_blocked",
			Help:      "Number of blocked bootstrap blocks",
		})
	m.numBootstrapped = prometheus.NewCounter(
		prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "sm_bs_accepted",
			Help:      "Number of accepted bootstrap blocks",
		})
	m.numDropped = prometheus.NewCounter(
		prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "sm_bs_dropped",
			Help:      "Number of dropped bootstrap blocks",
		})
	m.numBlkRequests = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "sm_blk_requests",
			Help:      "Number of pending vertex requests",
		})
	m.numBlockedBlk = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "sm_blocked_blks",
			Help:      "Number of blocked vertices",
		})

	if err := registerer.Register(m.numPendingRequests); err != nil {
		log.Error("Failed to register sm_bs_requests statistics due to %s", err)
	}
	if err := registerer.Register(m.numBlocked); err != nil {
		log.Error("Failed to register sm_bs_blocked statistics due to %s", err)
	}
	if err := registerer.Register(m.numBootstrapped); err != nil {
		log.Error("Failed to register sm_bs_accepted statistics due to %s", err)
	}
	if err := registerer.Register(m.numDropped); err != nil {
		log.Error("Failed to register sm_bs_dropped statistics due to %s", err)
	}
	if err := registerer.Register(m.numBlkRequests); err != nil {
		log.Error("Failed to register sm_blk_requests statistics due to %s", err)
	}
	if err := registerer.Register(m.numBlockedBlk); err != nil {
		log.Error("Failed to register sm_blocked_blks statistics due to %s", err)
	}
}

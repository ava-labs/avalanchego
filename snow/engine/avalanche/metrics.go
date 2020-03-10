// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package avalanche

import (
	"github.com/prometheus/client_golang/prometheus"

	"github.com/ava-labs/gecko/utils/logging"
)

type metrics struct {
	numPendingRequests, numBlockedVtx, numBlockedTx prometheus.Gauge
	numBootstrappedVtx, numDroppedVtx,
	numBootstrappedTx, numDroppedTx prometheus.Counter

	numPolls, numVtxRequests, numTxRequests, numPendingVtx prometheus.Gauge
}

// Initialize implements the Engine interface
func (m *metrics) Initialize(log logging.Logger, namespace string, registerer prometheus.Registerer) {
	m.numPendingRequests = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "av_bs_vtx_requests",
			Help:      "Number of pending bootstrap vertex requests",
		})
	m.numBlockedVtx = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "av_bs_blocked_vts",
			Help:      "Number of blocked bootstrap vertices",
		})
	m.numBlockedTx = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "av_bs_blocked_txs",
			Help:      "Number of blocked bootstrap txs",
		})
	m.numBootstrappedVtx = prometheus.NewCounter(
		prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "av_bs_accepted_vts",
			Help:      "Number of accepted vertices",
		})
	m.numDroppedVtx = prometheus.NewCounter(
		prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "av_bs_dropped_vts",
			Help:      "Number of dropped vertices",
		})
	m.numBootstrappedTx = prometheus.NewCounter(
		prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "av_bs_accepted_txs",
			Help:      "Number of accepted txs",
		})
	m.numDroppedTx = prometheus.NewCounter(
		prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "av_bs_dropped_txs",
			Help:      "Number of dropped txs",
		})
	m.numPolls = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "av_polls",
			Help:      "Number of pending network polls",
		})
	m.numVtxRequests = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "av_vtx_requests",
			Help:      "Number of pending vertex requests",
		})
	m.numTxRequests = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "av_tx_requests",
			Help:      "Number of pending transactions",
		})
	m.numPendingVtx = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "av_blocked_vts",
			Help:      "Number of blocked vertices",
		})

	if err := registerer.Register(m.numPendingRequests); err != nil {
		log.Error("Failed to register av_bs_vtx_requests statistics due to %s", err)
	}
	if err := registerer.Register(m.numBlockedVtx); err != nil {
		log.Error("Failed to register av_bs_blocked_vts statistics due to %s", err)
	}
	if err := registerer.Register(m.numBlockedTx); err != nil {
		log.Error("Failed to register av_bs_blocked_txs statistics due to %s", err)
	}
	if err := registerer.Register(m.numBootstrappedVtx); err != nil {
		log.Error("Failed to register av_bs_accepted_vts statistics due to %s", err)
	}
	if err := registerer.Register(m.numDroppedVtx); err != nil {
		log.Error("Failed to register av_bs_dropped_vts statistics due to %s", err)
	}
	if err := registerer.Register(m.numBootstrappedTx); err != nil {
		log.Error("Failed to register av_bs_accepted_txs statistics due to %s", err)
	}
	if err := registerer.Register(m.numDroppedTx); err != nil {
		log.Error("Failed to register av_bs_dropped_txs statistics due to %s", err)
	}
	if err := registerer.Register(m.numPolls); err != nil {
		log.Error("Failed to register av_polls statistics due to %s", err)
	}
	if err := registerer.Register(m.numVtxRequests); err != nil {
		log.Error("Failed to register av_vtx_requests statistics due to %s", err)
	}
	if err := registerer.Register(m.numTxRequests); err != nil {
		log.Error("Failed to register av_tx_requests statistics due to %s", err)
	}
	if err := registerer.Register(m.numPendingVtx); err != nil {
		log.Error("Failed to register av_blocked_vts statistics due to %s", err)
	}
}

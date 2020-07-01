// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package avalanche

import (
	"github.com/prometheus/client_golang/prometheus"

	"github.com/ava-labs/gecko/utils/logging"
)

type metrics struct {
	numBSPendingRequests, numBSBlockedVtx, numBSBlockedTx prometheus.Gauge
	numBSVtx, numBSDroppedVtx,
	numBSTx, numBSDroppedTx prometheus.Counter

	numVtxRequests, numTxRequests, numPendingVtx prometheus.Gauge
}

// Initialize implements the Engine interface
func (m *metrics) Initialize(log logging.Logger, namespace string, registerer prometheus.Registerer) {
	m.numBSPendingRequests = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "av_bs_vtx_requests",
			Help:      "Number of pending bootstrap vertex requests",
		})
	m.numBSBlockedVtx = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "av_bs_blocked_vts",
			Help:      "Number of blocked bootstrap vertices",
		})
	m.numBSBlockedTx = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "av_bs_blocked_txs",
			Help:      "Number of blocked bootstrap txs",
		})
	m.numBSVtx = prometheus.NewCounter(
		prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "av_bs_accepted_vts",
			Help:      "Number of accepted vertices",
		})
	m.numBSDroppedVtx = prometheus.NewCounter(
		prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "av_bs_dropped_vts",
			Help:      "Number of dropped vertices",
		})
	m.numBSTx = prometheus.NewCounter(
		prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "av_bs_accepted_txs",
			Help:      "Number of accepted txs",
		})
	m.numBSDroppedTx = prometheus.NewCounter(
		prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "av_bs_dropped_txs",
			Help:      "Number of dropped txs",
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

	if err := registerer.Register(m.numBSPendingRequests); err != nil {
		log.Error("Failed to register av_bs_vtx_requests statistics due to %s", err)
	}
	if err := registerer.Register(m.numBSBlockedVtx); err != nil {
		log.Error("Failed to register av_bs_blocked_vts statistics due to %s", err)
	}
	if err := registerer.Register(m.numBSBlockedTx); err != nil {
		log.Error("Failed to register av_bs_blocked_txs statistics due to %s", err)
	}
	if err := registerer.Register(m.numBSVtx); err != nil {
		log.Error("Failed to register av_bs_accepted_vts statistics due to %s", err)
	}
	if err := registerer.Register(m.numBSDroppedVtx); err != nil {
		log.Error("Failed to register av_bs_dropped_vts statistics due to %s", err)
	}
	if err := registerer.Register(m.numBSTx); err != nil {
		log.Error("Failed to register av_bs_accepted_txs statistics due to %s", err)
	}
	if err := registerer.Register(m.numBSDroppedTx); err != nil {
		log.Error("Failed to register av_bs_dropped_txs statistics due to %s", err)
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

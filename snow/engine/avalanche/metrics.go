// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package avalanche

import (
	"github.com/prometheus/client_golang/prometheus"

	"github.com/ava-labs/avalanchego/utils/wrappers"
)

type metrics struct {
	numVtxRequests, numPendingVts, numMissingTxs prometheus.Gauge
	getAncestorsVtxs                             prometheus.Histogram
}

// Initialize implements the Engine interface
func (m *metrics) Initialize(namespace string, registerer prometheus.Registerer) error {
	m.numVtxRequests = prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: namespace,
		Name:      "vtx_requests",
		Help:      "Number of outstanding vertex requests",
	})
	m.numPendingVts = prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: namespace,
		Name:      "pending_vts",
		Help:      "Number of pending vertices",
	})
	m.numMissingTxs = prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: namespace,
		Name:      "missing_txs",
		Help:      "Number of missing transactions",
	})
	m.getAncestorsVtxs = prometheus.NewHistogram(prometheus.HistogramOpts{
		Namespace: namespace,
		Name:      "get_ancestors_vtxs",
		Help:      "The number of vertices fetched in a call to GetAncestors",
		Buckets: []float64{
			0,
			1,
			5,
			10,
			100,
			500,
			1000,
			1500,
			2000,
		},
	})

	errs := wrappers.Errs{}
	errs.Add(
		registerer.Register(m.numVtxRequests),
		registerer.Register(m.numPendingVts),
		registerer.Register(m.numMissingTxs),
		registerer.Register(m.getAncestorsVtxs),
	)
	return errs.Err
}

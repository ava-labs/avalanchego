// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package avalanche

import (
	"github.com/prometheus/client_golang/prometheus"

	"github.com/ava-labs/avalanchego/utils/metric"
	"github.com/ava-labs/avalanchego/utils/wrappers"
)

type metrics struct {
	numVtxRequests, numPendingVts,
	numMissingTxs, pendingTxs,
	blockerVtxs, blockerTxs prometheus.Gauge
	getAncestorsVtxs metric.Averager
}

// Initialize implements the Engine interface
func (m *metrics) Initialize(namespace string, reg prometheus.Registerer) error {
	errs := wrappers.Errs{}
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
	m.pendingTxs = prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: namespace,
		Name:      "pending_txs",
		Help:      "Number of transactions from the VM waiting to be issued",
	})
	m.blockerVtxs = prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: namespace,
		Name:      "blocker_vtxs",
		Help:      "Number of vertices that are blocking other vertices from being issued because they haven't been issued",
	})
	m.blockerTxs = prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: namespace,
		Name:      "blocker_txs",
		Help:      "Number of transactions that are blocking other transactions from being issued because they haven't been issued",
	})

	m.getAncestorsVtxs = metric.NewAveragerWithErrs(
		namespace,
		"get_ancestors_vtxs",
		"vertices fetched in a call to GetAncestors",
		reg,
		&errs,
	)

	errs.Add(
		reg.Register(m.numVtxRequests),
		reg.Register(m.numPendingVts),
		reg.Register(m.numMissingTxs),
		reg.Register(m.pendingTxs),
		reg.Register(m.blockerVtxs),
		reg.Register(m.blockerTxs),
	)
	return errs.Err
}

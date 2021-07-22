// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package avalanche

import (
	"github.com/prometheus/client_golang/prometheus"

	"github.com/ava-labs/avalanchego/utils/metric"
	"github.com/ava-labs/avalanchego/utils/wrappers"
)

type metrics struct {
	numVtxRequests, numPendingVts, numMissingTxs prometheus.Gauge
	getAncestorsVtxs                             metric.Averager
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
	)
	return errs.Err
}

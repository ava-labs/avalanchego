// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package snowman

import (
	"github.com/prometheus/client_golang/prometheus"

	"github.com/ava-labs/avalanchego/utils/metric"
	"github.com/ava-labs/avalanchego/utils/wrappers"
)

type metrics struct {
	numRequests, numBlocked prometheus.Gauge
	getAncestorsBlks        metric.Averager
}

// Initialize the metrics
func (m *metrics) Initialize(namespace string, reg prometheus.Registerer) error {
	errs := wrappers.Errs{}
	m.numRequests = prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: namespace,
		Name:      "requests",
		Help:      "Number of outstanding block requests",
	})
	m.numBlocked = prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: namespace,
		Name:      "blocked",
		Help:      "Number of blocks that are pending issuance",
	})
	m.getAncestorsBlks = metric.NewAveragerWithErrs(
		namespace,
		"get_ancestors_blks",
		"blocks fetched in a call to GetAncestors",
		reg,
		&errs,
	)

	errs.Add(
		reg.Register(m.numRequests),
		reg.Register(m.numBlocked),
	)
	return errs.Err
}

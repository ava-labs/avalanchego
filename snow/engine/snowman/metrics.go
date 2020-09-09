// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package snowman

import (
	"github.com/prometheus/client_golang/prometheus"

	"github.com/ava-labs/avalanche-go/utils/wrappers"
)

type metrics struct {
	numRequests, numBlocked prometheus.Gauge
}

// Initialize the metrics
func (m *metrics) Initialize(namespace string, registerer prometheus.Registerer) error {
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

	errs := wrappers.Errs{}
	errs.Add(
		registerer.Register(m.numRequests),
		registerer.Register(m.numBlocked),
	)
	return errs.Err
}

// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package bootstrap

import (
	"github.com/prometheus/client_golang/prometheus"

	"github.com/ava-labs/avalanche-go/utils/wrappers"
)

type metrics struct {
	numRequests, numPendingRequests     prometheus.Gauge
	numFetched, numDropped, numAccepted prometheus.Counter
}

// Initialize implements the Engine interface
func (m *metrics) Initialize(
	namespace string,
	registerer prometheus.Registerer,
) error {
	m.numRequests = prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: namespace,
		Name:      "requests",
		Help:      "Number of outstanding bootstrap block requests",
	})
	m.numPendingRequests = prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: namespace,
		Name:      "pending_requests",
		Help:      "Number of pending bootstrap block requests",
	})

	m.numFetched = prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: namespace,
		Name:      "fetched",
		Help:      "Number of blocks fetched during bootstrapping",
	})
	m.numDropped = prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: namespace,
		Name:      "dropped",
		Help:      "Number of blocks dropped during bootstrapping",
	})
	m.numAccepted = prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: namespace,
		Name:      "accepted",
		Help:      "Number of blocks accepted during bootstrapping",
	})

	errs := wrappers.Errs{}
	errs.Add(
		registerer.Register(m.numRequests),
		registerer.Register(m.numPendingRequests),
		registerer.Register(m.numFetched),
		registerer.Register(m.numDropped),
		registerer.Register(m.numAccepted),
	)
	return errs.Err
}

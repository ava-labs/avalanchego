// (c) 2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package avm

import (
	"fmt"

	"github.com/ava-labs/avalanchego/utils/metricutils"
	"github.com/ava-labs/avalanchego/utils/wrappers"
	"github.com/prometheus/client_golang/prometheus"
)

func newCallsMetric(namespace, name string) prometheus.Counter {
	return prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: namespace,
		Name:      fmt.Sprintf("%s_calls", name),
		Help:      fmt.Sprintf("Number of times %s has been called", name),
	})
}

type metrics struct {
	numBootstrappingCalls, numBootstrappedCalls, numCreateHandlersCalls,
	numPendingCalls, numParseCalls, numGetCalls prometheus.Counter

	numTxRefreshes, numTxRefreshHits, numTxRefreshMisses prometheus.Counter

	apiRequestMetric metricutils.APIRequestMetrics
}

func (m *metrics) Initialize(
	namespace string,
	registerer prometheus.Registerer,
) error {
	m.numBootstrappingCalls = newCallsMetric(namespace, "bootstrapping")
	m.numBootstrappedCalls = newCallsMetric(namespace, "bootstrapped")
	m.numCreateHandlersCalls = newCallsMetric(namespace, "create_handlers")
	m.numPendingCalls = newCallsMetric(namespace, "pending")
	m.numParseCalls = newCallsMetric(namespace, "parse")
	m.numGetCalls = newCallsMetric(namespace, "get")

	m.numTxRefreshes = prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: namespace,
		Name:      "tx_refreshes",
		Help:      "Number of times unique txs have been refreshed",
	})
	m.numTxRefreshHits = prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: namespace,
		Name:      "tx_refresh_hits",
		Help:      "Number of times unique txs have not been unique, but were cached",
	})
	m.numTxRefreshMisses = prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: namespace,
		Name:      "tx_refresh_misses",
		Help:      "Number of times unique txs have not been unique and weren't cached",
	})

	m.apiRequestMetric = metricutils.NewAPIMetrics(namespace)
	errs := wrappers.Errs{}
	errs.Add(
		registerer.Register(m.numBootstrappingCalls),
		registerer.Register(m.numBootstrappedCalls),
		registerer.Register(m.numCreateHandlersCalls),
		registerer.Register(m.numPendingCalls),
		registerer.Register(m.numParseCalls),
		registerer.Register(m.numGetCalls),
		registerer.Register(m.numTxRefreshes),
		registerer.Register(m.numTxRefreshHits),
		registerer.Register(m.numTxRefreshMisses),

		m.apiRequestMetric.Register(registerer),
	)
	return errs.Err
}

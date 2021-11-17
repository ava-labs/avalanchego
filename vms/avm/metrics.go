// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package avm

import (
	"github.com/prometheus/client_golang/prometheus"

	"github.com/ava-labs/avalanchego/utils/metric"
	"github.com/ava-labs/avalanchego/utils/wrappers"
)

type metrics struct {
	numTxRefreshes, numTxRefreshHits, numTxRefreshMisses prometheus.Counter

	apiRequestMetric metric.APIInterceptor
}

func (m *metrics) Initialize(
	namespace string,
	registerer prometheus.Registerer,
) error {
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

	apiRequestMetric, err := metric.NewAPIInterceptor(namespace, registerer)
	m.apiRequestMetric = apiRequestMetric
	errs := wrappers.Errs{}
	errs.Add(
		err,
		registerer.Register(m.numTxRefreshes),
		registerer.Register(m.numTxRefreshHits),
		registerer.Register(m.numTxRefreshMisses),
	)
	return errs.Err
}

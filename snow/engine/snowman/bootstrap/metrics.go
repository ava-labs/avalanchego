// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package bootstrap

import (
	"github.com/prometheus/client_golang/prometheus"

	"github.com/ava-labs/avalanchego/utils/wrappers"
)

type metrics struct {
	numFetched, numDropped, numAccepted prometheus.Counter
	fetchETA                            prometheus.Gauge
}

func newMetrics(namespace string, registerer prometheus.Registerer) (*metrics, error) {
	m := &metrics{
		numFetched: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "fetched",
			Help:      "Number of blocks fetched during bootstrapping",
		}),
		numDropped: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "dropped",
			Help:      "Number of blocks dropped during bootstrapping",
		}),
		numAccepted: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "accepted",
			Help:      "Number of blocks accepted during bootstrapping",
		}),
		fetchETA: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "eta_fetching_complete",
			Help:      "ETA in nanoseconds until fetching phase of bootstrapping finishes",
		}),
	}

	errs := wrappers.Errs{}
	errs.Add(
		registerer.Register(m.numFetched),
		registerer.Register(m.numDropped),
		registerer.Register(m.numAccepted),
		registerer.Register(m.fetchETA),
	)
	return m, errs.Err
}

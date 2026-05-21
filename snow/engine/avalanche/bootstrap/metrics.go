// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package bootstrap

import (
	"errors"

	"github.com/prometheus/client_golang/prometheus"
)

type metrics struct {
	numFetchedVts, numAcceptedVts,
	numFetchedTxs, numAcceptedTxs prometheus.Counter
}

func (m *metrics) Initialize(registerer prometheus.Registerer) error {
	m.numFetchedVts = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "bs_fetched_vts",
		Help: "Number of vertices fetched during bootstrapping",
	})
	m.numAcceptedVts = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "bs_accepted_vts",
		Help: "Number of vertices accepted during bootstrapping",
	})

	m.numFetchedTxs = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "bs_fetched_txs",
		Help: "Number of transactions fetched during bootstrapping",
	})
	m.numAcceptedTxs = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "bs_accepted_txs",
		Help: "Number of transactions accepted during bootstrapping",
	})

	return errors.Join(
		registerer.Register(m.numFetchedVts),
		registerer.Register(m.numAcceptedVts),
		registerer.Register(m.numFetchedTxs),
		registerer.Register(m.numAcceptedTxs),
	)
}

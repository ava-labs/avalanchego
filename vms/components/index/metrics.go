// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package index

import (
	"github.com/prometheus/client_golang/prometheus"
)

type metrics struct {
	numTxsIndexed prometheus.Counter
}

func (m *metrics) initialize(namespace string, registerer prometheus.Registerer) error {
	m.numTxsIndexed = prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: namespace,
		Name:      "txs_indexed",
		Help:      "Number of transactions indexed",
	})
	return registerer.Register(m.numTxsIndexed)
}

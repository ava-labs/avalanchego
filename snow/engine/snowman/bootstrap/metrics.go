// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package bootstrap

import (
	"github.com/prometheus/client_golang/prometheus"

	"github.com/ava-labs/avalanchego/utils"
)

type metrics struct {
	numFetched, numAccepted prometheus.Counter
}

func newMetrics(namespace string, registerer prometheus.Registerer) (*metrics, error) {
	m := &metrics{
		numFetched: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "fetched",
			Help:      "Number of blocks fetched during bootstrapping",
		}),
		numAccepted: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "accepted",
			Help:      "Number of blocks accepted during bootstrapping",
		}),
	}

	err := utils.Err(
		registerer.Register(m.numFetched),
		registerer.Register(m.numAccepted),
	)
	return m, err
}

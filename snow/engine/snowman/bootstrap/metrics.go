// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package bootstrap

import (
	"errors"

	"github.com/prometheus/client_golang/prometheus"
)

type metrics struct {
	numFetched, numAccepted prometheus.Counter
}

func newMetrics(registerer prometheus.Registerer) (*metrics, error) {
	m := &metrics{
		numFetched: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "bs_fetched",
			Help: "Number of blocks fetched during bootstrapping",
		}),
		numAccepted: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "bs_accepted",
			Help: "Number of blocks accepted during bootstrapping",
		}),
	}

	err := errors.Join(
		registerer.Register(m.numFetched),
		registerer.Register(m.numAccepted),
	)
	return m, err
}

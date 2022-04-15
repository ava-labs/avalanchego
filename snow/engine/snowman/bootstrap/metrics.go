// Copyright (C) 2022, Chain4Travel AG. All rights reserved.
//
// This file is a derived work, based on ava-labs code whose
// original notices appear below.
//
// It is distributed under the same license conditions as the
// original code from which it is derived.
//
// Much love to the original authors for their work.
// **********************************************************

// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package bootstrap

import (
	"github.com/prometheus/client_golang/prometheus"

	"github.com/chain4travel/caminogo/utils/wrappers"
)

type metrics struct {
	numFetched, numDropped, numAccepted prometheus.Counter
}

func (m *metrics) Initialize(
	namespace string,
	registerer prometheus.Registerer,
) error {
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
		registerer.Register(m.numFetched),
		registerer.Register(m.numDropped),
		registerer.Register(m.numAccepted),
	)
	return errs.Err
}

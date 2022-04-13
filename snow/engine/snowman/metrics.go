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

package snowman

import (
	"github.com/prometheus/client_golang/prometheus"

	"github.com/chain4travel/caminogo/utils/metric"
	"github.com/chain4travel/caminogo/utils/wrappers"
)

type metrics struct {
	bootstrapFinished, numRequests, numBlocked, numBlockers, numNonVerifieds prometheus.Gauge
	numBuilt, numBuildsFailed                                                prometheus.Counter
	getAncestorsBlks                                                         metric.Averager
}

// Initialize the metrics
func (m *metrics) Initialize(namespace string, reg prometheus.Registerer) error {
	errs := wrappers.Errs{}
	m.bootstrapFinished = prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: namespace,
		Name:      "bootstrap_finished",
		Help:      "Whether or not bootstrap process has completed. 1 is success, 0 is fail or ongoing.",
	})
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
	m.numBlockers = prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: namespace,
		Name:      "blockers",
		Help:      "Number of blocks that are blocking other blocks from being issued because they haven't been issued",
	})
	m.numBuilt = prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: namespace,
		Name:      "blks_built",
		Help:      "Number of blocks that have been built locally",
	})
	m.numBuildsFailed = prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: namespace,
		Name:      "blk_builds_failed",
		Help:      "Number of BuildBlock calls that have failed",
	})
	m.getAncestorsBlks = metric.NewAveragerWithErrs(
		namespace,
		"get_ancestors_blks",
		"blocks fetched in a call to GetAncestors",
		reg,
		&errs,
	)
	m.numNonVerifieds = prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: namespace,
		Name:      "non_verified_blks",
		Help:      "Number of non-verified blocks in the memory",
	})

	errs.Add(
		reg.Register(m.bootstrapFinished),
		reg.Register(m.numRequests),
		reg.Register(m.numBlocked),
		reg.Register(m.numBlockers),
		reg.Register(m.numBuilt),
		reg.Register(m.numBuildsFailed),
		reg.Register(m.numNonVerifieds),
	)
	return errs.Err
}

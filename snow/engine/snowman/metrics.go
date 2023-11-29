// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package snowman

import (
	"github.com/prometheus/client_golang/prometheus"

	"github.com/ava-labs/avalanchego/utils/metric"
	"github.com/ava-labs/avalanchego/utils/wrappers"
)

const (
	pullGossipSource = "pull_gossip"
	pushGossipSource = "push_gossip"
	putGossipSource  = "put_gossip"
	builtSource      = "built"
	unknownSource    = "unknown"
)

type metrics struct {
	bootstrapFinished                     prometheus.Gauge
	numRequests                           prometheus.Gauge
	numBlocked                            prometheus.Gauge
	numBlockers                           prometheus.Gauge
	numNonVerifieds                       prometheus.Gauge
	numBuilt                              prometheus.Counter
	numBuildsFailed                       prometheus.Counter
	numUselessPutBytes                    prometheus.Counter
	numUselessPushQueryBytes              prometheus.Counter
	numMissingAcceptedBlocks              prometheus.Counter
	numProcessingAncestorFetchesFailed    prometheus.Counter
	numProcessingAncestorFetchesDropped   prometheus.Counter
	numProcessingAncestorFetchesSucceeded prometheus.Counter
	numProcessingAncestorFetchesUnneeded  prometheus.Counter
	getAncestorsBlks                      metric.Averager
	selectedVoteIndex                     metric.Averager
	issuerStake                           metric.Averager
	issued                                *prometheus.CounterVec
}

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
	m.numNonVerifieds = prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: namespace,
		Name:      "non_verified_blks",
		Help:      "Number of non-verified blocks in the memory",
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
	m.numUselessPutBytes = prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: namespace,
		Name:      "num_useless_put_bytes",
		Help:      "Amount of useless bytes received in Put messages",
	})
	m.numUselessPushQueryBytes = prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: namespace,
		Name:      "num_useless_push_query_bytes",
		Help:      "Amount of useless bytes received in PushQuery messages",
	})
	m.numMissingAcceptedBlocks = prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: namespace,
		Name:      "num_missing_accepted_blocks",
		Help:      "Number of times an accepted block height was referenced and it wasn't locally available",
	})
	m.numProcessingAncestorFetchesFailed = prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: namespace,
		Name:      "num_processing_ancestor_fetches_failed",
		Help:      "Number of votes that were dropped due to unknown blocks",
	})
	m.numProcessingAncestorFetchesDropped = prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: namespace,
		Name:      "num_processing_ancestor_fetches_dropped",
		Help:      "Number of votes that were dropped due to decided blocks",
	})
	m.numProcessingAncestorFetchesSucceeded = prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: namespace,
		Name:      "num_processing_ancestor_fetches_succeeded",
		Help:      "Number of votes that were applied to ancestor blocks",
	})
	m.numProcessingAncestorFetchesUnneeded = prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: namespace,
		Name:      "num_processing_ancestor_fetches_unneeded",
		Help:      "Number of votes that were directly applied to blocks",
	})
	m.getAncestorsBlks = metric.NewAveragerWithErrs(
		namespace,
		"get_ancestors_blks",
		"blocks fetched in a call to GetAncestors",
		reg,
		&errs,
	)
	m.selectedVoteIndex = metric.NewAveragerWithErrs(
		namespace,
		"selected_vote_index",
		"index of the voteID that was passed into consensus",
		reg,
		&errs,
	)
	m.issuerStake = metric.NewAveragerWithErrs(
		namespace,
		"issuer_stake",
		"stake weight of the peer who provided a block that was issued into consensus",
		reg,
		&errs,
	)
	m.issued = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: namespace,
		Name:      "blks_issued",
		Help:      "number of blocks that have been issued into consensus by discovery mechanism",
	}, []string{"source"})

	// Register the labels
	m.issued.WithLabelValues(pullGossipSource)
	m.issued.WithLabelValues(pushGossipSource)
	m.issued.WithLabelValues(putGossipSource)
	m.issued.WithLabelValues(builtSource)
	m.issued.WithLabelValues(unknownSource)

	errs.Add(
		reg.Register(m.bootstrapFinished),
		reg.Register(m.numRequests),
		reg.Register(m.numBlocked),
		reg.Register(m.numBlockers),
		reg.Register(m.numNonVerifieds),
		reg.Register(m.numBuilt),
		reg.Register(m.numBuildsFailed),
		reg.Register(m.numUselessPutBytes),
		reg.Register(m.numUselessPushQueryBytes),
		reg.Register(m.numMissingAcceptedBlocks),
		reg.Register(m.numProcessingAncestorFetchesFailed),
		reg.Register(m.numProcessingAncestorFetchesDropped),
		reg.Register(m.numProcessingAncestorFetchesSucceeded),
		reg.Register(m.numProcessingAncestorFetchesUnneeded),
		reg.Register(m.issued),
	)
	return errs.Err
}

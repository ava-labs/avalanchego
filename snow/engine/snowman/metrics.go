// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
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
	selectedVoteIndex                     metric.Averager
	issuerStake                           metric.Averager
	issued                                *prometheus.CounterVec
	blockTimeSkew                         prometheus.Gauge
}

func newMetrics(reg prometheus.Registerer) (*metrics, error) {
	errs := wrappers.Errs{}
	m := &metrics{
		bootstrapFinished: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "bootstrap_finished",
			Help: "Whether or not bootstrap process has completed. 1 is success, 0 is fail or ongoing.",
		}),
		numRequests: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "requests",
			Help: "Number of outstanding block requests",
		}),
		numBlocked: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "blocked",
			Help: "Number of blocks that are pending issuance",
		}),
		numBlockers: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "blockers",
			Help: "Number of blocks that are blocking other blocks from being issued because they haven't been issued",
		}),
		numNonVerifieds: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "non_verified_blks",
			Help: "Number of non-verified blocks in the memory",
		}),
		numBuilt: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "blks_built",
			Help: "Number of blocks that have been built locally",
		}),
		numBuildsFailed: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "blk_builds_failed",
			Help: "Number of BuildBlock calls that have failed",
		}),
		numUselessPutBytes: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "num_useless_put_bytes",
			Help: "Amount of useless bytes received in Put messages",
		}),
		numUselessPushQueryBytes: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "num_useless_push_query_bytes",
			Help: "Amount of useless bytes received in PushQuery messages",
		}),
		numMissingAcceptedBlocks: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "num_missing_accepted_blocks",
			Help: "Number of times an accepted block height was referenced and it wasn't locally available",
		}),
		numProcessingAncestorFetchesFailed: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "num_processing_ancestor_fetches_failed",
			Help: "Number of votes that were dropped due to unknown blocks",
		}),
		numProcessingAncestorFetchesDropped: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "num_processing_ancestor_fetches_dropped",
			Help: "Number of votes that were dropped due to decided blocks",
		}),
		numProcessingAncestorFetchesSucceeded: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "num_processing_ancestor_fetches_succeeded",
			Help: "Number of votes that were applied to ancestor blocks",
		}),
		numProcessingAncestorFetchesUnneeded: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "num_processing_ancestor_fetches_unneeded",
			Help: "Number of votes that were directly applied to blocks",
		}),
		selectedVoteIndex: metric.NewAveragerWithErrs(
			"selected_vote_index",
			"index of the voteID that was passed into consensus",
			reg,
			&errs,
		),
		issuerStake: metric.NewAveragerWithErrs(
			"issuer_stake",
			"stake weight of the peer who provided a block that was issued into consensus",
			reg,
			&errs,
		),
		issued: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "blks_issued",
			Help: "number of blocks that have been issued into consensus by discovery mechanism",
		}, []string{"source"}),
		blockTimeSkew: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "blks_built_time_skew",
			Help: "The differences between the time the block was built at and the block's timestamp",
		}),
	}

	// Register the labels
	m.issued.WithLabelValues(pullGossipSource)
	m.issued.WithLabelValues(pushGossipSource)
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
		reg.Register(m.blockTimeSkew),
	)
	return m, errs.Err
}

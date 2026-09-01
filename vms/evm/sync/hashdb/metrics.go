// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package hashdb

import (
	"errors"

	"github.com/prometheus/client_golang/prometheus"
)

var (
	// timeBuckets span 100µs (a cached read) to ~26s (a badly stalled request).
	timeBuckets = prometheus.ExponentialBuckets(100e-6, 4, 10)
	// leafCountBuckets span 1 to [maxLimit] leaves per response.
	leafCountBuckets = prometheus.ExponentialBuckets(1, 2, 11)
)

// handlerMetrics counts the leaf requests this node serves for one trie.
type handlerMetrics struct {
	count                  prometheus.Counter
	invalid                prometheus.Counter
	missingRoot            prometheus.Counter
	trieError              prometheus.Counter
	proofError             prometheus.Counter
	snapshotReadError      prometheus.Counter
	snapshotReadAttempt    prometheus.Counter
	snapshotReadSuccess    prometheus.Counter
	snapshotSegmentValid   prometheus.Counter
	snapshotSegmentInvalid prometheus.Counter

	processingTime         prometheus.Histogram
	readTime               prometheus.Histogram
	snapshotReadTime       prometheus.Histogram
	generateRangeProofTime prometheus.Histogram
	totalLeafs             prometheus.Histogram
	proofValsReturned      prometheus.Histogram
}

func newHandlerMetrics(reg prometheus.Registerer) (*handlerMetrics, error) {
	m := &handlerMetrics{
		count: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "leafs_request_count",
			Help: "Leafs requests served.",
		}),
		invalid: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "leafs_request_invalid",
			Help: "Malformed or invalid leafs requests.",
		}),
		missingRoot: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "leafs_request_missing_root",
			Help: "Leafs requests for a root not present locally.",
		}),
		trieError: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "leafs_request_trie_error",
			Help: "Errors while iterating the trie.",
		}),
		proofError: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "leafs_request_proof_error",
			Help: "Errors while generating the range proof.",
		}),
		snapshotReadError: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "leafs_request_snapshot_read_error",
			Help: "Errors while reading from the snapshot.",
		}),
		snapshotReadAttempt: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "leafs_request_snapshot_read_attempt",
			Help: "Attempts to serve leaves from the snapshot fast path.",
		}),
		snapshotReadSuccess: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "leafs_request_snapshot_read_success",
			Help: "Snapshot fast-path reads that proved against the trie in one shot.",
		}),
		snapshotSegmentValid: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "leafs_request_snapshot_segment_valid",
			Help: "Snapshot segments that validated against the trie.",
		}),
		snapshotSegmentInvalid: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "leafs_request_snapshot_segment_invalid",
			Help: "Snapshot segments that failed validation and fell back to the trie.",
		}),
		processingTime: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name:    "leafs_request_processing_time",
			Help:    "Seconds to serve a leafs request end to end.",
			Buckets: timeBuckets,
		}),
		readTime: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name:    "leafs_request_read_time",
			Help:    "Seconds spent reading leaves, from the snapshot or the trie, per request.",
			Buckets: timeBuckets,
		}),
		snapshotReadTime: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name:    "leafs_request_snapshot_read_time",
			Help:    "Seconds spent reading leaves from the snapshot per request.",
			Buckets: timeBuckets,
		}),
		generateRangeProofTime: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name:    "leafs_request_generate_range_proof_time",
			Help:    "Seconds spent generating a range proof.",
			Buckets: timeBuckets,
		}),
		totalLeafs: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name:    "leafs_request_total_leafs",
			Help:    "Leaves returned per request.",
			Buckets: leafCountBuckets,
		}),
		proofValsReturned: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name:    "leafs_request_proof_vals_returned",
			Help:    "Range-proof values returned per request.",
			Buckets: leafCountBuckets,
		}),
	}
	return m, errors.Join(
		reg.Register(m.count),
		reg.Register(m.invalid),
		reg.Register(m.missingRoot),
		reg.Register(m.trieError),
		reg.Register(m.proofError),
		reg.Register(m.snapshotReadError),
		reg.Register(m.snapshotReadAttempt),
		reg.Register(m.snapshotReadSuccess),
		reg.Register(m.snapshotSegmentValid),
		reg.Register(m.snapshotSegmentInvalid),
		reg.Register(m.processingTime),
		reg.Register(m.readTime),
		reg.Register(m.snapshotReadTime),
		reg.Register(m.generateRangeProofTime),
		reg.Register(m.totalLeafs),
		reg.Register(m.proofValsReturned),
	)
}

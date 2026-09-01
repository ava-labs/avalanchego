// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package code

import (
	"errors"

	"github.com/prometheus/client_golang/prometheus"
)

var (
	// readTimeBuckets span 100µs (a cached read) to ~26s (a badly stalled disk).
	readTimeBuckets = prometheus.ExponentialBuckets(100e-6, 4, 10)
	// bytesReturnedBuckets span 256B (one small contract) to ~16MB (a full
	// response of [params.MaxCodeSize] contracts).
	bytesReturnedBuckets = prometheus.ExponentialBuckets(256, 4, 9)
)

// handlerMetrics counts the code requests this node serves.
type handlerMetrics struct {
	count           prometheus.Counter
	missingCodeHash prometheus.Counter
	tooManyHashes   prometheus.Counter
	duplicateHashes prometheus.Counter
	readTime        prometheus.Histogram
	bytesReturned   prometheus.Histogram
}

func newHandlerMetrics(reg prometheus.Registerer) (*handlerMetrics, error) {
	m := &handlerMetrics{
		count: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "code_request_count",
			Help: "Code requests served.",
		}),
		missingCodeHash: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "code_request_missing_code_hash",
			Help: "Code requests for a code hash not present locally.",
		}),
		tooManyHashes: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "code_request_too_many_hashes",
			Help: "Code requests rejected for exceeding the max hashes per request.",
		}),
		duplicateHashes: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "code_request_duplicate_hashes",
			Help: "Code requests rejected for containing duplicate hashes.",
		}),
		readTime: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name:    "code_request_read_time",
			Help:    "Seconds spent reading code from disk per request.",
			Buckets: readTimeBuckets,
		}),
		bytesReturned: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name:    "code_request_bytes_returned",
			Help:    "Bytes of contract code returned per request.",
			Buckets: bytesReturnedBuckets,
		}),
	}
	return m, errors.Join(
		reg.Register(m.count),
		reg.Register(m.missingCodeHash),
		reg.Register(m.tooManyHashes),
		reg.Register(m.duplicateHashes),
		reg.Register(m.readTime),
		reg.Register(m.bytesReturned),
	)
}

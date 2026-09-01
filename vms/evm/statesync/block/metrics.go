// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package block

import (
	"errors"

	"github.com/prometheus/client_golang/prometheus"
)

var (
	// processingTimeBuckets span 100µs (a cached read) to ~26s (a request that
	// far overran [maxBlocksRetrievalTime]).
	processingTimeBuckets = prometheus.ExponentialBuckets(100e-6, 4, 10)
	// totalBlocksBuckets span 1 to [maxBlocksPerResponse] blocks per response.
	totalBlocksBuckets = prometheus.ExponentialBuckets(1, 2, 7)
)

// handlerMetrics counts the block requests this node serves.
type handlerMetrics struct {
	count            prometheus.Counter
	missingBlockHash prometheus.Counter
	totalBlocks      prometheus.Histogram
	processingTime   prometheus.Histogram
}

func newHandlerMetrics(reg prometheus.Registerer) (*handlerMetrics, error) {
	m := &handlerMetrics{
		count: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "block_request_count",
			Help: "Block requests served.",
		}),
		missingBlockHash: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "block_request_missing_block_hash",
			Help: "Block requests whose requested block was not found locally.",
		}),
		totalBlocks: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name:    "block_request_total_blocks",
			Help:    "Blocks returned per request.",
			Buckets: totalBlocksBuckets,
		}),
		processingTime: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name:    "block_request_processing_time",
			Help:    "Seconds to serve a block request.",
			Buckets: processingTimeBuckets,
		}),
	}
	return m, errors.Join(
		reg.Register(m.count),
		reg.Register(m.missingBlockHash),
		reg.Register(m.totalBlocks),
		reg.Register(m.processingTime),
	)
}

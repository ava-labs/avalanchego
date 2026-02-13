// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package snowman

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/linked"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/math"
	"github.com/ava-labs/avalanchego/utils/metric"
	"github.com/ava-labs/avalanchego/utils/wrappers"
)

type processingStart struct {
	time       time.Time
	pollNumber uint64
}

type metrics struct {
	log logging.Logger

	currentMaxVerifiedHeight uint64
	maxVerifiedHeight        prometheus.Gauge

	lastAcceptedHeight    prometheus.Gauge
	lastAcceptedTimestamp prometheus.Gauge

	// processingBlocks keeps track of the [processingStart] that each block was
	// issued into the consensus instance. This is used to calculate the amount
	// of time to accept or reject the block.
	processingBlocks *linked.Hashmap[ids.ID, processingStart]

	// numProcessing keeps track of the number of processing blocks
	numProcessing prometheus.Gauge

	blockSizeAcceptedSum prometheus.Gauge
	// pollsAccepted tracks the number of polls that a block was in processing
	// for before being accepted
	pollsAccepted metric.Averager
	// latAccepted tracks the number of nanoseconds that a block was processing
	// before being accepted
	latAccepted          metric.Averager
	consensusLatencies   prometheus.Histogram
	buildLatencyAccepted prometheus.Gauge

	blockSizeRejectedSum prometheus.Gauge
	// pollsRejected tracks the number of polls that a block was in processing
	// for before being rejected
	pollsRejected metric.Averager
	// latRejected tracks the number of nanoseconds that a block was processing
	// before being rejected
	latRejected metric.Averager

	// numFailedPolls keeps track of the number of polls that failed
	numFailedPolls prometheus.Counter

	// numSuccessfulPolls keeps track of the number of polls that succeeded
	numSuccessfulPolls prometheus.Counter

	// avgAcceptanceLatency tracks the average acceptance time
	avgAcceptanceLatency math.Averager
}

func newMetrics(
	log logging.Logger,
	reg prometheus.Registerer,
	lastAcceptedHeight uint64,
	lastAcceptedTime time.Time,
) (*metrics, error) {
	acceptanceHalfLife := 5 * time.Minute
	errs := wrappers.Errs{}
	m := &metrics{
		log:                      log,
		currentMaxVerifiedHeight: lastAcceptedHeight,
		maxVerifiedHeight: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "max_verified_height",
			Help: "highest verified height",
		}),
		lastAcceptedHeight: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "last_accepted_height",
			Help: "last height accepted",
		}),
		lastAcceptedTimestamp: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "last_accepted_timestamp",
			Help: "timestamp of the last accepted block in unix seconds",
		}),

		processingBlocks: linked.NewHashmap[ids.ID, processingStart](),

		numProcessing: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "blks_processing",
			Help: "number of currently processing blocks",
		}),

		blockSizeAcceptedSum: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "blks_accepted_container_size_sum",
			Help: "cumulative size of all accepted blocks",
		}),
		pollsAccepted: metric.NewAveragerWithErrs(
			"blks_polls_accepted",
			"number of polls from the issuance of a block to its acceptance",
			reg,
			&errs,
		),
		latAccepted: metric.NewAveragerWithErrs(
			"blks_accepted",
			"time (in ns) from the issuance of a block to its acceptance",
			reg,
			&errs,
		),
		consensusLatencies: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name:    "consensus_latencies",
			Help:    "times (in ns) from issuance of a block to acceptance, bucketed",
			Buckets: prometheus.LinearBuckets(float64(time.Second), float64(time.Second), 4),
		}),
		buildLatencyAccepted: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "blks_build_accept_latency",
			Help: "time (in ns) from the timestamp of a block to the time it was accepted",
		}),

		blockSizeRejectedSum: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "blks_rejected_container_size_sum",
			Help: "cumulative size of all rejected blocks",
		}),
		pollsRejected: metric.NewAveragerWithErrs(
			"blks_polls_rejected",
			"number of polls from the issuance of a block to its rejection",
			reg,
			&errs,
		),
		latRejected: metric.NewAveragerWithErrs(
			"blks_rejected",
			"time (in ns) from the issuance of a block to its rejection",
			reg,
			&errs,
		),

		numSuccessfulPolls: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "polls_successful",
			Help: "number of successful polls",
		}),
		numFailedPolls: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "polls_failed",
			Help: "number of failed polls",
		}),
		avgAcceptanceLatency: math.NewMaturedAverager(
			acceptanceHalfLife,
			math.NewUninitializedAverager(acceptanceHalfLife),
		),
	}

	// Initially set the metrics for the last accepted block.
	m.maxVerifiedHeight.Set(float64(lastAcceptedHeight))
	m.lastAcceptedHeight.Set(float64(lastAcceptedHeight))
	m.lastAcceptedTimestamp.Set(float64(lastAcceptedTime.Unix()))

	errs.Add(
		reg.Register(m.maxVerifiedHeight),
		reg.Register(m.lastAcceptedHeight),
		reg.Register(m.lastAcceptedTimestamp),
		reg.Register(m.numProcessing),
		reg.Register(m.blockSizeAcceptedSum),
		reg.Register(m.buildLatencyAccepted),
		reg.Register(m.consensusLatencies),
		reg.Register(m.blockSizeRejectedSum),
		reg.Register(m.numSuccessfulPolls),
		reg.Register(m.numFailedPolls),
	)
	return m, errs.Err
}

func (m *metrics) Issued(blkID ids.ID, pollNumber uint64) {
	m.processingBlocks.Put(blkID, processingStart{
		time:       time.Now(),
		pollNumber: pollNumber,
	})
	m.numProcessing.Inc()
}

func (m *metrics) Verified(height uint64) {
	m.currentMaxVerifiedHeight = max(m.currentMaxVerifiedHeight, height)
	m.maxVerifiedHeight.Set(float64(m.currentMaxVerifiedHeight))
}

func (m *metrics) Accepted(
	blkID ids.ID,
	height uint64,
	timestamp time.Time,
	pollNumber uint64,
	blockSize int,
) {
	start, ok := m.processingBlocks.Get(blkID)
	if !ok {
		m.log.Error("unable to measure latency",
			zap.Stringer("blkID", blkID),
			zap.String("status", "accepted"),
		)
		return
	}
	m.lastAcceptedHeight.Set(float64(height))
	m.lastAcceptedTimestamp.Set(float64(timestamp.Unix()))
	m.processingBlocks.Delete(blkID)
	m.numProcessing.Dec()

	m.blockSizeAcceptedSum.Add(float64(blockSize))

	m.pollsAccepted.Observe(float64(pollNumber - start.pollNumber))

	now := time.Now()
	processingDuration := now.Sub(start.time)
	m.latAccepted.Observe(float64(processingDuration))

	builtDuration := now.Sub(timestamp)
	m.buildLatencyAccepted.Add(float64(builtDuration))
	m.avgAcceptanceLatency.Observe(float64(builtDuration), now)
	m.consensusLatencies.Observe(float64(processingDuration))
}

func (m *metrics) Rejected(blkID ids.ID, pollNumber uint64, blockSize int) {
	start, ok := m.processingBlocks.Get(blkID)
	if !ok {
		m.log.Error("unable to measure latency",
			zap.Stringer("blkID", blkID),
			zap.String("status", "rejected"),
		)
		return
	}
	m.processingBlocks.Delete(blkID)
	m.numProcessing.Dec()

	m.blockSizeRejectedSum.Add(float64(blockSize))

	m.pollsRejected.Observe(float64(pollNumber - start.pollNumber))

	duration := time.Since(start.time)
	m.latRejected.Observe(float64(duration))
}

func (m *metrics) MeasureAndGetOldestDuration() time.Duration {
	_, oldestOp, exists := m.processingBlocks.Oldest()
	if !exists {
		return 0
	}
	return time.Since(oldestOp.time)
}

func (m *metrics) SuccessfulPoll() {
	m.numSuccessfulPolls.Inc()
}

func (m *metrics) FailedPoll() {
	m.numFailedPolls.Inc()
}

func (m *metrics) GetAverageAcceptanceTime() time.Duration {
	return time.Duration(m.avgAcceptanceLatency.Read())
}

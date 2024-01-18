// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package snowman

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/utils/linkedhashmap"
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
	processingBlocks linkedhashmap.LinkedHashmap[ids.ID, processingStart]

	// numProcessing keeps track of the number of processing blocks
	numProcessing prometheus.Gauge

	blockSizeAcceptedSum prometheus.Gauge
	// pollsAccepted tracks the number of polls that a block was in processing
	// for before being accepted
	pollsAccepted metric.Averager
	// latAccepted tracks the number of nanoseconds that a block was processing
	// before being accepted
	latAccepted          metric.Averager
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
}

func newMetrics(
	log logging.Logger,
	namespace string,
	reg prometheus.Registerer,
	lastAcceptedHeight uint64,
	lastAcceptedTime time.Time,
) (*metrics, error) {
	errs := wrappers.Errs{}
	m := &metrics{
		log:                      log,
		currentMaxVerifiedHeight: lastAcceptedHeight,
		maxVerifiedHeight: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "max_verified_height",
			Help:      "highest verified height",
		}),
		lastAcceptedHeight: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "last_accepted_height",
			Help:      "last height accepted",
		}),
		lastAcceptedTimestamp: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "last_accepted_timestamp",
			Help:      "timestamp of the last accepted block in unix seconds",
		}),

		processingBlocks: linkedhashmap.New[ids.ID, processingStart](),

		// e.g.,
		// "avalanche_X_blks_processing" reports how many blocks are currently processing
		numProcessing: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "blks_processing",
			Help:      "number of currently processing blocks",
		}),

		blockSizeAcceptedSum: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "blks_accepted_container_size_sum",
			Help:      "cumulative size of all accepted blocks",
		}),
		pollsAccepted: metric.NewAveragerWithErrs(
			namespace,
			"blks_polls_accepted",
			"number of polls from the issuance of a block to its acceptance",
			reg,
			&errs,
		),
		// e.g.,
		// "avalanche_C_blks_accepted_count" reports how many times "Observe" has been called which is the total number of blocks accepted
		// "avalanche_C_blks_accepted_sum" reports the cumulative sum of all block acceptance latencies in nanoseconds
		// "avalanche_C_blks_accepted_sum / avalanche_C_blks_accepted_count" is the average block acceptance latency in nanoseconds
		// "avalanche_C_blks_accepted_container_size_sum" reports the cumulative sum of all accepted blocks' sizes in bytes
		// "avalanche_C_blks_accepted_container_size_sum / avalanche_C_blks_accepted_count" is the average accepted block size in bytes
		latAccepted: metric.NewAveragerWithErrs(
			namespace,
			"blks_accepted",
			"time (in ns) from the issuance of a block to its acceptance",
			reg,
			&errs,
		),
		buildLatencyAccepted: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "blks_build_accept_latency",
			Help:      "time (in ns) from the timestamp of a block to the time it was accepted",
		}),

		blockSizeRejectedSum: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "blks_rejected_container_size_sum",
			Help:      "cumulative size of all rejected blocks",
		}),
		pollsRejected: metric.NewAveragerWithErrs(
			namespace,
			"blks_polls_rejected",
			"number of polls from the issuance of a block to its rejection",
			reg,
			&errs,
		),
		// e.g.,
		// "avalanche_P_blks_rejected_count" reports how many times "Observe" has been called which is the total number of blocks rejected
		// "avalanche_P_blks_rejected_sum" reports the cumulative sum of all block rejection latencies in nanoseconds
		// "avalanche_P_blks_rejected_sum / avalanche_P_blks_rejected_count" is the average block rejection latency in nanoseconds
		// "avalanche_P_blks_rejected_container_size_sum" reports the cumulative sum of all rejected blocks' sizes in bytes
		// "avalanche_P_blks_rejected_container_size_sum / avalanche_P_blks_rejected_count" is the average rejected block size in bytes
		latRejected: metric.NewAveragerWithErrs(
			namespace,
			"blks_rejected",
			"time (in ns) from the issuance of a block to its rejection",
			reg,
			&errs,
		),

		numSuccessfulPolls: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "polls_successful",
			Help:      "number of successful polls",
		}),
		numFailedPolls: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "polls_failed",
			Help:      "number of failed polls",
		}),
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
	m.currentMaxVerifiedHeight = math.Max(m.currentMaxVerifiedHeight, height)
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
			zap.Stringer("status", choices.Accepted),
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
}

func (m *metrics) Rejected(blkID ids.ID, pollNumber uint64, blockSize int) {
	start, ok := m.processingBlocks.Get(blkID)
	if !ok {
		m.log.Error("unable to measure latency",
			zap.Stringer("blkID", blkID),
			zap.Stringer("status", choices.Rejected),
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

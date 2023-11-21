// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
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

	// ProcessingEntries keeps track of the [processingStart] that each item was
	// issued into the consensus instance. This is used to calculate the amount
	// of time to accept or reject the block.
	processingBlocks linkedhashmap.LinkedHashmap[ids.ID, processingStart]

	// numProcessing keeps track of the number of processing blocks
	numProcessing prometheus.Gauge

	// pollsAccepted tracks the number of polls that an item was in processing
	// for before being accepted
	pollsAccepted metric.Averager
	// latAccepted tracks the number of nanoseconds that an item was processing
	// before being accepted
	latAccepted          metric.Averager
	blockSizeAcceptedSum prometheus.Gauge

	// pollsRejected tracks the number of polls that an item was in processing
	// for before being rejected
	pollsRejected metric.Averager
	// rejected tracks the number of nanoseconds that an item was processing
	// before being rejected
	latRejected          metric.Averager
	blockSizeRejectedSum prometheus.Gauge

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
		// "avalanche_7y7zwo7XatqnX4dtTakLo32o7jkMX4XuDa26WaxbCXoCT1qKK_blks_processing" to count how blocks are currently processing
		numProcessing: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "blks_processing",
			Help:      "number of currently processing blocks",
		}),

		pollsAccepted: metric.NewAveragerWithErrs(
			namespace,
			"blks_polls_accepted",
			"number of polls from the issuance of a block to its acceptance",
			reg,
			&errs,
		),
		// e.g.,
		// "avalanche_C_blks_accepted_count" to count how many "Observe" gets called -- count all "Accept"
		// "avalanche_C_blks_accepted_sum" to count how many ns have elapsed since its issuance on acceptance
		// "avalanche_C_blks_accepted_sum / avalanche_C_blks_accepted_count" is the average block acceptance latency in ns
		// "avalanche_C_blks_accepted_container_size_sum" to track cumulative sum of all accepted blocks' sizes
		// "avalanche_C_blks_accepted_container_size_sum / avalanche_C_blks_accepted_count" is the average block size
		latAccepted: metric.NewAveragerWithErrs(
			namespace,
			"blks_accepted",
			"time (in ns) from the issuance of a block to its acceptance",
			reg,
			&errs,
		),
		blockSizeAcceptedSum: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "blks_accepted_container_size_sum",
			Help:      "cumulative size of all accepted blocks",
		}),

		pollsRejected: metric.NewAveragerWithErrs(
			namespace,
			"blks_polls_rejected",
			"number of polls from the issuance of a block to its rejection",
			reg,
			&errs,
		),
		// e.g.,
		// "avalanche_P_blks_rejected_count" to count how many "Observe" gets called -- count all "Reject"
		// "avalanche_P_blks_rejected_sum" to count how many ns have elapsed since its issuance on rejection
		// "avalanche_P_blks_accepted_sum / avalanche_P_blks_accepted_count" is the average block acceptance latency in ns
		// "avalanche_P_blks_accepted_container_size_sum" to track cumulative sum of all accepted blocks' sizes
		// "avalanche_P_blks_accepted_container_size_sum / avalanche_P_blks_accepted_count" is the average block size
		latRejected: metric.NewAveragerWithErrs(
			namespace,
			"blks_rejected",
			"time (in ns) from the issuance of a block to its rejection",
			reg,
			&errs,
		),
		blockSizeRejectedSum: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "blks_rejected_container_size_sum",
			Help:      "cumulative size of all rejected blocks",
		}),

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
		m.log.Warn("unable to measure latency",
			zap.Stringer("blkID", blkID),
			zap.Stringer("status", choices.Accepted),
		)
		return
	}
	m.lastAcceptedHeight.Set(float64(height))
	m.lastAcceptedTimestamp.Set(float64(timestamp.Unix()))
	m.processingBlocks.Delete(blkID)
	m.numProcessing.Dec()

	m.pollsAccepted.Observe(float64(pollNumber - start.pollNumber))

	duration := time.Since(start.time)
	m.latAccepted.Observe(float64(duration))

	m.blockSizeAcceptedSum.Add(float64(blockSize))
}

func (m *metrics) Rejected(blkID ids.ID, pollNumber uint64, blockSize int) {
	start, ok := m.processingBlocks.Get(blkID)
	if !ok {
		m.log.Warn("unable to measure latency",
			zap.Stringer("blkID", blkID),
			zap.Stringer("status", choices.Rejected),
		)
		return
	}
	m.processingBlocks.Delete(blkID)
	m.numProcessing.Dec()

	m.pollsRejected.Observe(float64(pollNumber - start.pollNumber))

	duration := time.Since(start.time)
	m.latRejected.Observe(float64(duration))

	m.blockSizeRejectedSum.Add(float64(blockSize))
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

// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package saexec

import (
	"errors"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

// queueDurationBuckets span 1ms (executor keeping up) to ~16s (deep backlog).
var queueDurationBuckets = prometheus.ExponentialBuckets(time.Millisecond.Seconds(), 2, 15)

// executeBlockBuckets span 500µs (small block) to ~16s (large/slow block).
var executeBlockBuckets = prometheus.ExponentialBuckets(500*time.Microsecond.Seconds(), 2, 16)

type metrics struct {
	lastExecutedHeight prometheus.Gauge

	// queueDuration tracks a block's entire lifetime from acceptance into the
	// queue until its execution completes, so executeBlockDuration is a subset
	// of it: queueDuration = time spent in the queue + executeBlockDuration.
	queueDuration        prometheus.Histogram
	executeBlockDuration prometheus.Histogram

	// executionQueueBlocks and executionQueueGasLimit track outstanding work:
	// blocks accepted but not yet executed (including the one being executed),
	// and the sum of their gas limits.
	executionQueueBlocks   prometheus.Gauge
	executionQueueGasLimit prometheus.Gauge

	// executedGasCharged is the gas charged for executed blocks: transaction
	// gas used plus end-of-block operation gas. It is not the eth gas used.
	// executedGasLimit is the gas limit (worst-case gas) of those same blocks.
	executedGasCharged prometheus.Counter
	executedGasLimit   prometheus.Counter

	// acceptedGasLimit is the gas limit (worst-case gas) of blocks entering
	// the execution queue, the acceptance-side counterpart of executedGasLimit.
	acceptedGasLimit prometheus.Counter
}

func newMetrics(reg prometheus.Registerer, lastExecutedHeight uint64) (*metrics, error) {
	m := &metrics{
		lastExecutedHeight: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "last_executed_height",
			Help: "Height of the latest block that completed async execution.",
		}),
		queueDuration: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name:    "execution_queue_duration_seconds",
			Help:    "Time from a block's acceptance until its execution completes.",
			Buckets: queueDurationBuckets,
		}),
		executeBlockDuration: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name:    "execute_block_duration_seconds",
			Help:    "Wall-clock time to execute a single block, including state commit and post-execution work.",
			Buckets: executeBlockBuckets,
		}),
		executionQueueBlocks: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "execution_queue_blocks",
			Help: "Number of accepted blocks that have not yet completed execution.",
		}),
		executionQueueGasLimit: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "execution_queue_gas_limit",
			Help: "Sum of the gas limits of accepted blocks that have not yet completed execution.",
		}),
		executedGasCharged: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "executed_gas_charged_total",
			Help: "Cumulative gas charged by executed blocks (transaction gas used plus end-of-block operation gas); this is not the eth gas used.",
		}),
		executedGasLimit: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "executed_gas_limit_total",
			Help: "Cumulative gas limit (worst-case gas) of executed blocks.",
		}),
		acceptedGasLimit: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "accepted_gas_limit_total",
			Help: "Cumulative gas limit (worst-case gas) of blocks accepted into the execution queue.",
		}),
	}
	m.lastExecutedHeight.Set(float64(lastExecutedHeight))
	return m, errors.Join(
		reg.Register(m.lastExecutedHeight),
		reg.Register(m.queueDuration),
		reg.Register(m.executeBlockDuration),
		reg.Register(m.executionQueueBlocks),
		reg.Register(m.executionQueueGasLimit),
		reg.Register(m.executedGasCharged),
		reg.Register(m.executedGasLimit),
		reg.Register(m.acceptedGasLimit),
	)
}

func (m *metrics) observeQueueDuration(d time.Duration) {
	m.queueDuration.Observe(d.Seconds())
}

func (m *metrics) observeExecuteDuration(d time.Duration) {
	m.executeBlockDuration.Observe(d.Seconds())
}

// markEnqueued records that a block with the given gas limit has been accepted
// into the execution queue.
func (m *metrics) markEnqueued(gasLimit uint64) {
	m.executionQueueBlocks.Inc()
	m.executionQueueGasLimit.Add(float64(gasLimit))
	m.acceptedGasLimit.Add(float64(gasLimit))
}

// markExecuted records that the block at the given height, with the given gas
// limit, has finished executing, having been charged gasCharged gas. It
// advances the last-executed height, removes the block from the queue gauges,
// and adds to the cumulative executed totals.
func (m *metrics) markExecuted(height, gasCharged, gasLimit uint64) {
	m.lastExecutedHeight.Set(float64(height))
	m.executionQueueBlocks.Dec()
	m.executionQueueGasLimit.Sub(float64(gasLimit))
	m.executedGasCharged.Add(float64(gasCharged))
	m.executedGasLimit.Add(float64(gasLimit))
}

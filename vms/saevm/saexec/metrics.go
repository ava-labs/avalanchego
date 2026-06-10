// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package saexec

import (
	"errors"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

// queueWaitBuckets span 1ms (executor keeping up) to ~131s (deep backlog).
var queueWaitBuckets = prometheus.ExponentialBuckets(0.001, 2, 18)

// executeBlockBuckets span 500µs (small block) to ~16s (large/slow block).
var executeBlockBuckets = prometheus.ExponentialBuckets(0.0005, 2, 16)

type metrics struct {
	lastExecutedHeight   prometheus.Gauge
	queueWaitDuration    prometheus.Histogram
	executeBlockDuration prometheus.Histogram

	// executionQueueBlocks and executionQueueGasLimit track outstanding work:
	// blocks accepted but not yet executed (including the one being executed),
	// and the sum of their gas limits.
	executionQueueBlocks   prometheus.Gauge
	executionQueueGasLimit prometheus.Gauge

	// executedGas is the actual gas consumed by executed blocks;
	// executedGasLimit is the gas limit (worst-case gas) of those same blocks.
	executedGas      prometheus.Counter
	executedGasLimit prometheus.Counter
}

func newMetrics(reg prometheus.Registerer) (*metrics, error) {
	m := &metrics{
		lastExecutedHeight: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "last_executed_height",
			Help: "Height of the latest block that completed async execution.",
		}),
		queueWaitDuration: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name:    "execution_queue_wait_duration_seconds",
			Help:    "Time an accepted block waits in the execution queue before execution starts.",
			Buckets: queueWaitBuckets,
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
		executedGas: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "executed_gas_total",
			Help: "Cumulative gas actually consumed by executed blocks.",
		}),
		executedGasLimit: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "executed_gas_limit_total",
			Help: "Cumulative gas limit (worst-case gas) of executed blocks.",
		}),
	}
	return m, errors.Join(
		reg.Register(m.lastExecutedHeight),
		reg.Register(m.queueWaitDuration),
		reg.Register(m.executeBlockDuration),
		reg.Register(m.executionQueueBlocks),
		reg.Register(m.executionQueueGasLimit),
		reg.Register(m.executedGas),
		reg.Register(m.executedGasLimit),
	)
}

func (m *metrics) setLastExecutedHeight(height uint64) {
	m.lastExecutedHeight.Set(float64(height))
}

func (m *metrics) observeQueueWait(d time.Duration) {
	m.queueWaitDuration.Observe(d.Seconds())
}

func (m *metrics) observeExecuteDuration(d time.Duration) {
	m.executeBlockDuration.Observe(d.Seconds())
}

// markEnqueued records that a block with the given gas limit has been accepted
// into the execution queue.
func (m *metrics) markEnqueued(gasLimit uint64) {
	m.executionQueueBlocks.Inc()
	m.executionQueueGasLimit.Add(float64(gasLimit))
}

// markExecuted records that a block with the given gas limit has finished
// executing, having consumed gasConsumed gas. It removes the block from the
// queue gauges and adds to the cumulative executed totals.
func (m *metrics) markExecuted(gasConsumed, gasLimit uint64) {
	m.executionQueueBlocks.Dec()
	m.executionQueueGasLimit.Sub(float64(gasLimit))
	m.executedGas.Add(float64(gasConsumed))
	m.executedGasLimit.Add(float64(gasLimit))
}

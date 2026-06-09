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
	unexecutedTxs        prometheus.Gauge
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
			Help:    "Wall-clock time to execute a single block, excluding state commit.",
			Buckets: executeBlockBuckets,
		}),
		unexecutedTxs: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "unexecuted_txs",
			Help: "Number of transactions in accepted blocks that have not yet completed execution.",
		}),
	}
	return m, errors.Join(
		reg.Register(m.lastExecutedHeight),
		reg.Register(m.queueWaitDuration),
		reg.Register(m.executeBlockDuration),
		reg.Register(m.unexecutedTxs),
	)
}

func (m *metrics) markExecuted(height uint64) {
	m.lastExecutedHeight.Set(float64(height))
}

func (m *metrics) observeQueueWait(d time.Duration) {
	m.queueWaitDuration.Observe(d.Seconds())
}

func (m *metrics) observeExecuteDuration(d time.Duration) {
	m.executeBlockDuration.Observe(d.Seconds())
}

// addUnexecutedTxs records that n more transactions have been accepted but not
// yet executed.
func (m *metrics) addUnexecutedTxs(n int) {
	m.unexecutedTxs.Add(float64(n))
}

// subUnexecutedTxs records that n transactions have finished executing.
func (m *metrics) subUnexecutedTxs(n int) {
	m.unexecutedTxs.Sub(float64(n))
}

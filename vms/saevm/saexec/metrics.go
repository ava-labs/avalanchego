// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package saexec

import (
	"errors"
	"fmt"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/ava-labs/avalanchego/vms/saevm/blocks"
	"github.com/ava-labs/avalanchego/vms/saevm/gastime"
	"github.com/ava-labs/avalanchego/vms/saevm/hook"
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

	// lastExecutedGasTime is the gas time reached by the latest executed
	// block; gasTimeWallTimeGap is its gap to the wall time at execution
	// completion.
	lastExecutedGasTime prometheus.Gauge
	gasTimeWallTimeGap  prometheus.Gauge

	// accepted* are the worst-case pricing values admitted by consensus for
	// the latest enqueued block; executed* are the values realized by
	// execution. gasTarget has no such pair because execution never moves it.
	acceptedBaseFee   prometheus.Gauge
	executedBaseFee   prometheus.Gauge
	acceptedGasExcess prometheus.Gauge
	executedGasExcess prometheus.Gauge
	gasTarget         prometheus.Gauge
}

func newMetrics(reg prometheus.Registerer, lastExecuted *blocks.Block, hooks hook.Points) (*metrics, error) {
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
		lastExecutedGasTime: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "last_executed_gas_time_timestamp_seconds",
			Help: "Gas time reached by the latest executed block, as a Unix timestamp.",
		}),
		gasTimeWallTimeGap: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "gas_time_wall_time_gap_seconds",
			Help: "Gas time minus wall time, observed when the latest block finished executing; negative when gas time lags the wall clock.",
		}),
		acceptedBaseFee: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "accepted_base_fee",
			Help: "Worst-case base fee admitted by consensus for the latest block accepted into the execution queue.",
		}),
		executedBaseFee: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "executed_base_fee",
			Help: "Base fee realized by execution of the latest executed block.",
		}),
		acceptedGasExcess: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "accepted_gas_excess",
			Help: "Worst-case gas excess admitted by consensus for the latest block accepted into the execution queue.",
		}),
		executedGasExcess: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "executed_gas_excess",
			Help: "Gas excess realized by execution of the latest executed block.",
		}),
		gasTarget: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "gas_target",
			Help: "ACP-176 gas target in force as of the latest block accepted into the execution queue.",
		}),
	}

	// Seed the gauges from the last-executed block so startup and steady state
	// report the same signals.
	worstCase, err := worstCaseGasTime(hooks, lastExecuted.Header())
	if err != nil {
		return nil, fmt.Errorf("deriving worst-case gas time of block %d: %w", lastExecuted.Height(), err)
	}
	m.lastExecutedHeight.Set(float64(lastExecuted.Height()))
	m.markAccepted(worstCase)
	m.markExecutedGasTime(lastExecuted.ExecutedByGasTime(), time.Now())

	return m, errors.Join(
		reg.Register(m.lastExecutedHeight),
		reg.Register(m.queueDuration),
		reg.Register(m.executeBlockDuration),
		reg.Register(m.executionQueueBlocks),
		reg.Register(m.executionQueueGasLimit),
		reg.Register(m.executedGasCharged),
		reg.Register(m.executedGasLimit),
		reg.Register(m.acceptedGasLimit),
		reg.Register(m.lastExecutedGasTime),
		reg.Register(m.gasTimeWallTimeGap),
		reg.Register(m.acceptedBaseFee),
		reg.Register(m.executedBaseFee),
		reg.Register(m.acceptedGasExcess),
		reg.Register(m.executedGasExcess),
		reg.Register(m.gasTarget),
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

// markAccepted records the worst-case pricing admitted by consensus for the
// most recently enqueued block: base fee, gas excess, and gas target.
func (m *metrics) markAccepted(worstCase *gastime.Time) {
	m.acceptedBaseFee.Set(float64(worstCase.Price()))
	m.acceptedGasExcess.Set(float64(worstCase.Excess()))
	m.gasTarget.Set(float64(worstCase.Target()))
}

// markExecuted records that the block at the given height, with the given gas
// limit, has finished executing, having been charged gasCharged gas and
// advanced the gas-time clock to executedBy at wall time wall. It advances the
// last-executed height and gas-time gauges, removes the block from the queue
// gauges, and adds to the cumulative executed totals.
func (m *metrics) markExecuted(height, gasCharged, gasLimit uint64, executedBy *gastime.Time, wall time.Time) {
	m.lastExecutedHeight.Set(float64(height))
	m.executionQueueBlocks.Dec()
	m.executionQueueGasLimit.Sub(float64(gasLimit))
	m.executedGasCharged.Add(float64(gasCharged))
	m.executedGasLimit.Add(float64(gasLimit))
	m.markExecutedGasTime(executedBy, wall)
}

// markExecutedGasTime records the gas-time state realized by the most recently
// executed block: the gas-time clock reading, its gap to the given wall time,
// and the realized base fee and gas excess.
func (m *metrics) markExecutedGasTime(executedBy *gastime.Time, wall time.Time) {
	gasTime := executedBy.AsTime()
	m.lastExecutedGasTime.Set(float64(gasTime.UnixNano()) / 1e9)
	m.gasTimeWallTimeGap.Set(gasTime.Sub(wall).Seconds())
	m.executedBaseFee.Set(float64(executedBy.Price()))
	m.executedGasExcess.Set(float64(executedBy.Excess()))
}

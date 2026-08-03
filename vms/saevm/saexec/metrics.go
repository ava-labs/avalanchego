// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package saexec

import (
	"errors"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/ava-labs/avalanchego/vms/saevm/blocks"
)

var (
	// queueDurationBuckets span 1ms (executor keeping up) to ~16s (deep backlog).
	queueDurationBuckets = prometheus.ExponentialBuckets(time.Millisecond.Seconds(), 2, 15)
	// executeBlockBuckets span 500µs (small block) to ~16s (large/slow block).
	executeBlockBuckets = prometheus.ExponentialBuckets(500*time.Microsecond.Seconds(), 2, 16)
)

type metrics struct {
	lastExecutedHeight prometheus.Gauge

	// queueDuration spans acceptance until execution completes, so it contains
	// executeBlockDuration.
	queueDuration        prometheus.Histogram
	executeBlockDuration prometheus.Histogram

	// Blocks accepted but not yet executed, including the one executing, and
	// the worst-case gas they may be charged.
	executionQueueBlocks   prometheus.Gauge
	executionQueueGasLimit prometheus.Gauge

	// executedGasCharged is the gas that executed blocks consumed, transactions
	// plus end-of-block ops. executedGasLimit is the worst-case gas they could
	// have been charged, which is what an SAE header reports as its gas used.
	executedGasCharged prometheus.Counter
	executedGasLimit   prometheus.Counter

	// acceptedGasLimit is the acceptance-side counterpart of executedGasLimit.
	acceptedGasLimit prometheus.Counter

	// lastExecutedGasTime is the latest executed block's gas time.
	// gasTimeWallTimeGap is its gap to wall time.
	lastExecutedGasTime prometheus.Gauge
	gasTimeWallTimeGap  prometheus.Gauge

	// worstCaseBaseFee is the base fee consensus required for the latest
	// enqueued block. executedBaseFee is the fee the executor actually charged.
	worstCaseBaseFee prometheus.Gauge
	executedBaseFee  prometheus.Gauge

	// worstCaseGasExcess is the excess after simulating the latest enqueued
	// block. executedGasExcess is the excess execution actually realized.
	worstCaseGasExcess prometheus.Gauge
	executedGasExcess  prometheus.Gauge

	// Execution never moves the target, so there is no executed counterpart.
	gasTarget prometheus.Gauge
}

func newMetrics(reg prometheus.Registerer, lastExecuted *blocks.Block) (*metrics, error) {
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
			Help: "Sum of the transaction gas limits in accepted blocks that have not yet completed execution.",
		}),
		executedGasCharged: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "executed_gas_charged_total",
			Help: "Cumulative gas charged by executed blocks (transaction gas used plus end-of-block operation gas); this is not the eth gas used.",
		}),
		executedGasLimit: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "executed_gas_limit_total",
			Help: "Cumulative transaction gas limits in executed blocks.",
		}),
		acceptedGasLimit: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "accepted_gas_limit_total",
			Help: "Cumulative transaction gas limits in accepted blocks.",
		}),
		lastExecutedGasTime: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "last_executed_gas_time_seconds",
			Help: "Gas time reached by the latest executed block, as a Unix timestamp.",
		}),
		gasTimeWallTimeGap: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "gas_time_wall_time_gap_seconds",
			Help: "Gas time minus wall time, observed when the latest block finished executing; negative when gas time lags the wall clock.",
		}),
		worstCaseBaseFee: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "worst_case_base_fee",
			Help: "Worst-case base fee admitted by consensus for the latest enqueued block.",
		}),
		executedBaseFee: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "executed_base_fee",
			Help: "Base fee realized by execution of the latest executed block.",
		}),
		worstCaseGasExcess: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "worst_case_gas_excess",
			Help: "Worst-case gas excess simulated for the latest enqueued block.",
		}),
		executedGasExcess: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "executed_gas_excess",
			Help: "Gas excess realized by execution of the latest executed block.",
		}),
		gasTarget: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "gas_target",
			Help: "ACP-176 gas target in force as of the latest enqueued block.",
		}),
	}

	// Seed the gauges from the last executed block.
	m.setExecuted(lastExecuted)

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
		reg.Register(m.worstCaseBaseFee),
		reg.Register(m.executedBaseFee),
		reg.Register(m.worstCaseGasExcess),
		reg.Register(m.executedGasExcess),
		reg.Register(m.gasTarget),
	)
}

// markEnqueued records that the block has been accepted into the execution
// queue.
func (m *metrics) markEnqueued(block *blocks.Block) {
	m.executionQueueBlocks.Inc()
	worstCaseGas := float64(block.EthBlock().GasUsed())
	m.executionQueueGasLimit.Add(worstCaseGas)
	m.acceptedGasLimit.Add(worstCaseGas)
}

func (m *metrics) observeQueueDuration(d time.Duration) {
	m.queueDuration.Observe(d.Seconds())
}

// markExecuted records that the block has finished executing with the given
// results.
func (m *metrics) markExecuted(block *blocks.Block, results *ExecutionResults) {
	m.executeBlockDuration.Observe(results.Consumed.Time.Seconds())
	m.executionQueueBlocks.Dec()
	worstCaseGas := float64(block.EthBlock().GasUsed())
	m.executionQueueGasLimit.Sub(worstCaseGas)
	m.executedGasCharged.Add(float64(results.Consumed.Gas))
	m.executedGasLimit.Add(worstCaseGas)
	m.setExecuted(block)
}

func (m *metrics) setExecuted(block *blocks.Block) {
	m.lastExecutedHeight.Set(float64(block.NumberU64()))

	gasTime := block.ExecutedByGasTime()
	gasClock := gasTime.AsTime()
	m.lastExecutedGasTime.Set(float64(gasClock.UnixNano()) / 1e9)
	m.gasTimeWallTimeGap.Set(time.Since(gasClock).Seconds())

	m.worstCaseBaseFee.Set(float64(block.HeaderBaseFee()))
	m.executedBaseFee.Set(block.ExecutedBaseFee().Float64())

	// Blocks accepted while bootstrapping, and those replayed during recovery,
	// don't have their bounds set, so the gauges default to the executed value.
	if bounds := block.WorstCaseBounds(); bounds != nil {
		m.worstCaseGasExcess.Set(float64(bounds.LatestEndTime.Excess()))
	} else {
		m.worstCaseGasExcess.Set(float64(gasTime.Excess()))
	}
	m.executedGasExcess.Set(float64(gasTime.Excess()))
	m.gasTarget.Set(float64(gasTime.Target()))
}

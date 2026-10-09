// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package saexec

import (
	"errors"
	"math"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/ava-labs/avalanchego/utils/buffer"
	"github.com/ava-labs/avalanchego/utils/math/intmath"
	"github.com/ava-labs/avalanchego/vms/components/gas"
	"github.com/ava-labs/avalanchego/vms/saevm/blocks"

	saeparams "github.com/ava-labs/avalanchego/vms/saevm/params"
)

var (
	// queueDurationBuckets span 1ms (executor keeping up) to ~16s (deep backlog).
	queueDurationBuckets = prometheus.ExponentialBuckets(time.Millisecond.Seconds(), 2, 15)
	// executeBlockBuckets span 500µs (small block) to ~16s (large/slow block).
	executeBlockBuckets = prometheus.ExponentialBuckets(500*time.Microsecond.Seconds(), 2, 16)
	// gasRateHeadroomBuckets define the [rollingGasThroughput.headroom] histogram.
	gasRateHeadroomBuckets = []float64{
		0.8,                            // Terrain, terrain!
		1,                              // lagging across [saeparams.Tau]: worth alerting
		1.05, 1.1, 1.15, 1.2, 1.3, 1.4, // finer resolution when close
		1.6, 2, 2.5, 4, 10, 30, 100, // coarser resolution when far
	}
)

type metrics struct {
	lastExecutedHeight prometheus.Gauge

	// queueDuration spans acceptance until execution completes, so it contains
	// executeBlockDuration.
	queueDuration        prometheus.Histogram
	executeBlockDuration prometheus.Histogram

	// executionQueueBlocks are the blocks accepted but not yet executed,
	// including the one executing. executionQueueGasLimit is the worst-case gas
	// the blocks in the queue may be charged.
	executionQueueBlocks   prometheus.Gauge
	executionQueueGasLimit prometheus.Gauge

	// executedGasCharged is the gas that executed blocks consumed.
	// executedGasLimit is the worst-case gas they could have been charged.
	executedGasCharged        prometheus.Counter
	executedGasLimit          prometheus.Counter
	averageExecutedGasCharged rollingGasThroughput

	// acceptedGasLimit is the acceptance-side counterpart of executedGasLimit.
	acceptedGasLimit prometheus.Counter

	// lastExecutedGasTime is the latest block's gas time after execution.
	// gasTimeWallTimeGap is its gap to the wall time immediately after
	// execution.
	lastExecutedGasTime prometheus.Gauge
	gasTimeWallTimeGap  prometheus.Gauge

	// Both pairs describe the latest executed block, so each gap is what
	// consensus over-committed to. The base fee is measured at the start of the
	// block, while the excess is measured at the end of the block.
	worstCaseBaseFee   prometheus.Gauge
	executedBaseFee    prometheus.Gauge
	worstCaseGasExcess prometheus.Gauge
	executedGasExcess  prometheus.Gauge

	// The target exactly matches between simulation and execution, so only
	// one value is reported.
	gasTarget prometheus.Gauge
}

// rollingGasThroughput tracks execution over a rolling [saeparams.Tau] window,
// publishing after each block.
type rollingGasThroughput struct {
	history buffer.Deque[executionSpan]
	total   struct {
		gas               gas.Gas
		wallTime, gasTime time.Duration
	}

	// headroom is the factor by which the gas rate could have been multiplied
	// for gas- and wall-clock time to have advanced by the same duration;
	// values below 1 mean that execution is falling behind.
	headroom       prometheus.Histogram
	latestHeadroom prometheus.Gauge
	// gasPerSecond is the raw, rolling-average throughput.
	gasPerSecond prometheus.Gauge
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
			Help:    "Wall-clock time to execute a single block, including the state commit and post-execution work.",
			Buckets: executeBlockBuckets,
		}),
		executionQueueBlocks: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "execution_queue_blocks",
			Help: "Number of accepted blocks that have not yet completed execution.",
		}),
		executionQueueGasLimit: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "execution_queue_gas_limit",
			Help: "Worst-case gas of accepted blocks that have not yet completed execution.",
		}),
		executedGasCharged: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "executed_gas_charged_total",
			Help: "Cumulative gas charged by executed blocks, transaction gas used plus end-of-block operation gas.",
		}),
		executedGasLimit: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "executed_gas_limit_total",
			Help: "Cumulative worst-case gas of executed blocks, transaction gas limits plus end-of-block operation gas.",
		}),
		averageExecutedGasCharged: rollingGasThroughput{
			history: buffer.NewUnboundedDeque[executionSpan](0),
			headroom: prometheus.NewHistogram(prometheus.HistogramOpts{
				Name:    "execution_gas_rate_headroom",
				Help:    "Factor by which the gas rate could have been multiplied for executed gas time to have equalled elapsed wall time, over a rolling window of tau execution, and observed after each block. Below 1 means execution is falling behind.",
				Buckets: gasRateHeadroomBuckets,
			}),
			latestHeadroom: prometheus.NewGauge(prometheus.GaugeOpts{
				Name: "execution_gas_rate_headroom_latest",
				Help: "Latest value observed by execution_gas_rate_headroom.",
			}),
			gasPerSecond: prometheus.NewGauge(prometheus.GaugeOpts{
				Name: "executed_gas_per_second",
				Help: "Gas consumed per second of wall time spent executing, over a rolling tau window of execution, as of the latest executed block.",
			}),
		},
		acceptedGasLimit: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "accepted_gas_limit_total",
			Help: "Cumulative worst-case gas of blocks accepted into the execution queue.",
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
			Help: "Worst-case base fee admitted by consensus for the latest executed block.",
		}),
		executedBaseFee: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "executed_base_fee",
			Help: "Base fee realized by execution of the latest executed block.",
		}),
		worstCaseGasExcess: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "worst_case_gas_excess",
			Help: "Worst-case gas excess simulated for the latest executed block.",
		}),
		executedGasExcess: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "executed_gas_excess",
			Help: "Gas excess realized by execution of the latest executed block.",
		}),
		gasTarget: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "gas_target",
			Help: "ACP-176 gas target in force as of the latest executed block.",
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
		reg.Register(m.averageExecutedGasCharged.headroom),
		reg.Register(m.averageExecutedGasCharged.latestHeadroom),
		reg.Register(m.averageExecutedGasCharged.gasPerSecond),
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
	worstCaseGas := float64(block.WorstCaseGasUsed())
	m.executionQueueGasLimit.Add(worstCaseGas)
	m.acceptedGasLimit.Add(worstCaseGas)
}

func (m *metrics) observeQueueDuration(d time.Duration) {
	m.queueDuration.Observe(d.Seconds())
}

// markExecuted records that the block has finished executing with the given
// results.
func (m *metrics) markExecuted(block *blocks.Block, results *ExecutionResults) {
	m.executionQueueBlocks.Dec()
	// MUST use the same worst-case gas value as [metrics.markEnqueued].
	worstCaseGas := float64(block.WorstCaseGasUsed())
	m.executionQueueGasLimit.Sub(worstCaseGas)
	m.executedGasCharged.Add(float64(results.GasConsumed))
	m.executedGasLimit.Add(worstCaseGas)
	m.setExecuted(block)
}

func (m *metrics) setExecuted(block *blocks.Block) {
	m.lastExecutedHeight.Set(float64(block.NumberU64()))

	gasTime := block.ExecutedByGasTime()
	gasClock := gasTime.AsTime()
	m.lastExecutedGasTime.Set(float64(gasClock.UnixNano()) / 1e9)
	// Restored blocks have no wall time. Subtracting the zero [time.Time]
	// maxes out the [time.Duration], so report the gap as unknown.
	if byWall := block.ExecutedByWallTime(); byWall.IsZero() {
		m.gasTimeWallTimeGap.Set(math.NaN())
	} else {
		m.gasTimeWallTimeGap.Set(gasClock.Sub(byWall).Seconds())
	}

	m.worstCaseBaseFee.Set(block.WorstCaseBaseFee().Float64())
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

func (m *metrics) observeExecuteDuration(start, end time.Time, b *blocks.Block, res *ExecutionResults) {
	m.executeBlockDuration.Observe(end.Sub(start).Seconds())

	if res == nil /* e.g. on error */ || b.Synchronous() {
		return
	}
	m.averageExecutedGasCharged.observe(executionSpan{
		start:    start,
		end:      end,
		consumed: res.GasConsumed,
		rate:     res.rate,
	})
}

type executionSpan struct {
	start, end     time.Time
	consumed, rate gas.Gas
}

func (r *rollingGasThroughput) observe(s executionSpan) {
	r.history.PushRight(s)
	r.total.gas += s.consumed
	r.total.wallTime += s.wallTime()
	r.total.gasTime += s.gasTime()

	// The observation pushed above can never be evicted so peeking will never
	// fail. Observations that straddle the [saeparams.Tau] window are retained,
	// whole, for simplicity.
	for cutoff := s.end.Add(-saeparams.Tau); ; {
		old, _ := r.history.PeekLeft()
		if !old.end.Before(cutoff) {
			break
		}
		r.history.PopLeft()
		r.total.gas -= old.consumed
		r.total.wallTime -= old.wallTime()
		r.total.gasTime -= old.gasTime()
	}

	headroom := float64(r.total.gasTime) / float64(r.total.wallTime)
	r.headroom.Observe(headroom)
	r.latestHeadroom.Set(headroom)
	r.gasPerSecond.Set(float64(r.total.gas) / r.total.wallTime.Seconds())
}

func (s *executionSpan) gasTime() time.Duration {
	// See gosec disabling rationale for why we ignore the (impossible) overflow
	// error.
	ns, _, _ := intmath.MulDiv(
		s.consumed,                           // gas
		gas.Gas(time.Second/time.Nanosecond), // no unit
		s.rate,                               // gas/time
	)
	return time.Duration(ns) //nolint:gosec // Known to be O(seconds) so won't overflow int64
}

func (s *executionSpan) wallTime() time.Duration {
	return s.end.Sub(s.start)
}

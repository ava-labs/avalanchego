// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package prometheus

import (
	"strings"
	"testing"
	"time"

	"github.com/ava-labs/libevm/metrics"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/vms/evm/metrics/metricstest"
)

const expectedMetrics = `
	# HELP test_counter
	# TYPE test_counter counter
	test_counter 12345
	# HELP test_counter_float64
	# TYPE test_counter_float64 counter
	test_counter_float64 1.1
	# HELP test_empty_resetting_timer
	# TYPE test_empty_resetting_timer summary
	test_empty_resetting_timer{quantile="50"} 0
	test_empty_resetting_timer{quantile="95"} 0
	test_empty_resetting_timer{quantile="99"} 0
	test_empty_resetting_timer_sum 0
	test_empty_resetting_timer_count 0
	# HELP test_gauge
	# TYPE test_gauge gauge
	test_gauge 23456
	# HELP test_gauge_float64
	# TYPE test_gauge_float64 gauge
	test_gauge_float64 34567.89
	# HELP test_histogram
	# TYPE test_histogram summary
	test_histogram{quantile="0.5"} 0
	test_histogram{quantile="0.75"} 0
	test_histogram{quantile="0.95"} 0
	test_histogram{quantile="0.99"} 0
	test_histogram{quantile="0.999"} 0
	test_histogram{quantile="0.9999"} 0
	test_histogram_sum 0
	test_histogram_count 0
	# HELP test_meter
	# TYPE test_meter gauge
	test_meter 9.999999e+06
	# HELP test_resetting_timer
	# TYPE test_resetting_timer summary
	test_resetting_timer{quantile="50"} 1e+09
	test_resetting_timer{quantile="95"} 1e+09
	test_resetting_timer{quantile="99"} 1e+09
	test_resetting_timer_sum 1e+09
	test_resetting_timer_count 1
	# HELP test_timer
	# TYPE test_timer summary
	test_timer{quantile="0.5"} 2.25e+07
	test_timer{quantile="0.75"} 4.8e+07
	test_timer{quantile="0.95"} 1.2e+08
	test_timer{quantile="0.99"} 1.2e+08
	test_timer{quantile="0.999"} 1.2e+08
	test_timer{quantile="0.9999"} 1.2e+08
	test_timer_sum 2.3e+08
	test_timer_count 6
`

func TestGatherer_Gather(t *testing.T) {
	metricstest.WithMetrics(t)

	registry := metrics.NewRegistry()
	register := func(t *testing.T, name string, collector any) {
		t.Helper()
		err := registry.Register(name, collector)
		require.NoErrorf(t, err, "registering collector %q", name)
	}

	registerNilMetrics(t, register)
	registerRealMetrics(t, register)

	gatherer := NewGatherer(registry)

	// Test successful gathering.
	//
	// TODO: This results in resetting the timer, is this expected behavior?
	require.NoError(t, testutil.GatherAndCompare(
		gatherer,
		strings.NewReader(expectedMetrics),
	))

	wantMetrics, err := gatherer.Gather()
	require.NoError(t, err)

	// Test gathering with unsupported metric type
	register(t, "unsupported", metrics.NewHealthcheck(nil))
	metrics, err := gatherer.Gather()
	require.ErrorIs(t, err, errMetricTypeNotSupported)
	require.Equal(t, wantMetrics, metrics)
}

func registerRealMetrics(t *testing.T, register func(t *testing.T, name string, collector any)) {
	counter := metrics.NewCounter()
	counter.Inc(12345)
	register(t, "test/counter", counter)

	counterFloat64 := metrics.NewCounterFloat64()
	counterFloat64.Inc(1.1)
	register(t, "test/counter_float64", counterFloat64)

	gauge := metrics.NewGauge()
	gauge.Update(23456)
	register(t, "test/gauge", gauge)

	gaugeFloat64 := metrics.NewGaugeFloat64()
	gaugeFloat64.Update(34567.89)
	register(t, "test/gauge_float64", gaugeFloat64)

	gaugeInfo := metrics.NewGaugeInfo()
	gaugeInfo.Update(metrics.GaugeInfoValue{"key": "value"})
	register(t, "test/gauge_info", gaugeInfo) // skipped

	sample := metrics.NewUniformSample(1028)
	histogram := metrics.NewHistogram(sample)
	register(t, "test/histogram", histogram)

	meter := metrics.NewMeter()
	t.Cleanup(meter.Stop)
	meter.Mark(9999999)
	register(t, "test/meter", meter)

	timer := metrics.NewTimer()
	t.Cleanup(timer.Stop)
	timer.Update(20 * time.Millisecond)
	timer.Update(21 * time.Millisecond)
	timer.Update(22 * time.Millisecond)
	timer.Update(120 * time.Millisecond)
	timer.Update(23 * time.Millisecond)
	timer.Update(24 * time.Millisecond)
	register(t, "test/timer", timer)

	resettingTimer := metrics.NewResettingTimer()
	register(t, "test/resetting_timer", resettingTimer)
	resettingTimer.Update(time.Second) // must be after register call

	emptyResettingTimer := metrics.NewResettingTimer()
	register(t, "test/empty_resetting_timer", emptyResettingTimer)

	emptyResettingTimer.Update(time.Second) // no effect because of snapshot below
	register(t, "test/empty_resetting_timer_snapshot", emptyResettingTimer.Snapshot())
}

func registerNilMetrics(t *testing.T, register func(t *testing.T, name string, collector any)) {
	// The NewXXX metrics functions return nil metrics types when the metrics
	// are disabled.
	metrics.Enabled = false
	defer func() { metrics.Enabled = true }()

	register(t, "nil/counter", metrics.NewCounter())
	register(t, "nil/counter_float64", metrics.NewCounterFloat64())
	register(t, "nil/ewma", &metrics.NilEWMA{})
	register(t, "nil/gauge", metrics.NewGauge())
	register(t, "nil/gauge_float64", metrics.NewGaugeFloat64())
	register(t, "nil/gauge_info", metrics.NewGaugeInfo())
	register(t, "nil/healthcheck", metrics.NewHealthcheck(nil))
	register(t, "nil/histogram", metrics.NewHistogram(nil))
	register(t, "nil/meter", metrics.NewMeter())
	register(t, "nil/resetting_timer", metrics.NewResettingTimer())
	register(t, "nil/sample", metrics.NewUniformSample(1028))
	register(t, "nil/timer", metrics.NewTimer())
}

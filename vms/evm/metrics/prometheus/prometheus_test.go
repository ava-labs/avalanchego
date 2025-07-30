// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package prometheus

import (
	"strings"
	"testing"
	"time"

	"github.com/ava-labs/libevm/metrics"
	"github.com/prometheus/common/expfmt"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/vms/evm/metrics/metricstest"
)

func TestGatherer_Gather(t *testing.T) {
	metricstest.WithMetrics(t)

	registry := metrics.NewRegistry()
	register := func(t *testing.T, name string, collector any) {
		t.Helper()
		err := registry.Register(name, collector)
		require.NoErrorf(t, err, "registering collector %q", name)
	}

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

	registerNilMetrics(t, register)

	gatherer := NewGatherer(registry)

	families, err := gatherer.Gather()
	require.NoError(t, err)

	const expectedString = `
# TYPE test_counter counter
test_counter 12345
# TYPE test_counter_float64 counter
test_counter_float64 1.1
# TYPE test_gauge gauge
test_gauge 23456
# TYPE test_gauge_float64 gauge
test_gauge_float64 34567.89
# TYPE test_histogram summary
test_histogram{quantile="0.5"} 0
test_histogram{quantile="0.75"} 0
test_histogram{quantile="0.95"} 0
test_histogram{quantile="0.99"} 0
test_histogram{quantile="0.999"} 0
test_histogram{quantile="0.9999"} 0
test_histogram_sum 0
test_histogram_count 0
# TYPE test_meter gauge
test_meter 9.999999e+06
# TYPE test_resetting_timer summary
test_resetting_timer{quantile="50"} 1e+09
test_resetting_timer{quantile="95"} 1e+09
test_resetting_timer{quantile="99"} 1e+09
test_resetting_timer_sum 1e+09
test_resetting_timer_count 1
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
	var (
		stringReader = strings.NewReader(expectedString)
		parser       expfmt.TextParser
	)
	expectedMetrics, err := parser.TextToMetricFamilies(stringReader)
	require.NoError(t, err)

	require.Len(t, families, len(expectedMetrics))
	for i, got := range families {
		require.NotNil(t, *got.Name)

		want := expectedMetrics[*got.Name]
		require.Equal(t, want, got, i)
	}

	register(t, "unsupported", metrics.NewHealthcheck(nil))
	families, err = gatherer.Gather()
	require.ErrorIs(t, err, errMetricTypeNotSupported)
	require.Empty(t, families)
}

func registerNilMetrics(t *testing.T, register func(t *testing.T, name string, collector any)) {
	// The NewXXX metrics functions return nil metrics types when the metrics
	// are disabled.
	metrics.Enabled = false
	defer func() { metrics.Enabled = true }()
	nilCounter := metrics.NewCounter()
	register(t, "nil/counter", nilCounter)
	nilCounterFloat64 := metrics.NewCounterFloat64()
	register(t, "nil/counter_float64", nilCounterFloat64)
	nilEWMA := &metrics.NilEWMA{}
	register(t, "nil/ewma", nilEWMA)
	nilGauge := metrics.NewGauge()
	register(t, "nil/gauge", nilGauge)
	nilGaugeFloat64 := metrics.NewGaugeFloat64()
	register(t, "nil/gauge_float64", nilGaugeFloat64)
	nilGaugeInfo := metrics.NewGaugeInfo()
	register(t, "nil/gauge_info", nilGaugeInfo)
	nilHealthcheck := metrics.NewHealthcheck(nil)
	register(t, "nil/healthcheck", nilHealthcheck)
	nilHistogram := metrics.NewHistogram(nil)
	register(t, "nil/histogram", nilHistogram)
	nilMeter := metrics.NewMeter()
	register(t, "nil/meter", nilMeter)
	nilResettingTimer := metrics.NewResettingTimer()
	register(t, "nil/resetting_timer", nilResettingTimer)
	nilSample := metrics.NewUniformSample(1028)
	register(t, "nil/sample", nilSample)
	nilTimer := metrics.NewTimer()
	register(t, "nil/timer", nilTimer)
}

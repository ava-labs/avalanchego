// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package prometheus

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/coreth/metrics/metricstest"
	"github.com/ava-labs/libevm/metrics"
)

func TestGatherer_Gather(t *testing.T) {
	metricstest.WithMetrics(t)

	registry := metrics.NewRegistry()
	register := func(t *testing.T, name string, collector any) {
		t.Helper()
		require.NoError(t, registry.Register(name, collector))
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

	gatherer := NewGatherer(registry)

	families, err := gatherer.Gather()
	require.NoError(t, err)

	familyStrings := make([]string, len(families))
	for i := range families {
		familyStrings[i] = families[i].String()
	}
	want := []string{
		`name:"test_counter" type:COUNTER metric:{counter:{value:12345}}`,
		`name:"test_counter_float64" type:COUNTER metric:{counter:{value:1.1}}`,
		`name:"test_gauge" type:GAUGE metric:{gauge:{value:23456}}`,
		`name:"test_gauge_float64" type:GAUGE metric:{gauge:{value:34567.89}}`,
		`name:"test_histogram" type:SUMMARY metric:{summary:{sample_count:0 sample_sum:0 quantile:{quantile:0.5 value:0} quantile:{quantile:0.75 value:0} quantile:{quantile:0.95 value:0} quantile:{quantile:0.99 value:0} quantile:{quantile:0.999 value:0} quantile:{quantile:0.9999 value:0}}}`,
		`name:"test_meter" type:GAUGE metric:{gauge:{value:9.999999e+06}}`,
		`name:"test_resetting_timer" type:SUMMARY metric:{summary:{sample_count:1 quantile:{quantile:50 value:1e+09} quantile:{quantile:95 value:1e+09} quantile:{quantile:99 value:1e+09}}}`,
		`name:"test_timer" type:SUMMARY metric:{summary:{sample_count:6 sample_sum:2.3e+08 quantile:{quantile:0.5 value:2.25e+07} quantile:{quantile:0.75 value:4.8e+07} quantile:{quantile:0.95 value:1.2e+08} quantile:{quantile:0.99 value:1.2e+08} quantile:{quantile:0.999 value:1.2e+08} quantile:{quantile:0.9999 value:1.2e+08}}}`,
	}
	assert.Equal(t, want, familyStrings)

	register(t, "unsupported", metrics.NewHealthcheck(nil))
	families, err = gatherer.Gather()
	assert.ErrorIs(t, err, errMetricTypeNotSupported)
	assert.Empty(t, families)
}

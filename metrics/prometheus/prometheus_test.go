// (c) 2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package prometheus

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/ava-labs/subnet-evm/metrics"
)

func TestGatherer(t *testing.T) {
	registry := metrics.NewRegistry()

	counter := metrics.NewCounter()
	counter.Inc(12345)

	err := registry.Register("test/counter", counter)
	assert.NoError(t, err)

	gauge := metrics.NewGauge()
	gauge.Update(23456)

	err = registry.Register("test/gauge", gauge)
	assert.NoError(t, err)

	gaugeFloat64 := metrics.NewGaugeFloat64()
	gaugeFloat64.Update(34567.89)

	err = registry.Register("test/gauge_float64", gaugeFloat64)
	assert.NoError(t, err)

	sample := metrics.NewUniformSample(1028)
	histogram := metrics.NewHistogram(sample)

	err = registry.Register("test/histogram", histogram)
	assert.NoError(t, err)

	meter := metrics.NewMeter()
	defer meter.Stop()
	meter.Mark(9999999)

	err = registry.Register("test/meter", meter)
	assert.NoError(t, err)

	timer := metrics.NewTimer()
	defer timer.Stop()
	timer.Update(20 * time.Millisecond)
	timer.Update(21 * time.Millisecond)
	timer.Update(22 * time.Millisecond)
	timer.Update(120 * time.Millisecond)
	timer.Update(23 * time.Millisecond)
	timer.Update(24 * time.Millisecond)

	err = registry.Register("test/timer", timer)
	assert.NoError(t, err)

	resettingTimer := metrics.NewResettingTimer()
	resettingTimer.Update(10 * time.Millisecond)
	resettingTimer.Update(11 * time.Millisecond)
	resettingTimer.Update(12 * time.Millisecond)
	resettingTimer.Update(120 * time.Millisecond)
	resettingTimer.Update(13 * time.Millisecond)
	resettingTimer.Update(14 * time.Millisecond)

	err = registry.Register("test/resetting_timer", resettingTimer)
	assert.NoError(t, err)

	err = registry.Register("test/resetting_timer_snapshot", resettingTimer.Snapshot())
	assert.NoError(t, err)

	emptyResettingTimer := metrics.NewResettingTimer()

	err = registry.Register("test/empty_resetting_timer", emptyResettingTimer)
	assert.NoError(t, err)

	err = registry.Register("test/empty_resetting_timer_snapshot", emptyResettingTimer.Snapshot())
	assert.NoError(t, err)

	g := Gatherer(registry)

	_, err = g.Gather()
	assert.NoError(t, err)
}

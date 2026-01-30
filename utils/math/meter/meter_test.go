// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package meter

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

var (
	halflife = time.Second
	meters   = []struct {
		name    string
		factory Factory
	}{
		{
			name:    "continuous",
			factory: ContinuousFactory{},
		},
	}

	meterTests = []struct {
		name string
		test func(*testing.T, Factory)
	}{
		{
			name: "new",
			test: NewTest,
		},
		{
			name: "standard usage",
			test: StandardUsageTest,
		},
		{
			name: "time travel",
			test: TimeTravelTest,
		},
	}
)

func TestMeters(t *testing.T) {
	for _, s := range meters {
		for _, test := range meterTests {
			t.Run(fmt.Sprintf("meter %s test %s", s.name, test.name), func(t *testing.T) {
				test.test(t, s.factory)
			})
		}
	}
}

func NewTest(t *testing.T, factory Factory) {
	require.NotNil(t, factory.New(halflife))
}

func TimeTravelTest(t *testing.T, factory Factory) {
	require := require.New(t)

	m := factory.New(halflife)

	now := time.Date(1, 2, 3, 4, 5, 6, 7, time.UTC)
	m.Inc(now, 1)

	now = now.Add(halflife - 1)
	delta := 0.0001
	require.InDelta(.5, m.Read(now), delta)

	m.Dec(now, 1)

	now = now.Add(-halflife)
	require.InDelta(.5, m.Read(now), delta)

	m.Inc(now, 1)

	now = now.Add(halflife / 2)
	require.InDelta(.5, m.Read(now), delta)
}

func StandardUsageTest(t *testing.T, factory Factory) {
	require := require.New(t)

	m := factory.New(halflife)

	now := time.Date(1, 2, 3, 4, 5, 6, 7, time.UTC)
	m.Inc(now, 1)

	now = now.Add(halflife - 1)
	delta := 0.0001
	require.InDelta(.5, m.Read(now), delta)

	m.Inc(now, 1)
	require.InDelta(.5, m.Read(now), delta)

	m.Dec(now, 1)
	require.InDelta(.5, m.Read(now), delta)

	m.Dec(now, 1)

	require.InDelta(.5, m.Read(now), delta)

	now = now.Add(halflife)
	require.InDelta(.25, m.Read(now), delta)

	m.Inc(now, 1)

	now = now.Add(halflife)
	require.InDelta(.625, m.Read(now), delta)

	now = now.Add(34 * halflife)
	require.InDelta(1, m.Read(now), delta)

	m.Dec(now, 1)

	now = now.Add(34 * halflife)
	require.InDelta(0, m.Read(now), delta)

	m.Inc(now, 1)

	now = now.Add(2 * halflife)
	require.InDelta(.75, m.Read(now), delta)

	// Second start
	m.Inc(now, 1)

	now = now.Add(34 * halflife)
	require.InDelta(2, m.Read(now), delta)

	// Stop the second CPU
	m.Dec(now, 1)

	now = now.Add(34 * halflife)
	require.InDelta(1, m.Read(now), delta)
}

func TestTimeUntil(t *testing.T) {
	require := require.New(t)

	halflife := 5 * time.Second
	f := ContinuousFactory{}
	m := f.New(halflife)
	now := time.Now()
	// Start the meter
	m.Inc(now, 1)
	// One halflife passes; stop the meter
	now = now.Add(halflife)
	m.Dec(now, 1)
	// Read the current value
	currentVal := m.Read(now)
	// Suppose we want to wait for the value to be
	// a third of its current value
	desiredVal := currentVal / 3
	// See when that should happen
	timeUntilDesiredVal := m.TimeUntil(now, desiredVal)
	// Get the actual value at that time
	now = now.Add(timeUntilDesiredVal)
	actualVal := m.Read(now)
	// Make sure the actual/expected are close
	require.InDelta(desiredVal, actualVal, .00001)
	// Make sure TimeUntil returns the zero duration if
	// the value provided >= the current value
	require.Zero(m.TimeUntil(now, actualVal))
	require.Zero(m.TimeUntil(now, actualVal+.1))
}

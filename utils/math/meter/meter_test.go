// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package meter

import (
	"fmt"
	"math"
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
	m := factory.New(halflife)
	require.NotNil(t, m, "should have returned a valid interface")
}

func TimeTravelTest(t *testing.T, factory Factory) {
	m := factory.New(halflife)

	now := time.Date(1, 2, 3, 4, 5, 6, 7, time.UTC)
	m.Inc(now, 1)

	now = now.Add(halflife - 1)
	epsilon := 0.0001
	if uptime := m.Read(now); math.Abs(uptime-.5) > epsilon {
		t.Fatalf("Wrong uptime value. Expected %f got %f", .5, uptime)
	}

	m.Dec(now, 1)

	now = now.Add(-halflife)
	if uptime := m.Read(now); math.Abs(uptime-.5) > epsilon {
		t.Fatalf("Wrong uptime value. Expected %f got %f", .5, uptime)
	}

	m.Inc(now, 1)

	now = now.Add(halflife / 2)
	if uptime := m.Read(now); math.Abs(uptime-.5) > epsilon {
		t.Fatalf("Wrong uptime value. Expected %f got %f", .5, uptime)
	}
}

func StandardUsageTest(t *testing.T, factory Factory) {
	m := factory.New(halflife)

	now := time.Date(1, 2, 3, 4, 5, 6, 7, time.UTC)
	m.Inc(now, 1)

	now = now.Add(halflife - 1)
	epsilon := 0.0001
	if uptime := m.Read(now); math.Abs(uptime-.5) > epsilon {
		t.Fatalf("Wrong uptime value. Expected %f got %f", .5, uptime)
	}

	m.Inc(now, 1)

	if uptime := m.Read(now); math.Abs(uptime-.5) > epsilon {
		t.Fatalf("Wrong uptime value. Expected %f got %f", .5, uptime)
	}

	m.Dec(now, 1)

	if uptime := m.Read(now); math.Abs(uptime-.5) > epsilon {
		t.Fatalf("Wrong uptime value. Expected %f got %f", .5, uptime)
	}

	m.Dec(now, 1)

	if uptime := m.Read(now); math.Abs(uptime-.5) > epsilon {
		t.Fatalf("Wrong uptime value. Expected %f got %f", .5, uptime)
	}

	now = now.Add(halflife)
	if uptime := m.Read(now); math.Abs(uptime-.25) > epsilon {
		t.Fatalf("Wrong uptime value. Expected %f got %f", .25, uptime)
	}

	m.Inc(now, 1)

	now = now.Add(halflife)
	if uptime := m.Read(now); math.Abs(uptime-.625) > epsilon {
		t.Fatalf("Wrong uptime value. Expected %f got %f", .625, uptime)
	}

	now = now.Add(34 * halflife)
	if uptime := m.Read(now); math.Abs(uptime-1) > epsilon {
		t.Fatalf("Wrong uptime value. Expected %d got %f", 1, uptime)
	}

	m.Dec(now, 1)

	now = now.Add(34 * halflife)
	if uptime := m.Read(now); math.Abs(uptime-0) > epsilon {
		t.Fatalf("Wrong uptime value. Expected %d got %f", 0, uptime)
	}

	m.Inc(now, 1)

	now = now.Add(2 * halflife)
	if uptime := m.Read(now); math.Abs(uptime-.75) > epsilon {
		t.Fatalf("Wrong uptime value. Expected %f got %f", .75, uptime)
	}

	// Second start
	m.Inc(now, 1)

	now = now.Add(34 * halflife)
	if uptime := m.Read(now); math.Abs(uptime-2) > epsilon {
		t.Fatalf("Wrong uptime value. Expected %d got %f", 2, uptime)
	}

	// Stop the second CPU
	m.Dec(now, 1)

	now = now.Add(34 * halflife)
	if uptime := m.Read(now); math.Abs(uptime-1) > epsilon {
		t.Fatalf("Wrong uptime value. Expected %d got %f", 1, uptime)
	}
}

func TestTimeUntil(t *testing.T) {
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
	require.InDelta(t, desiredVal, actualVal, .00001)
	// Make sure TimeUntil returns the zero duration if
	// the value provided >= the current value
	require.Zero(t, m.TimeUntil(now, actualVal))
	require.Zero(t, m.TimeUntil(now, actualVal+.1))
}

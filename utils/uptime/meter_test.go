// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package uptime

import (
	"fmt"
	"math"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
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
	assert.NotNil(t, m, "should have returned a valid interface")
}

func TimeTravelTest(t *testing.T, factory Factory) {
	m := factory.New(halflife)

	currentTime := time.Date(1, 2, 3, 4, 5, 6, 7, time.UTC)
	m.Start(currentTime)

	currentTime = currentTime.Add(halflife - 1)
	epsilon := 0.0001
	if uptime := m.Read(currentTime); math.Abs(uptime-.5) > epsilon {
		t.Fatalf("Wrong uptime value. Expected %f got %f", .5, uptime)
	}

	m.Stop(currentTime)

	currentTime = currentTime.Add(-halflife)
	if uptime := m.Read(currentTime); math.Abs(uptime-.5) > epsilon {
		t.Fatalf("Wrong uptime value. Expected %f got %f", .5, uptime)
	}

	m.Start(currentTime)

	currentTime = currentTime.Add(halflife / 2)
	if uptime := m.Read(currentTime); math.Abs(uptime-.5) > epsilon {
		t.Fatalf("Wrong uptime value. Expected %f got %f", .5, uptime)
	}
}

func StandardUsageTest(t *testing.T, factory Factory) {
	m := factory.New(halflife)

	currentTime := time.Date(1, 2, 3, 4, 5, 6, 7, time.UTC)
	m.Start(currentTime)

	currentTime = currentTime.Add(halflife - 1)
	epsilon := 0.0001
	if uptime := m.Read(currentTime); math.Abs(uptime-.5) > epsilon {
		t.Fatalf("Wrong uptime value. Expected %f got %f", .5, uptime)
	}

	m.Start(currentTime)

	if uptime := m.Read(currentTime); math.Abs(uptime-.5) > epsilon {
		t.Fatalf("Wrong uptime value. Expected %f got %f", .5, uptime)
	}

	m.Stop(currentTime)

	if uptime := m.Read(currentTime); math.Abs(uptime-.5) > epsilon {
		t.Fatalf("Wrong uptime value. Expected %f got %f", .5, uptime)
	}

	m.Stop(currentTime)

	if uptime := m.Read(currentTime); math.Abs(uptime-.5) > epsilon {
		t.Fatalf("Wrong uptime value. Expected %f got %f", .5, uptime)
	}

	currentTime = currentTime.Add(halflife)
	if uptime := m.Read(currentTime); math.Abs(uptime-.25) > epsilon {
		t.Fatalf("Wrong uptime value. Expected %f got %f", .25, uptime)
	}

	m.Start(currentTime)

	currentTime = currentTime.Add(halflife)
	if uptime := m.Read(currentTime); math.Abs(uptime-.625) > epsilon {
		t.Fatalf("Wrong uptime value. Expected %f got %f", .625, uptime)
	}

	currentTime = currentTime.Add(34 * halflife)
	if uptime := m.Read(currentTime); math.Abs(uptime-1) > epsilon {
		t.Fatalf("Wrong uptime value. Expected %d got %f", 1, uptime)
	}

	m.Stop(currentTime)

	currentTime = currentTime.Add(34 * halflife)
	if uptime := m.Read(currentTime); math.Abs(uptime-0) > epsilon {
		t.Fatalf("Wrong uptime value. Expected %d got %f", 0, uptime)
	}

	m.Start(currentTime)

	currentTime = currentTime.Add(2 * halflife)
	if uptime := m.Read(currentTime); math.Abs(uptime-.75) > epsilon {
		t.Fatalf("Wrong uptime value. Expected %f got %f", .75, uptime)
	}

	// Second start
	m.Start(currentTime)

	currentTime = currentTime.Add(34 * halflife)
	if uptime := m.Read(currentTime); math.Abs(uptime-2) > epsilon {
		t.Fatalf("Wrong uptime value. Expected %d got %f", 2, uptime)
	}

	// Stop the second CPU
	m.Stop(currentTime)

	currentTime = currentTime.Add(34 * halflife)
	if uptime := m.Read(currentTime); math.Abs(uptime-1) > epsilon {
		t.Fatalf("Wrong uptime value. Expected %d got %f", 1, uptime)
	}
}

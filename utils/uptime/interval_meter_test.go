// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package uptime

import (
	"math"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestNewIntervalMeter(t *testing.T) {
	m := NewIntervalMeter(time.Second)
	assert.NotNil(t, m, "should have returned a valid interface")
}

func TestIntervalMeter(t *testing.T) {
	halflife := time.Second
	m := &intervalMeter{halflife: halflife}

	currentTime := time.Date(1, 2, 3, 4, 5, 6, 7, time.UTC)
	m.clock.Set(currentTime)

	m.lastUpdated = m.clock.Time()
	m.nextHalvening = m.lastUpdated.Add(halflife)

	m.Start()

	currentTime = currentTime.Add(halflife - 1)
	m.clock.Set(currentTime)

	epsilon := 0.0001
	if uptime := m.Read(); math.Abs(uptime-.5) > epsilon {
		t.Fatalf("Wrong uptime value. Expected %f got %f", .5, uptime)
	}

	m.Start()

	if uptime := m.Read(); math.Abs(uptime-.5) > epsilon {
		t.Fatalf("Wrong uptime value. Expected %f got %f", .5, uptime)
	}

	m.Stop()

	if uptime := m.Read(); math.Abs(uptime-.5) > epsilon {
		t.Fatalf("Wrong uptime value. Expected %f got %f", .5, uptime)
	}

	m.Stop()

	if uptime := m.Read(); math.Abs(uptime-.5) > epsilon {
		t.Fatalf("Wrong uptime value. Expected %f got %f", .5, uptime)
	}

	currentTime = currentTime.Add(halflife)
	m.clock.Set(currentTime)

	if uptime := m.Read(); math.Abs(uptime-.25) > epsilon {
		t.Fatalf("Wrong uptime value. Expected %f got %f", .25, uptime)
	}

	m.Start()

	currentTime = currentTime.Add(halflife)
	m.clock.Set(currentTime)

	if uptime := m.Read(); math.Abs(uptime-.625) > epsilon {
		t.Fatalf("Wrong uptime value. Expected %f got %f", .625, uptime)
	}
}

func TestIntervalMeterTimeTravel(t *testing.T) {
	halflife := time.Second
	m := &intervalMeter{halflife: halflife}

	currentTime := time.Date(1, 2, 3, 4, 5, 6, 7, time.UTC)
	m.clock.Set(currentTime)

	m.lastUpdated = m.clock.Time()
	m.nextHalvening = m.lastUpdated.Add(halflife)

	m.Start()

	currentTime = currentTime.Add(halflife - 1)
	m.clock.Set(currentTime)

	epsilon := 0.0001
	if uptime := m.Read(); math.Abs(uptime-.5) > epsilon {
		t.Fatalf("Wrong uptime value. Expected %f got %f", .5, uptime)
	}

	m.Stop()

	currentTime = currentTime.Add(-halflife)
	m.clock.Set(currentTime)

	if uptime := m.Read(); math.Abs(uptime-.5) > epsilon {
		t.Fatalf("Wrong uptime value. Expected %f got %f", .5, uptime)
	}

	m.Start()

	currentTime = currentTime.Add(halflife / 2)
	m.clock.Set(currentTime)

	if uptime := m.Read(); math.Abs(uptime-.5) > epsilon {
		t.Fatalf("Wrong uptime value. Expected %f got %f", .5, uptime)
	}
}

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

	m.lastUpdated = currentTime
	m.nextHalvening = m.lastUpdated.Add(halflife)

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

	currentTime = currentTime.Add((maxSkippedIntervals + 2) * halflife)

	if uptime := m.Read(currentTime); math.Abs(uptime-1) > epsilon {
		t.Fatalf("Wrong uptime value. Expected %d got %f", 1, uptime)
	}

	m.Stop(currentTime)

	currentTime = currentTime.Add((maxSkippedIntervals + 2) * halflife)

	if uptime := m.Read(currentTime); math.Abs(uptime-0) > epsilon {
		t.Fatalf("Wrong uptime value. Expected %d got %f", 0, uptime)
	}
}

func TestIntervalMeterTimeTravel(t *testing.T) {
	halflife := time.Second
	m := &intervalMeter{halflife: halflife}

	currentTime := time.Date(1, 2, 3, 4, 5, 6, 7, time.UTC)

	m.lastUpdated = currentTime
	m.nextHalvening = m.lastUpdated.Add(halflife)

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

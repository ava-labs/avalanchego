// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package uptime

import (
	"testing"
	"time"

	"github.com/ava-labs/gecko/utils/timer"

	"github.com/stretchr/testify/assert"
)

func TestNewMeter(t *testing.T) {
	m := NewMeter(time.Second)
	assert.NotNil(t, m, "should have returned a valid interface")
}

func TestMeter(t *testing.T) {
	halflife := time.Second
	m := &meter{halflife: halflife}
	clock := timer.Clock{}

	currentTime := time.Date(1, 2, 3, 4, 5, 6, 7, time.UTC)
	clock.Set(currentTime)

	m.Start(clock.Time())

	currentTime = currentTime.Add(halflife)
	clock.Set(currentTime)

	if uptime := m.Read(clock.Time()); uptime != .5 {
		t.Fatalf("Wrong uptime value. Expected %f got %f", .5, uptime)
	}

	m.Start(clock.Time())

	if uptime := m.Read(clock.Time()); uptime != .5 {
		t.Fatalf("Wrong uptime value. Expected %f got %f", .5, uptime)
	}

	m.Stop(clock.Time())

	if uptime := m.Read(clock.Time()); uptime != .5 {
		t.Fatalf("Wrong uptime value. Expected %f got %f", .5, uptime)
	}

	m.Stop(clock.Time())

	if uptime := m.Read(clock.Time()); uptime != .5 {
		t.Fatalf("Wrong uptime value. Expected %f got %f", .5, uptime)
	}

	currentTime = currentTime.Add(halflife)
	clock.Set(currentTime)

	if uptime := m.Read(clock.Time()); uptime != .25 {
		t.Fatalf("Wrong uptime value. Expected %f got %f", .25, uptime)
	}

	m.Start(clock.Time())

	currentTime = currentTime.Add(halflife)
	clock.Set(currentTime)

	if uptime := m.Read(clock.Time()); uptime != .625 {
		t.Fatalf("Wrong uptime value. Expected %f got %f", .625, uptime)
	}
}

func TestMeterTimeTravel(t *testing.T) {
	halflife := time.Second
	clock := timer.Clock{}
	m := &meter{
		running: false,
		started: time.Time{},

		halflife: halflife,
		value:    0,
	}

	currentTime := time.Date(1, 2, 3, 4, 5, 6, 7, time.UTC)
	clock.Set(currentTime)

	m.lastUpdated = clock.Time()

	m.Start(clock.Time())

	currentTime = currentTime.Add(halflife)
	clock.Set(currentTime)

	if uptime := m.Read(clock.Time()); uptime != .5 {
		t.Fatalf("Wrong uptime value. Expected %f got %f", .5, uptime)
	}

	m.Stop(clock.Time())

	currentTime = currentTime.Add(-halflife)
	clock.Set(currentTime)

	if uptime := m.Read(clock.Time()); uptime != .5 {
		t.Fatalf("Wrong uptime value. Expected %f got %f", .5, uptime)
	}

	m.Start(clock.Time())

	currentTime = currentTime.Add(halflife / 2)
	clock.Set(currentTime)

	if uptime := m.Read(clock.Time()); uptime != .5 {
		t.Fatalf("Wrong uptime value. Expected %f got %f", .5, uptime)
	}
}

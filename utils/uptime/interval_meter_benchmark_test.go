// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package uptime

import (
	"testing"
	"time"
)

func BenchmarkIntervalMeterSeconds(b *testing.B) {
	m := NewIntervalMeter(time.Second).(*intervalMeter)
	m.Start()

	currentTime := m.clock.Time()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		currentTime = currentTime.Add(10*time.Second + 500*time.Millisecond)
		m.clock.Set(currentTime)
		time.Now()

		m.Read()
	}
}

func BenchmarkIntervalMeterMilliseconds(b *testing.B) {
	m := NewIntervalMeter(time.Second).(*intervalMeter)
	m.Start()

	currentTime := m.clock.Time()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		currentTime = currentTime.Add(10 * time.Millisecond)
		m.clock.Set(currentTime)
		time.Now()

		m.Read()
	}
}

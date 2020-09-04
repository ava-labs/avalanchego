// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package uptime

import (
	"testing"
	"time"
)

func BenchmarkMeterSeconds(b *testing.B) {
	m := NewMeter(time.Second).(*meter)
	m.Start()

	currentTime := m.clock.Time()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		currentTime = currentTime.Add(4 * time.Second)
		m.clock.Set(currentTime)
		time.Now()

		m.Read()
	}
}

func BenchmarkMeterMilliseconds(b *testing.B) {
	m := NewMeter(time.Second).(*meter)
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

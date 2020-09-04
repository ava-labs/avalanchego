// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package uptime

import (
	"testing"
	"time"
)

func BenchmarkMeterSeconds(b *testing.B) {
	m := NewMeter(time.Second).(*meter)
	m.Start(time.Now())

	currentTime := time.Now()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		currentTime = currentTime.Add(10*time.Second + 500*time.Millisecond)
		time.Now()

		m.Read(currentTime)
	}
}

func BenchmarkMeterMilliseconds(b *testing.B) {
	m := NewMeter(time.Second).(*meter)

	currentTime := time.Now()
	m.Start(currentTime)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		currentTime = currentTime.Add(10 * time.Millisecond)
		time.Now()

		m.Read(currentTime)
	}
}

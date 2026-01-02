// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package meter

import (
	"fmt"
	"testing"
	"time"
)

func BenchmarkMeters(b *testing.B) {
	for _, meterDef := range meters {
		period := time.Second + 500*time.Millisecond
		name := fmt.Sprintf("%s-%s", meterDef.name, period)
		b.Run(name, func(b *testing.B) {
			m := meterDef.factory.New(halflife)
			MeterBenchmark(b, m, period)
		})

		period = time.Millisecond
		name = fmt.Sprintf("%s-%s", meterDef.name, period)
		b.Run(name, func(b *testing.B) {
			m := meterDef.factory.New(halflife)
			MeterBenchmark(b, m, period)
		})
	}
}

func MeterBenchmark(b *testing.B, m Meter, period time.Duration) {
	currentTime := time.Now()
	m.Inc(currentTime, 1)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		currentTime = currentTime.Add(period)
		m.Read(currentTime)
	}
}

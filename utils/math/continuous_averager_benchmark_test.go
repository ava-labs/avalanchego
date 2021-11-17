// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package math

import (
	"fmt"
	"testing"
	"time"
)

func BenchmarkAveragers(b *testing.B) {
	periods := []time.Duration{
		time.Millisecond,
		time.Duration(0),
		-time.Millisecond,
	}
	for _, period := range periods {
		name := fmt.Sprintf("period=%s", period)
		b.Run(name, func(b *testing.B) {
			a := NewAverager(0, time.Second, time.Now())
			AveragerBenchmark(b, a, period)
		})
	}
}

func AveragerBenchmark(b *testing.B, a Averager, period time.Duration) {
	currentTime := time.Now()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		currentTime = currentTime.Add(period)
		a.Observe(float64(i), currentTime)
	}
}

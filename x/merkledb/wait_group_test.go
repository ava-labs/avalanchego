// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package merkledb

import "testing"

func Benchmark_WaitGroup_Wait(b *testing.B) {
	for i := 0; i < b.N; i++ {
		var wg waitGroup
		wg.Wait()
	}
}

func Benchmark_WaitGroup_Add(b *testing.B) {
	for i := 0; i < b.N; i++ {
		var wg waitGroup
		wg.Add(1)
	}
}

func Benchmark_WaitGroup_AddDoneWait(b *testing.B) {
	for i := 0; i < b.N; i++ {
		var wg waitGroup
		wg.Add(1)
		wg.wg.Done()
		wg.Wait()
	}
}

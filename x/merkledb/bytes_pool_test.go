// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package merkledb

import "testing"

func Benchmark_BytesPool_Acquire(b *testing.B) {
	s := newBytesPool(b.N)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		s.Acquire()
	}
}

func Benchmark_BytesPool_Release(b *testing.B) {
	s := newBytesPool(b.N)
	for i := 0; i < b.N; i++ {
		s.Acquire()
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		s.Release(nil)
	}
}

func Benchmark_BytesPool_TryAcquire_Success(b *testing.B) {
	s := newBytesPool(b.N)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		s.TryAcquire()
	}
}

func Benchmark_BytesPool_TryAcquire_Failure(b *testing.B) {
	s := newBytesPool(1)
	s.Acquire()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		s.TryAcquire()
	}
}

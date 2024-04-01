// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package merkledb

import "testing"

func Benchmark_Semaphore_Acquire(b *testing.B) {
	s := make(semaphore, b.N)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		s.Acquire()
	}
}

func Benchmark_Semaphore_Release(b *testing.B) {
	s := make(semaphore, b.N)
	for i := 0; i < b.N; i++ {
		s.Acquire()
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		s.Release()
	}
}

func Benchmark_Semaphore_TryAcquire_Success(b *testing.B) {
	s := make(semaphore, b.N)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		s.TryAcquire()
	}
}

func Benchmark_Semaphore_TryAcquire_Failure(b *testing.B) {
	s := make(semaphore, 1)
	s.Acquire()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		s.TryAcquire()
	}
}

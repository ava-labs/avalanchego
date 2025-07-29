// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sampler

import (
	"math"
	"math/rand"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/thepudds/fzgen/fuzzer"
	"gonum.org/v1/gonum/mathext/prng"
)

type testSource struct {
	onInvalid func()
	nums      []uint64
}

func (s *testSource) Seed(uint64) {
	s.onInvalid()
}

func (s *testSource) Uint64() uint64 {
	if len(s.nums) == 0 {
		s.onInvalid()
	}
	num := s.nums[0]
	s.nums = s.nums[1:]
	return num
}

type testSTDSource struct {
	onInvalid func()
	nums      []uint64
}

func (s *testSTDSource) Seed(int64) {
	s.onInvalid()
}

func (s *testSTDSource) Int63() int64 {
	return int64(s.Uint64() & (1<<63 - 1))
}

func (s *testSTDSource) Uint64() uint64 {
	if len(s.nums) == 0 {
		s.onInvalid()
	}
	num := s.nums[0]
	s.nums = s.nums[1:]
	return num
}

func TestRNG(t *testing.T) {
	tests := []struct {
		maximum  uint64
		nums     []uint64
		expected uint64
	}{
		{
			maximum: math.MaxUint64,
			nums: []uint64{
				0x01,
			},
			expected: 0x01,
		},
		{
			maximum: math.MaxUint64,
			nums: []uint64{
				0x0102030405060708,
			},
			expected: 0x0102030405060708,
		},
		{
			maximum: math.MaxUint64,
			nums: []uint64{
				0xF102030405060708,
			},
			expected: 0xF102030405060708,
		},
		{
			maximum: math.MaxInt64,
			nums: []uint64{
				0x01,
			},
			expected: 0x01,
		},
		{
			maximum: math.MaxInt64,
			nums: []uint64{
				0x0102030405060708,
			},
			expected: 0x0102030405060708,
		},
		{
			maximum: math.MaxInt64,
			nums: []uint64{
				0x8102030405060708,
			},
			expected: 0x0102030405060708,
		},
		{
			maximum: 15,
			nums: []uint64{
				0x810203040506071a,
			},
			expected: 0x0a,
		},
		{
			maximum: math.MaxInt64 + 1,
			nums: []uint64{
				math.MaxInt64 + 1,
			},
			expected: math.MaxInt64 + 1,
		},
		{
			maximum: math.MaxInt64 + 1,
			nums: []uint64{
				math.MaxInt64 + 2,
				0,
			},
			expected: 0,
		},
		{
			maximum: math.MaxInt64 + 1,
			nums: []uint64{
				math.MaxInt64 + 2,
				0x0102030405060708,
			},
			expected: 0x0102030405060708,
		},
		{
			maximum: 2,
			nums: []uint64{
				math.MaxInt64 - 2,
			},
			expected: 0x02,
		},
		{
			maximum: 2,
			nums: []uint64{
				math.MaxInt64 - 1,
				0x01,
			},
			expected: 0x01,
		},
	}
	for i, test := range tests {
		t.Run(strconv.Itoa(i), func(t *testing.T) {
			require := require.New(t)

			source := &testSource{
				onInvalid: t.FailNow,
				nums:      test.nums,
			}
			r := &rng{rng: source}
			val := r.Uint64Inclusive(test.maximum)
			require.Equal(test.expected, val)
			require.Empty(source.nums)

			if test.maximum >= math.MaxInt64 {
				return
			}

			stdSource := &testSTDSource{
				onInvalid: t.FailNow,
				nums:      test.nums,
			}
			mathRNG := rand.New(stdSource) //#nosec G404
			stdVal := mathRNG.Int63n(int64(test.maximum + 1))
			require.Equal(test.expected, uint64(stdVal))
			require.Empty(source.nums)
		})
	}
}

func FuzzRNG(f *testing.F) {
	f.Fuzz(func(t *testing.T, data []byte) {
		require := require.New(t)

		var (
			maximum    uint64
			sourceNums []uint64
		)
		fz := fuzzer.NewFuzzer(data)
		fz.Fill(&maximum, &sourceNums)
		if maximum >= math.MaxInt64 {
			t.SkipNow()
		}

		source := &testSource{
			onInvalid: t.SkipNow,
			nums:      sourceNums,
		}
		r := &rng{rng: source}
		val := r.Uint64Inclusive(maximum)

		stdSource := &testSTDSource{
			onInvalid: t.SkipNow,
			nums:      sourceNums,
		}
		mathRNG := rand.New(stdSource) //#nosec G404
		stdVal := mathRNG.Int63n(int64(maximum + 1))
		require.Equal(val, uint64(stdVal))
		require.Len(stdSource.nums, len(source.nums))
	})
}

func BenchmarkSeed32(b *testing.B) {
	source := prng.NewMT19937()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		source.Seed(0)
	}
}

func BenchmarkSeed64(b *testing.B) {
	source := prng.NewMT19937_64()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		source.Seed(0)
	}
}

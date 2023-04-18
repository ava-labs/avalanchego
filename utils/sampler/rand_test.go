// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sampler

import (
	"fmt"
	"math"
	"math/rand"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/thepudds/fzgen/fuzzer"
)

type testRNGSource struct {
	onInvalid func()
	nums      []uint64
}

func (r *testRNGSource) Seed(uint64) {
	r.onInvalid()
}

func (r *testRNGSource) Uint64() uint64 {
	if len(r.nums) == 0 {
		r.onInvalid()
	}
	num := r.nums[0]
	r.nums = r.nums[1:]
	return num
}

type testSTDRNGSource struct {
	onInvalid func()
	nums      []uint64
}

func (r *testSTDRNGSource) Seed(int64) {
	r.onInvalid()
}

func (r *testSTDRNGSource) Int63() int64 {
	return int64(r.Uint64() & (1<<63 - 1))
}

func (r *testSTDRNGSource) Uint64() uint64 {
	if len(r.nums) == 0 {
		r.onInvalid()
	}
	num := r.nums[0]
	r.nums = r.nums[1:]
	return num
}

func TestRNG(t *testing.T) {
	tests := []struct {
		max      uint64
		nums     []uint64
		expected uint64
	}{
		{
			max: math.MaxUint64,
			nums: []uint64{
				0x01,
			},
			expected: 0x01,
		},
		{
			max: math.MaxUint64,
			nums: []uint64{
				0x0102030405060708,
			},
			expected: 0x0102030405060708,
		},
		{
			max: math.MaxUint64,
			nums: []uint64{
				0xF102030405060708,
			},
			expected: 0xF102030405060708,
		},
		{
			max: math.MaxInt64,
			nums: []uint64{
				0x01,
			},
			expected: 0x01,
		},
		{
			max: math.MaxInt64,
			nums: []uint64{
				0x0102030405060708,
			},
			expected: 0x0102030405060708,
		},
		{
			max: math.MaxInt64,
			nums: []uint64{
				0x8102030405060708,
			},
			expected: 0x0102030405060708,
		},
		{
			max: 15,
			nums: []uint64{
				0x810203040506071a,
			},
			expected: 0x0a,
		},
		{
			max: math.MaxInt64 + 1,
			nums: []uint64{
				math.MaxInt64 + 1,
			},
			expected: math.MaxInt64 + 1,
		},
		{
			max: math.MaxInt64 + 1,
			nums: []uint64{
				math.MaxInt64 + 2,
				0,
			},
			expected: 0,
		},
		{
			max: math.MaxInt64 + 1,
			nums: []uint64{
				math.MaxInt64 + 2,
				0x0102030405060708,
			},
			expected: 0x0102030405060708,
		},
		{
			max: 2,
			nums: []uint64{
				math.MaxInt64 - 2,
			},
			expected: 0x02,
		},
		{
			max: 2,
			nums: []uint64{
				math.MaxInt64 - 1,
				0x01,
			},
			expected: 0x01,
		},
	}
	for i, test := range tests {
		t.Run(fmt.Sprintf("%d", i), func(t *testing.T) {
			require := require.New(t)

			source := &testRNGSource{
				onInvalid: t.FailNow,
				nums:      test.nums,
			}
			r := &rng{rng: source}
			val := r.Uint64n(test.max)
			require.Equal(test.expected, val)
			require.Empty(source.nums)

			if test.max >= math.MaxInt64 {
				return
			}

			stdSource := &testSTDRNGSource{
				onInvalid: t.FailNow,
				nums:      test.nums,
			}
			mathRNG := rand.New(stdSource) //#nosec G404
			stdVal := mathRNG.Int63n(int64(test.max + 1))
			require.Equal(test.expected, uint64(stdVal))
			require.Empty(source.nums)
		})
	}
}

func FuzzRNG(f *testing.F) {
	f.Fuzz(func(t *testing.T, data []byte) {
		require := require.New(t)

		var (
			max       uint64
			rngSource []uint64
		)
		fz := fuzzer.NewFuzzer(data)
		fz.Fill(&max, &rngSource)
		if max >= math.MaxInt64 {
			t.SkipNow()
		}

		source := &testRNGSource{
			onInvalid: t.SkipNow,
			nums:      rngSource,
		}
		r := &rng{rng: source}
		val := r.Uint64n(max)

		stdSource := &testSTDRNGSource{
			onInvalid: t.SkipNow,
			nums:      rngSource,
		}
		mathRNG := rand.New(stdSource) //#nosec G404
		stdVal := mathRNG.Int63n(int64(max + 1))
		require.Equal(val, uint64(stdVal))
		require.Equal(len(source.nums), len(stdSource.nums))
	})
}

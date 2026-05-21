// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package utils

import (
	"math"
	"math/rand"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"
)

// Verify that [intSize] is correctly set based on the word size. This test uses
// [math.MaxInt] to detect the word size.
func TestIntSize(t *testing.T) {
	require := require.New(t)

	require.Contains([]int{32, 64}, intSize)
	if intSize == 32 {
		require.Equal(math.MaxInt32, math.MaxInt)
	} else {
		require.Equal(math.MaxInt64, math.MaxInt)
	}
}

func TestBytesPool(t *testing.T) {
	require := require.New(t)

	p := NewBytesPool()
	for i := 0; i < 128; i++ {
		bytes := p.Get(i)
		require.NotNil(bytes)
		require.Len(*bytes, i)
		p.Put(bytes)
	}
}

func BenchmarkBytesPool_Constant(b *testing.B) {
	sizes := []int{
		0,
		8,
		16,
		32,
		64,
		256,
		2048,
	}
	for _, size := range sizes {
		b.Run(strconv.Itoa(size), func(b *testing.B) {
			p := NewBytesPool()
			for i := 0; i < b.N; i++ {
				p.Put(p.Get(size))
			}
		})
	}
}

func BenchmarkBytesPool_Descending(b *testing.B) {
	p := NewBytesPool()
	for i := 0; i < b.N; i++ {
		for size := 100_000; size > 0; size-- {
			p.Put(p.Get(size))
		}
	}
}

func BenchmarkBytesPool_Ascending(b *testing.B) {
	p := NewBytesPool()
	for i := 0; i < b.N; i++ {
		for size := 0; size < 100_000; size++ {
			p.Put(p.Get(size))
		}
	}
}

func BenchmarkBytesPool_Random(b *testing.B) {
	p := NewBytesPool()
	sizes := make([]int, 1_000)
	for i := range sizes {
		sizes[i] = rand.Intn(100_000) //#nosec G404
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, size := range sizes {
			p.Put(p.Get(size))
		}
	}
}

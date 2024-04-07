// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package utils

import (
	"math"
	"math/rand"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"
)

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

func TestBytesPoolMaxInt(t *testing.T) {
	require := require.New(t)

	p := NewBytesPool()
	bytes := p.Get(math.MaxInt32)
	require.NotNil(bytes)
	require.Len(*bytes, math.MaxInt32)
	p.Put(bytes)
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

func BenchmarkBytesPool_Decending(b *testing.B) {
	p := NewBytesPool()
	for i := b.N; i > 0; i-- {
		p.Put(p.Get(i))
	}
}

func BenchmarkBytesPool_Ascending(b *testing.B) {
	p := NewBytesPool()
	for i := 0; i < b.N; i++ {
		p.Put(p.Get(i))
	}
}

func BenchmarkBytesPool_Random(b *testing.B) {
	p := NewBytesPool()
	for i := 0; i < b.N; i++ {
		p.Put(p.Get(rand.Intn(100_000))) //#nosec G404
	}
}

// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package utils

import (
	"math"
	"math/rand"
	"strconv"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBytesPool(t *testing.T) {
	require := require.New(t)

	p := NewBytesPool()
	for i := 0; i < 128; i++ {
		bytes := p.Get(i)
		require.Len(*bytes, i)
		p.Put(bytes)
	}
}

func TestBytesPoolMaxInt(t *testing.T) {
	require := require.New(t)

	p := NewBytesPool()
	bytes := p.Get(math.MaxInt32)
	require.Len(*bytes, math.MaxInt32)
	p.Put(bytes)
}

func getBufferFromPool(bufferPool *sync.Pool, size int) *[]byte {
	buffer := bufferPool.Get().(*[]byte)
	if cap(*buffer) >= size {
		// The [] byte we got from the pool is big enough to hold the prefixed key
		*buffer = (*buffer)[:size]
	} else {
		// The []byte from the pool wasn't big enough.
		// Put it back and allocate a new, bigger one
		bufferPool.Put(buffer)
		b := make([]byte, size)
		buffer = &b
	}
	return buffer
}

func BenchmarkOldBytesPool_Constant(b *testing.B) {
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
			p := &sync.Pool{
				New: func() interface{} {
					b := make([]byte, 0, 256)
					return &b
				},
			}
			for i := 0; i < b.N; i++ {
				p.Put(getBufferFromPool(p, size))
			}
		})
	}
}

func BenchmarkOldBytesPool_Decending(b *testing.B) {
	p := &sync.Pool{
		New: func() interface{} {
			b := make([]byte, 0, 256)
			return &b
		},
	}
	for i := b.N; i > 0; i-- {
		p.Put(getBufferFromPool(p, i))
	}
}

func BenchmarkOldBytesPool_Ascending(b *testing.B) {
	p := &sync.Pool{
		New: func() interface{} {
			b := make([]byte, 0, 256)
			return &b
		},
	}
	for i := 0; i < b.N; i++ {
		p.Put(getBufferFromPool(p, i))
	}
}

func BenchmarkOldBytesPool_Random(b *testing.B) {
	p := &sync.Pool{
		New: func() interface{} {
			b := make([]byte, 0, 256)
			return &b
		},
	}
	for i := 0; i < b.N; i++ {
		p.Put(getBufferFromPool(p, rand.Intn(100_000))) //#nosec G404
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

func BenchmarkAllocBytes_Constant(b *testing.B) {
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
			for i := 0; i < b.N; i++ {
				_ = make([]byte, size)
			}
		})
	}
}

func BenchmarkAllocBytes_Decending(b *testing.B) {
	for i := b.N; i > 0; i-- {
		_ = make([]byte, i)
	}
}

func BenchmarkAllocBytes_Ascending(b *testing.B) {
	for i := 0; i < b.N; i++ {
		_ = make([]byte, i)
	}
}

func BenchmarkAllocBytes_Random(b *testing.B) {
	for i := 0; i < b.N; i++ {
		_ = make([]byte, rand.Intn(100_000)) //#nosec G404
	}
}

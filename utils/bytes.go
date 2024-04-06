// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package utils

import (
	"crypto/rand"
	"math/bits"
	"sync"
)

// RandomBytes returns a slice of n random bytes
// Intended for use in testing
func RandomBytes(n int) []byte {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return b
}

// Constant taken from the "math" package
const intSize = 32 << (^uint(0) >> 63) // 32 or 64

// BytesPool tracks buckets of available buffers to be allocated. Each bucket
// allocates buffers of the following length:
//
// 1
// 3
// 7
// 15
// 31
// 63
// 127
// ...
// MaxInt
//
// In order to allocate a buffer of length 19 (for example), we calculate the
// number of bits required to represent 19 (5). And therefore allocate a slice
// from bucket 4, which has length 31. This is the bucket which produces the
// smallest slices that are at least length 19.
//
// When replacing a buffer of length 19, we calculate the number of bits
// required to represent 20 (5). And therefore place the slice into bucket 3,
// which has length 15. This is the bucket which produces the largest slices
// that are at most length 19.
type BytesPool [intSize - 1]*sync.Pool

func NewBytesPool() *BytesPool {
	var p BytesPool
	for i := range p {
		size := uint64(1)<<(i+1) - 1
		p[i] = &sync.Pool{
			New: func() interface{} {
				return make([]byte, size)
			},
		}
	}
	return &p
}

func (p *BytesPool) Get(length int) []byte {
	if length <= 0 {
		return nil
	}

	index := bits.Len(uint(length)) - 1 // Round up
	bytes := p[index].Get().([]byte)
	return bytes[:length]
}

func (p *BytesPool) Put(bytes []byte) {
	size := cap(bytes)
	if size == 0 {
		return
	}

	index := bits.Len(uint(size+1)) - 2 // Round down
	p[index].Put(bytes)
}

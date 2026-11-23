// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package avalanchego

import (
	"math/big"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestBitmask(t *testing.T) {
	t.Run("empty bitmask", func(t *testing.T) {
		bm := BitmaskFromBytes(nil)
		require.Equal(t, 0, bm.Len())
		require.False(t, bm.Contains(0))
		require.False(t, bm.Contains(5))
	})

	t.Run("from bytes and Contains", func(t *testing.T) {
		// 0b00000111 = 7 → bits 0, 1, 2 are set
		bm := BitmaskFromBytes([]byte{7})
		require.True(t, bm.Contains(0))
		require.True(t, bm.Contains(1))
		require.True(t, bm.Contains(2))
		require.False(t, bm.Contains(3))
		require.Equal(t, 3, bm.Len())
	})

	t.Run("Add", func(t *testing.T) {
		bm := BitmaskFromBytes([]byte{1}) // bit 0
		require.True(t, bm.Contains(0))
		require.False(t, bm.Contains(3))

		bm.Add(3)
		require.True(t, bm.Contains(3))
		require.Equal(t, 2, bm.Len())
	})

	t.Run("Bytes round-trip", func(t *testing.T) {
		bm := BitmaskFromBytes([]byte{0xAB})
		bm2 := BitmaskFromBytes(bm.Bytes())
		require.Equal(t, bm.Len(), bm2.Len())
		for i := 0; i < 8; i++ {
			require.Equal(t, bm.Contains(i), bm2.Contains(i))
		}
	})

	t.Run("BitLen", func(t *testing.T) {
		empty := BitmaskFromBytes(nil)
		require.Equal(t, 0, empty.BitLen())

		// 0b00000111 → highest set bit is 2, so 3 bits are needed to represent it.
		low := BitmaskFromBytes([]byte{7})
		require.Equal(t, 3, low.BitLen())

		high := BitmaskFromBytes(nil)
		high.Add(63)
		require.Equal(t, 64, high.BitLen())
	})

	// A negative value has no meaning as a bitmask. Nothing in this package can produce
	// one, but Len reports 0 rather than counting the bits of the magnitude, which keeps
	// it consistent with how it has always behaved.
	t.Run("Len reports zero for a negative value", func(t *testing.T) {
		for _, v := range []int64{-1, -7, -255, -1 << 40} {
			var bm Bitmask
			(*big.Int)(&bm).SetInt64(v)
			require.Equal(t, 0, bm.Len())
		}
	})

	// Len counts bits over the byte representation, which strips leading zero bytes and
	// is unaffected by how wide the input buffer was.
	t.Run("Len is independent of leading zero bytes", func(t *testing.T) {
		bare := BitmaskFromBytes([]byte{7})
		padded := BitmaskFromBytes([]byte{0, 0, 0, 7})
		require.Equal(t, 3, bare.Len())
		require.Equal(t, 3, padded.Len())

		zeros := BitmaskFromBytes([]byte{0, 0, 0, 0})
		require.Equal(t, 0, zeros.Len())
	})

	t.Run("Len counts every bit across many bytes", func(t *testing.T) {
		for n := 1; n <= 32; n++ {
			buff := make([]byte, n)
			for i := range buff {
				buff[i] = 0xFF
			}
			bm := BitmaskFromBytes(buff)
			require.Equal(t, n*8, bm.Len())
			require.Equal(t, n*8, bm.BitLen())
		}
	})

	// A wide bitmask costs a proposer almost nothing to produce, so Len has to stay
	// linear in its size. Counting bits by shifting a big.Int down one position at a
	// time is quadratic in the position of the highest set bit, and took minutes here.
	t.Run("Len stays linear over a wide bitmask", func(t *testing.T) {
		buff := make([]byte, 1<<20) // 1 MiB, ~8.4 million bits
		buff[0] = 0xFF              // set the highest bits, so the full width is walked
		bm := BitmaskFromBytes(buff)

		start := time.Now()
		require.Equal(t, 8, bm.Len())
		require.Less(t, time.Since(start), 5*time.Second)
	})

	t.Run("Difference", func(t *testing.T) {
		// bm1 = bits 0,1,2 (0b111 = 7)
		// bm2 = bits 0,1   (0b011 = 3)
		// bm1.Difference(bm2) should leave only bit 2
		bm1 := BitmaskFromBytes([]byte{7})
		bm2 := BitmaskFromBytes([]byte{3})
		bm1.Difference(&bm2)
		require.False(t, bm1.Contains(0))
		require.False(t, bm1.Contains(1))
		require.True(t, bm1.Contains(2))
		require.Equal(t, 1, bm1.Len())
	})

	t.Run("Len with multiple bytes", func(t *testing.T) {
		// 0xFF = 8 bits set, 0x01 = 1 bit set → 9 total
		bm := BitmaskFromBytes([]byte{0x01, 0xFF})
		require.Equal(t, 9, bm.Len())
	})

	t.Run("Clone produces independent copy", func(t *testing.T) {
		bm := BitmaskFromBytes([]byte{7}) // bits 0,1,2
		cloned := bm.Clone()

		// Clone matches original
		require.Equal(t, bm.Len(), cloned.Len())
		for i := 0; i < 3; i++ {
			require.Equal(t, bm.Contains(i), cloned.Contains(i))
		}

		// Mutating clone does not affect original
		cloned.Add(5)
		require.True(t, cloned.Contains(5))
		require.False(t, bm.Contains(5))

		// Mutating original does not affect clone
		bm.Add(7)
		require.True(t, bm.Contains(7))
		require.False(t, cloned.Contains(7))
	})
}

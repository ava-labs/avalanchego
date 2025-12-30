// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package ids

import (
	"fmt"
	"math"
	"math/rand"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func flip(b uint8) uint8 {
	b = b>>4 | b<<4
	b = (b&0xCC)>>2 | (b&0x33)<<2
	b = (b&0xAA)>>1 | (b&0x55)<<1
	return b
}

func BitString(id ID) string {
	sb := strings.Builder{}
	for _, b := range id {
		sb.WriteString(fmt.Sprintf("%08b", flip(b)))
	}
	return sb.String()
}

func Check(start, stop int, id1, id2 ID) bool {
	s1 := BitString(id1)
	s2 := BitString(id2)

	shorts1 := s1[start:stop]
	shorts2 := s2[start:stop]

	return shorts1 == shorts2
}

func TestEqualSubsetEarlyStop(t *testing.T) {
	require := require.New(t)

	id1 := ID{0xf0, 0x0f}
	id2 := ID{0xf0, 0x1f}

	require.True(EqualSubset(0, 12, id1, id2))
	require.False(EqualSubset(0, 13, id1, id2))
}

func TestEqualSubsetLateStart(t *testing.T) {
	id1 := ID{0x1f, 0xf8}
	id2 := ID{0x10, 0x08}

	require.True(t, EqualSubset(4, 12, id1, id2))
}

func TestEqualSubsetSameByte(t *testing.T) {
	id1 := ID{0x18}
	id2 := ID{0xfc}

	require.True(t, EqualSubset(3, 5, id1, id2))
}

func TestEqualSubsetBadMiddle(t *testing.T) {
	id1 := ID{0x18, 0xe8, 0x55}
	id2 := ID{0x18, 0x8e, 0x55}

	require.False(t, EqualSubset(0, 8*3, id1, id2))
}

func TestEqualSubsetAll3Bytes(t *testing.T) {
	seed := rand.Uint64() //#nosec G404
	t.Logf("seed: %d", seed)
	id1 := ID{}.Prefix(seed)

	for i := 0; i < BitsPerByte; i++ {
		for j := i; j < BitsPerByte; j++ {
			for k := j; k < BitsPerByte; k++ {
				id2 := ID{uint8(i), uint8(j), uint8(k)}

				for start := 0; start < BitsPerByte*3; start++ {
					for end := start; end <= BitsPerByte*3; end++ {
						require.Equal(t, Check(start, end, id1, id2), EqualSubset(start, end, id1, id2))
					}
				}
			}
		}
	}
}

func TestEqualSubsetOutOfBounds(t *testing.T) {
	id1 := ID{0x18, 0xe8, 0x55}
	id2 := ID{0x18, 0x8e, 0x55}

	require.False(t, EqualSubset(0, math.MaxInt32, id1, id2))
}

func TestFirstDifferenceSubsetEarlyStop(t *testing.T) {
	require := require.New(t)

	id1 := ID{0xf0, 0x0f}
	id2 := ID{0xf0, 0x1f}

	_, found := FirstDifferenceSubset(0, 12, id1, id2)
	require.False(found)

	index, found := FirstDifferenceSubset(0, 13, id1, id2)
	require.True(found)
	require.Equal(12, index)
}

func TestFirstDifferenceEqualByte4(t *testing.T) {
	require := require.New(t)

	id1 := ID{0x10}
	id2 := ID{0x00}

	_, found := FirstDifferenceSubset(0, 4, id1, id2)
	require.False(found)

	index, found := FirstDifferenceSubset(0, 5, id1, id2)
	require.True(found)
	require.Equal(4, index)
}

func TestFirstDifferenceEqualByte5(t *testing.T) {
	require := require.New(t)

	id1 := ID{0x20}
	id2 := ID{0x00}

	_, found := FirstDifferenceSubset(0, 5, id1, id2)
	require.False(found)

	index, found := FirstDifferenceSubset(0, 6, id1, id2)
	require.True(found)
	require.Equal(5, index)
}

func TestFirstDifferenceSubsetMiddle(t *testing.T) {
	require := require.New(t)

	id1 := ID{0xf0, 0x0f, 0x11}
	id2 := ID{0xf0, 0x1f, 0xff}

	index, found := FirstDifferenceSubset(0, 24, id1, id2)
	require.True(found)
	require.Equal(12, index)
}

func TestFirstDifferenceStartMiddle(t *testing.T) {
	require := require.New(t)

	id1 := ID{0x1f, 0x0f, 0x11}
	id2 := ID{0x0f, 0x1f, 0xff}

	index, found := FirstDifferenceSubset(0, 24, id1, id2)
	require.True(found)
	require.Equal(4, index)
}

func TestFirstDifferenceVacuous(t *testing.T) {
	id1 := ID{0xf0, 0x0f, 0x11}
	id2 := ID{0xf0, 0x1f, 0xff}

	_, found := FirstDifferenceSubset(0, 0, id1, id2)
	require.False(t, found)
}

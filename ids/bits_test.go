// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package ids

import (
	"fmt"
	"math"
	"math/rand"
	"strings"
	"testing"
	"time"
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
	id1 := ID{0xf0, 0x0f}
	id2 := ID{0xf0, 0x1f}

	if !EqualSubset(0, 12, id1, id2) {
		t.Fatalf("Should have passed: %08b %08b == %08b %08b", id1[0], id1[1], id2[0], id2[1])
	} else if EqualSubset(0, 13, id1, id2) {
		t.Fatalf("Should not have passed: %08b %08b == %08b %08b", id1[0], id1[1], id2[0], id2[1])
	}
}

func TestEqualSubsetLateStart(t *testing.T) {
	id1 := ID{0x1f, 0xf8}
	id2 := ID{0x10, 0x08}

	if !EqualSubset(4, 12, id1, id2) {
		t.Fatalf("Should have passed: %08b %08b == %08b %08b", id1[0], id1[1], id2[0], id2[1])
	}
}

func TestEqualSubsetSameByte(t *testing.T) {
	id1 := ID{0x18}
	id2 := ID{0xfc}

	if !EqualSubset(3, 5, id1, id2) {
		t.Fatalf("Should have passed: %08b == %08b", id1[0], id2[0])
	}
}

func TestEqualSubsetBadMiddle(t *testing.T) {
	id1 := ID{0x18, 0xe8, 0x55}
	id2 := ID{0x18, 0x8e, 0x55}

	if EqualSubset(0, 8*3, id1, id2) {
		t.Fatalf("Should not have passed: %08b == %08b", id1[1], id2[1])
	}
}

func TestEqualSubsetAll3Bytes(t *testing.T) {
	rand.Seed(time.Now().UnixNano())
	seed := uint64(rand.Int63()) // #nosec G404
	id1 := ID{}.Prefix(seed)

	for i := 0; i < BitsPerByte; i++ {
		for j := i; j < BitsPerByte; j++ {
			for k := j; k < BitsPerByte; k++ {
				id2 := ID{uint8(i), uint8(j), uint8(k)}

				for start := 0; start < BitsPerByte*3; start++ {
					for end := start; end <= BitsPerByte*3; end++ {
						if EqualSubset(start, end, id1, id2) != Check(start, end, id1, id2) {
							t.Fatalf("Subset failed on seed %d:\ns = %d\ne = %d\n%08b %08b %08b == %08b %08b %08b",
								seed, start, end,
								id1[0], id1[1], id1[2],
								id2[0], id2[1], id2[2])
						}
					}
				}
			}
		}
	}
}

func TestEqualSubsetOutOfBounds(t *testing.T) {
	id1 := ID{0x18, 0xe8, 0x55}
	id2 := ID{0x18, 0x8e, 0x55}

	if EqualSubset(0, math.MaxInt32, id1, id2) {
		t.Fatalf("Should not have passed")
	}
}

func TestFirstDifferenceSubsetEarlyStop(t *testing.T) {
	id1 := ID{0xf0, 0x0f}
	id2 := ID{0xf0, 0x1f}

	if _, found := FirstDifferenceSubset(0, 12, id1, id2); found {
		t.Fatalf("Shouldn't have found a difference: %08b %08b == %08b %08b", id1[0], id1[1], id2[0], id2[1])
	} else if index, found := FirstDifferenceSubset(0, 13, id1, id2); !found {
		t.Fatalf("Should have found a difference: %08b %08b == %08b %08b", id1[0], id1[1], id2[0], id2[1])
	} else if index != 12 {
		t.Fatalf("Found a difference at index %d expected %d: %08b %08b == %08b %08b", index, 12, id1[0], id1[1], id2[0], id2[1])
	}
}

func TestFirstDifferenceEqualByte4(t *testing.T) {
	id1 := ID{0x10}
	id2 := ID{0x00}

	if _, found := FirstDifferenceSubset(0, 4, id1, id2); found {
		t.Fatalf("Shouldn't have found a difference: %08b == %08b", id1[0], id2[0])
	} else if index, found := FirstDifferenceSubset(0, 5, id1, id2); !found {
		t.Fatalf("Should have found a difference: %08b == %08b", id1[0], id2[0])
	} else if index != 4 {
		t.Fatalf("Found a difference at index %d expected %d: %08b == %08b", index, 4, id1[0], id2[0])
	}
}

func TestFirstDifferenceEqualByte5(t *testing.T) {
	id1 := ID{0x20}
	id2 := ID{0x00}

	if _, found := FirstDifferenceSubset(0, 5, id1, id2); found {
		t.Fatalf("Shouldn't have found a difference: %08b == %08b", id1[0], id2[0])
	} else if index, found := FirstDifferenceSubset(0, 6, id1, id2); !found {
		t.Fatalf("Should have found a difference: %08b == %08b", id1[0], id2[0])
	} else if index != 5 {
		t.Fatalf("Found a difference at index %d expected %d: %08b == %08b", index, 5, id1[0], id2[0])
	}
}

func TestFirstDifferenceSubsetMiddle(t *testing.T) {
	id1 := ID{0xf0, 0x0f, 0x11}
	id2 := ID{0xf0, 0x1f, 0xff}

	if index, found := FirstDifferenceSubset(0, 24, id1, id2); !found {
		t.Fatalf("Should have found a difference: %08b %08b %08b == %08b %08b %08b", id1[0], id1[1], id1[2], id2[0], id2[1], id2[2])
	} else if index != 12 {
		t.Fatalf("Found a difference at index %d expected %d: %08b %08b %08b == %08b %08b %08b", index, 12, id1[0], id1[1], id1[2], id2[0], id2[1], id2[2])
	}
}

func TestFirstDifferenceStartMiddle(t *testing.T) {
	id1 := ID{0x1f, 0x0f, 0x11}
	id2 := ID{0x0f, 0x1f, 0xff}

	if index, found := FirstDifferenceSubset(0, 24, id1, id2); !found {
		t.Fatalf("Should have found a difference: %08b %08b %08b == %08b %08b %08b", id1[0], id1[1], id1[2], id2[0], id2[1], id2[2])
	} else if index != 4 {
		t.Fatalf("Found a difference at index %d expected %d: %08b %08b %08b == %08b %08b %08b", index, 4, id1[0], id1[1], id1[2], id2[0], id2[1], id2[2])
	}
}

func TestFirstDifferenceVacuous(t *testing.T) {
	id1 := ID{0xf0, 0x0f, 0x11}
	id2 := ID{0xf0, 0x1f, 0xff}

	if _, found := FirstDifferenceSubset(0, 0, id1, id2); found {
		t.Fatalf("Shouldn't have found a difference")
	}
}

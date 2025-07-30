// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package merkledb

import (
	"fmt"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBranchFactor_Valid(t *testing.T) {
	require := require.New(t)
	for _, bf := range validBranchFactors {
		require.NoError(bf.Valid())
	}
	var empty BranchFactor
	err := empty.Valid()
	require.ErrorIs(err, ErrInvalidBranchFactor)
}

func TestHasPartialByte(t *testing.T) {
	for _, ts := range validTokenSizes {
		t.Run(strconv.Itoa(ts), func(t *testing.T) {
			require := require.New(t)

			key := Key{}
			require.False(key.hasPartialByte())

			if ts == 8 {
				// Tokens are an entire byte so
				// there is never a partial byte.
				key = key.Extend(ToToken(1, ts))
				require.False(key.hasPartialByte())
				key = key.Extend(ToToken(0, ts))
				require.False(key.hasPartialByte())
				return
			}

			// Fill all but the last token of the first byte.
			for i := 0; i < 8-ts; i += ts {
				key = key.Extend(ToToken(1, ts))
				require.True(key.hasPartialByte())
			}

			// Fill the last token of the first byte.
			key = key.Extend(ToToken(0, ts))
			require.False(key.hasPartialByte())

			// Fill the first token of the second byte.
			key = key.Extend(ToToken(0, ts))
			require.True(key.hasPartialByte())
		})
	}
}

func Test_Key_Has_Prefix(t *testing.T) {
	type test struct {
		name           string
		keyA           func(ts int) Key
		keyB           func(ts int) Key
		isStrictPrefix bool
		isPrefix       bool
	}

	key := "Key"

	tests := []test{
		{
			name:           "equal keys",
			keyA:           func(int) Key { return ToKey([]byte(key)) },
			keyB:           func(int) Key { return ToKey([]byte(key)) },
			isPrefix:       true,
			isStrictPrefix: false,
		},
		{
			name: "one key has one fewer token",
			keyA: func(int) Key { return ToKey([]byte(key)) },
			keyB: func(ts int) Key {
				return ToKey([]byte(key)).Take(len(key)*8 - ts)
			},
			isPrefix:       true,
			isStrictPrefix: true,
		},
		{
			name: "equal keys, both have one fewer token",
			keyA: func(ts int) Key {
				return ToKey([]byte(key)).Take(len(key)*8 - ts)
			},
			keyB: func(ts int) Key {
				return ToKey([]byte(key)).Take(len(key)*8 - ts)
			},
			isPrefix:       true,
			isStrictPrefix: false,
		},
		{
			name:           "different keys",
			keyA:           func(int) Key { return ToKey([]byte{0xF7}) },
			keyB:           func(int) Key { return ToKey([]byte{0xF0}) },
			isPrefix:       false,
			isStrictPrefix: false,
		},
		{
			name: "same bytes, different lengths",
			keyA: func(ts int) Key {
				return ToKey([]byte{0x10, 0x00}).Take(ts)
			},
			keyB: func(ts int) Key {
				return ToKey([]byte{0x10, 0x00}).Take(ts * 2)
			},
			isPrefix:       false,
			isStrictPrefix: false,
		},
	}

	for _, tt := range tests {
		for _, ts := range validTokenSizes {
			t.Run(tt.name+" ts "+strconv.Itoa(ts), func(t *testing.T) {
				require := require.New(t)
				keyA := tt.keyA(ts)
				keyB := tt.keyB(ts)

				require.Equal(tt.isPrefix, keyA.HasPrefix(keyB))
				require.Equal(tt.isPrefix, keyA.iteratedHasPrefix(keyB, 0, ts))
				require.Equal(tt.isStrictPrefix, keyA.HasStrictPrefix(keyB))
			})
		}
	}
}

func Test_Key_Skip(t *testing.T) {
	require := require.New(t)

	empty := Key{}
	require.Equal(ToKey([]byte{0}).Skip(8), empty)
	for _, ts := range validTokenSizes {
		if ts == 8 {
			continue
		}
		shortKey := ToKey([]byte{0b0101_0101})
		longKey := ToKey([]byte{0b0101_0101, 0b0101_0101})
		for shift := 0; shift < 8; shift += ts {
			skipKey := shortKey.Skip(shift)
			require.Equal(byte(0b0101_0101<<shift), skipKey.value[0])

			skipKey = longKey.Skip(shift)
			require.Equal(byte(0b0101_0101<<shift+0b0101_0101>>(8-shift)), skipKey.value[0])
			require.Equal(byte(0b0101_0101<<shift), skipKey.value[1])
		}
	}

	skip := ToKey([]byte{0b0101_0101, 0b1010_1010}).Skip(8)
	require.Len(skip.value, 1)
	require.Equal(byte(0b1010_1010), skip.value[0])

	skip = ToKey([]byte{0b0101_0101, 0b1010_1010, 0b0101_0101}).Skip(8)
	require.Len(skip.value, 2)
	require.Equal(byte(0b1010_1010), skip.value[0])
	require.Equal(byte(0b0101_0101), skip.value[1])
}

func Test_Key_Take(t *testing.T) {
	require := require.New(t)

	require.Equal(Key{}, ToKey([]byte{0}).Take(0))

	for _, ts := range validTokenSizes {
		if ts == 8 {
			continue
		}
		key := ToKey([]byte{0b0101_0101})
		for length := ts; length <= 8; length += ts {
			take := key.Take(length)
			require.Equal(length, take.length)
			shift := 8 - length
			require.Equal(byte((0b0101_0101>>shift)<<shift), take.value[0])
		}
	}

	take := ToKey([]byte{0b0101_0101, 0b1010_1010}).Take(8)
	require.Len(take.value, 1)
	require.Equal(byte(0b0101_0101), take.value[0])
}

func Test_Key_Token(t *testing.T) {
	type test struct {
		name         string
		inputBytes   []byte
		assertTokens func(*require.Assertions, Key)
	}

	tests := []test{
		{
			name:       "branch factor 2",
			inputBytes: []byte{0b0_1_0_1_0_1_0_1, 0b1_0_1_0_1_0_1_0},
			assertTokens: func(require *require.Assertions, key Key) {
				require.Equal(byte(0), key.Token(0, 1))
				require.Equal(byte(1), key.Token(1, 1))
				require.Equal(byte(0), key.Token(2, 1))
				require.Equal(byte(1), key.Token(3, 1))
				require.Equal(byte(0), key.Token(4, 1))
				require.Equal(byte(1), key.Token(5, 1))
				require.Equal(byte(0), key.Token(6, 1))
				require.Equal(byte(1), key.Token(7, 1)) // end first byte
				require.Equal(byte(1), key.Token(8, 1))
				require.Equal(byte(0), key.Token(9, 1))
				require.Equal(byte(1), key.Token(10, 1))
				require.Equal(byte(0), key.Token(11, 1))
				require.Equal(byte(1), key.Token(12, 1))
				require.Equal(byte(0), key.Token(13, 1))
				require.Equal(byte(1), key.Token(14, 1))
				require.Equal(byte(0), key.Token(15, 1)) // end second byte
			},
		},
		{
			name:       "branch factor 4",
			inputBytes: []byte{0b00_01_10_11, 0b11_10_01_00},
			assertTokens: func(require *require.Assertions, key Key) {
				require.Equal(byte(0), key.Token(0, 2))  // 00
				require.Equal(byte(1), key.Token(2, 2))  // 01
				require.Equal(byte(2), key.Token(4, 2))  // 10
				require.Equal(byte(3), key.Token(6, 2))  // 11 end first byte
				require.Equal(byte(3), key.Token(8, 2))  // 11
				require.Equal(byte(2), key.Token(10, 2)) // 10
				require.Equal(byte(1), key.Token(12, 2)) // 01
				require.Equal(byte(0), key.Token(14, 2)) // 00 end second byte
			},
		},
		{
			name: "branch factor 16",
			inputBytes: []byte{
				0b0000_0001,
				0b0010_0011,
				0b0100_0101,
				0b0110_0111,
				0b1000_1001,
				0b1010_1011,
				0b1100_1101,
				0b1110_1111,
			},
			assertTokens: func(require *require.Assertions, key Key) {
				for i := 0; i < 16; i++ {
					require.Equal(byte(i), key.Token(i*4, 4))
				}
			},
		},
	}

	for i := 0; i < 256; i++ {
		tests = append(tests, test{
			name:       fmt.Sprintf("branch factor 256, byte %d", i),
			inputBytes: []byte{byte(i)},
			assertTokens: func(require *require.Assertions, key Key) {
				require.Equal(byte(i), key.Token(0, 8))
			},
		})
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)
			key := ToKey(tt.inputBytes)
			tt.assertTokens(require, key)
		})
	}
}

func Test_Key_Append(t *testing.T) {
	require := require.New(t)

	key := ToKey([]byte{})
	for _, bf := range validBranchFactors {
		size := BranchFactorToTokenSize[bf]
		for i := 0; i < int(bf); i++ {
			appendedKey := key.Extend(ToToken(byte(i), size), ToToken(byte(i/2), size))
			require.Equal(byte(i), appendedKey.Token(0, size))
			require.Equal(byte(i/2), appendedKey.Token(size, size))
		}
	}
}

func Test_Key_AppendExtend(t *testing.T) {
	require := require.New(t)

	key2 := ToKey([]byte{0b1000_0000}).Take(1)
	p := ToKey([]byte{0b01010101})
	extendedP := key2.Extend(ToToken(0, 1), p)
	require.Equal([]byte{0b10010101, 0b01000_000}, extendedP.Bytes())
	require.Equal(byte(1), extendedP.Token(0, 1))
	require.Equal(byte(0), extendedP.Token(1, 1))
	require.Equal(byte(0), extendedP.Token(2, 1))
	require.Equal(byte(1), extendedP.Token(3, 1))
	require.Equal(byte(0), extendedP.Token(4, 1))
	require.Equal(byte(1), extendedP.Token(5, 1))
	require.Equal(byte(0), extendedP.Token(6, 1))
	require.Equal(byte(1), extendedP.Token(7, 1))
	require.Equal(byte(0), extendedP.Token(8, 1))
	require.Equal(byte(1), extendedP.Token(9, 1))

	p = ToKey([]byte{0b0101_0101, 0b1000_0000}).Take(9)
	extendedP = key2.Extend(ToToken(0, 1), p)
	require.Equal([]byte{0b1001_0101, 0b0110_0000}, extendedP.Bytes())
	require.Equal(byte(1), extendedP.Token(0, 1))
	require.Equal(byte(0), extendedP.Token(1, 1))
	require.Equal(byte(0), extendedP.Token(2, 1))
	require.Equal(byte(1), extendedP.Token(3, 1))
	require.Equal(byte(0), extendedP.Token(4, 1))
	require.Equal(byte(1), extendedP.Token(5, 1))
	require.Equal(byte(0), extendedP.Token(6, 1))
	require.Equal(byte(1), extendedP.Token(7, 1))
	require.Equal(byte(0), extendedP.Token(8, 1))
	require.Equal(byte(1), extendedP.Token(9, 1))
	require.Equal(byte(1), extendedP.Token(10, 1))

	key4 := ToKey([]byte{0b0100_0000}).Take(2)
	p = ToKey([]byte{0b0101_0101})
	extendedP = key4.Extend(ToToken(0, 2), p)
	require.Equal([]byte{0b0100_0101, 0b0101_0000}, extendedP.Bytes())
	require.Equal(byte(1), extendedP.Token(0, 2))
	require.Equal(byte(0), extendedP.Token(2, 2))
	require.Equal(byte(1), extendedP.Token(4, 2))
	require.Equal(byte(1), extendedP.Token(6, 2))
	require.Equal(byte(1), extendedP.Token(8, 2))
	require.Equal(byte(1), extendedP.Token(10, 2))

	key16 := ToKey([]byte{0b0001_0000}).Take(4)
	p = ToKey([]byte{0b0001_0001})
	extendedP = key16.Extend(ToToken(0, 4), p)
	require.Equal([]byte{0b0001_0000, 0b0001_0001}, extendedP.Bytes())
	require.Equal(byte(1), extendedP.Token(0, 4))
	require.Equal(byte(0), extendedP.Token(4, 4))
	require.Equal(byte(1), extendedP.Token(8, 4))
	require.Equal(byte(1), extendedP.Token(12, 4))

	p = ToKey([]byte{0b0001_0001, 0b0001_0001})
	extendedP = key16.Extend(ToToken(0, 4), p)
	require.Equal([]byte{0b0001_0000, 0b0001_0001, 0b0001_0001}, extendedP.Bytes())
	require.Equal(byte(1), extendedP.Token(0, 4))
	require.Equal(byte(0), extendedP.Token(4, 4))
	require.Equal(byte(1), extendedP.Token(8, 4))
	require.Equal(byte(1), extendedP.Token(12, 4))
	require.Equal(byte(1), extendedP.Token(16, 4))
	require.Equal(byte(1), extendedP.Token(20, 4))

	key256 := ToKey([]byte{0b0000_0001})
	p = ToKey([]byte{0b0000_0001})
	extendedP = key256.Extend(ToToken(0, 8), p)
	require.Equal([]byte{0b0000_0001, 0b0000_0000, 0b0000_0001}, extendedP.Bytes())
	require.Equal(byte(1), extendedP.Token(0, 8))
	require.Equal(byte(0), extendedP.Token(8, 8))
	require.Equal(byte(1), extendedP.Token(16, 8))
}

func TestKeyBytesNeeded(t *testing.T) {
	type test struct {
		bitLength   int
		bytesNeeded int
	}

	tests := []test{
		{
			bitLength:   7,
			bytesNeeded: 1,
		},
		{
			bitLength:   8,
			bytesNeeded: 1,
		},
		{
			bitLength:   9,
			bytesNeeded: 2,
		},
		{
			bitLength:   0,
			bytesNeeded: 0,
		},
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("bit length %d", tt.bitLength), func(t *testing.T) {
			require := require.New(t)
			require.Equal(tt.bytesNeeded, bytesNeeded(tt.bitLength))
		})
	}
}

func FuzzKeyDoubleExtend_Tokens(f *testing.F) {
	f.Fuzz(func(
		t *testing.T,
		first []byte,
		second []byte,
		tokenByte byte,
		forceFirstOdd bool,
		forceSecondOdd bool,
	) {
		require := require.New(t)
		for _, ts := range validTokenSizes {
			key1 := ToKey(first)
			if forceFirstOdd && key1.length > ts {
				key1 = key1.Take(key1.length - ts)
			}
			key2 := ToKey(second)
			if forceSecondOdd && key2.length > ts {
				key2 = key2.Take(key2.length - ts)
			}
			token := byte(int(tokenByte) % int(tokenSizeToBranchFactor[ts]))
			extendedP := key1.Extend(ToToken(token, ts), key2)
			require.Equal(key1.length+key2.length+ts, extendedP.length)
			firstIndex := 0
			for ; firstIndex < key1.length; firstIndex += ts {
				require.Equal(key1.Token(firstIndex, ts), extendedP.Token(firstIndex, ts))
			}
			require.Equal(token, extendedP.Token(firstIndex, ts))
			firstIndex += ts
			for secondIndex := 0; secondIndex < key2.length; secondIndex += ts {
				require.Equal(key2.Token(secondIndex, ts), extendedP.Token(firstIndex+secondIndex, ts))
			}
		}
	})
}

func FuzzKeyDoubleExtend_Any(f *testing.F) {
	f.Fuzz(func(
		t *testing.T,
		baseKeyBytes []byte,
		firstKeyBytes []byte,
		secondKeyBytes []byte,
		forceBaseOdd bool,
		forceFirstOdd bool,
		forceSecondOdd bool,
	) {
		require := require.New(t)
		for _, ts := range validTokenSizes {
			baseKey := ToKey(baseKeyBytes)
			if forceBaseOdd && baseKey.length > ts {
				baseKey = baseKey.Take(baseKey.length - ts)
			}
			firstKey := ToKey(firstKeyBytes)
			if forceFirstOdd && firstKey.length > ts {
				firstKey = firstKey.Take(firstKey.length - ts)
			}

			secondKey := ToKey(secondKeyBytes)
			if forceSecondOdd && secondKey.length > ts {
				secondKey = secondKey.Take(secondKey.length - ts)
			}

			extendedP := baseKey.Extend(firstKey, secondKey)
			require.Equal(baseKey.length+firstKey.length+secondKey.length, extendedP.length)
			totalIndex := 0
			for baseIndex := 0; baseIndex < baseKey.length; baseIndex += ts {
				require.Equal(baseKey.Token(baseIndex, ts), extendedP.Token(baseIndex, ts))
			}
			totalIndex += baseKey.length
			for firstIndex := 0; firstIndex < firstKey.length; firstIndex += ts {
				require.Equal(firstKey.Token(firstIndex, ts), extendedP.Token(totalIndex+firstIndex, ts))
			}
			totalIndex += firstKey.length
			for secondIndex := 0; secondIndex < secondKey.length; secondIndex += ts {
				require.Equal(secondKey.Token(secondIndex, ts), extendedP.Token(totalIndex+secondIndex, ts))
			}
		}
	})
}

func FuzzKeySkip(f *testing.F) {
	f.Fuzz(func(
		t *testing.T,
		first []byte,
		tokensToSkip uint,
	) {
		require := require.New(t)
		key1 := ToKey(first)
		for _, ts := range validTokenSizes {
			// need bits to be a multiple of token size
			ubitsToSkip := tokensToSkip * uint(ts)
			if ubitsToSkip >= uint(key1.length) {
				t.SkipNow()
			}
			bitsToSkip := int(ubitsToSkip)
			key2 := key1.Skip(bitsToSkip)
			require.Equal(key1.length-bitsToSkip, key2.length)
			for i := 0; i < key2.length; i += ts {
				require.Equal(key1.Token(bitsToSkip+i, ts), key2.Token(i, ts))
			}
		}
	})
}

func FuzzKeyTake(f *testing.F) {
	f.Fuzz(func(
		t *testing.T,
		first []byte,
		uTokensToTake uint,
	) {
		require := require.New(t)
		for _, ts := range validTokenSizes {
			key1 := ToKey(first)
			uBitsToTake := uTokensToTake * uint(ts)
			if uBitsToTake >= uint(key1.length) {
				t.SkipNow()
			}
			bitsToTake := int(uBitsToTake)
			key2 := key1.Take(bitsToTake)
			require.Equal(bitsToTake, key2.length)
			if key2.hasPartialByte() {
				paddingMask := byte(0xFF >> (key2.length % 8))
				require.Zero(key2.value[len(key2.value)-1] & paddingMask)
			}
			for i := 0; i < bitsToTake; i += ts {
				require.Equal(key1.Token(i, ts), key2.Token(i, ts))
			}
		}
	})
}

func TestShiftCopy(t *testing.T) {
	type test struct {
		dst      []byte
		src      []byte
		expected []byte
		shift    int
	}

	tests := []test{
		{
			dst:      []byte{},
			src:      []byte{},
			expected: []byte{},
			shift:    0,
		},
		{
			dst:      []byte{},
			src:      []byte{},
			expected: []byte{},
			shift:    1,
		},
		{
			dst:      make([]byte, 1),
			src:      []byte{0b0000_0001},
			expected: []byte{0b0000_0010},
			shift:    1,
		},
		{
			dst:      make([]byte, 1),
			src:      []byte{0b0000_0001},
			expected: []byte{0b0000_0100},
			shift:    2,
		},
		{
			dst:      make([]byte, 1),
			src:      []byte{0b0000_0001},
			expected: []byte{0b1000_0000},
			shift:    7,
		},
		{
			dst:      make([]byte, 2),
			src:      []byte{0b0000_0001, 0b1000_0001},
			expected: []byte{0b0000_0011, 0b0000_0010},
			shift:    1,
		},
		{
			dst:      make([]byte, 1),
			src:      []byte{0b0000_0001, 0b1000_0001},
			expected: []byte{0b0000_0011},
			shift:    1,
		},
		{
			dst:      make([]byte, 2),
			src:      []byte{0b0000_0001, 0b1000_0001},
			expected: []byte{0b1100_0000, 0b1000_0000},
			shift:    7,
		},
		{
			dst:      make([]byte, 1),
			src:      []byte{0b0000_0001, 0b1000_0001},
			expected: []byte{0b1100_0000},
			shift:    7,
		},
		{
			dst:      make([]byte, 2),
			src:      []byte{0b0000_0001, 0b1000_0001},
			expected: []byte{0b1000_0001, 0b0000_0000},
			shift:    8,
		},
		{
			dst:      make([]byte, 1),
			src:      []byte{0b0000_0001, 0b1000_0001},
			expected: []byte{0b1000_0001},
			shift:    8,
		},
		{
			dst:      make([]byte, 2),
			src:      []byte{0b0000_0001, 0b1000_0001, 0b1111_0101},
			expected: []byte{0b0000_0110, 0b000_0111},
			shift:    2,
		},
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("dst: %v, src: %v", tt.dst, tt.src), func(t *testing.T) {
			shiftCopy(tt.dst, string(tt.src), tt.shift)
			require.Equal(t, tt.expected, tt.dst)
		})
	}
}

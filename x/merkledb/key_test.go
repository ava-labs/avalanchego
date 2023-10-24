// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package merkledb

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestHasPartialByte(t *testing.T) {
	for _, branchFactor := range validTokenConfigurations {
		t.Run(fmt.Sprint(branchFactor), func(t *testing.T) {
			require := require.New(t)

			key := Key{}
			require.False(key.hasPartialByte())

			if branchFactor == BranchFactor256TokenConfig {
				// Tokens are an entire byte so
				// there is never a partial byte.
				key = key.Append(branchFactor.ToToken(1))
				require.False(key.hasPartialByte())
				key = key.Append(branchFactor.ToToken(0))
				require.False(key.hasPartialByte())
				return
			}

			// Fill all but the last token of the first byte.
			for i := 0; i < 8-branchFactor.bitsPerToken; i += branchFactor.bitsPerToken {
				key = key.Append(branchFactor.ToToken(1))
				require.True(key.hasPartialByte())
			}

			// Fill the last token of the first byte.
			key = key.Append(branchFactor.ToToken(0))
			require.False(key.hasPartialByte())

			// Fill the first token of the second byte.
			key = key.Append(branchFactor.ToToken(0))
			require.True(key.hasPartialByte())
		})
	}
}

func Test_Key_Has_Prefix(t *testing.T) {
	type test struct {
		name           string
		keyA           func(bf TokenConfiguration) Key
		keyB           func(bf TokenConfiguration) Key
		isStrictPrefix bool
		isPrefix       bool
	}

	key := "Key"

	tests := []test{
		{
			name:           "equal keys",
			keyA:           func(bf TokenConfiguration) Key { return ToKey([]byte(key)) },
			keyB:           func(bf TokenConfiguration) Key { return ToKey([]byte(key)) },
			isPrefix:       true,
			isStrictPrefix: false,
		},
		{
			name: "one key has one fewer token",
			keyA: func(bf TokenConfiguration) Key { return ToKey([]byte(key)) },
			keyB: func(bf TokenConfiguration) Key {
				return ToKey([]byte(key)).Take(len(key)*8 - bf.bitsPerToken)
			},
			isPrefix:       true,
			isStrictPrefix: true,
		},
		{
			name: "equal keys, both have one fewer token",
			keyA: func(bf TokenConfiguration) Key {
				return ToKey([]byte(key)).Take(len(key)*8 - bf.bitsPerToken)
			},
			keyB: func(bf TokenConfiguration) Key {
				return ToKey([]byte(key)).Take(len(key)*8 - bf.bitsPerToken)
			},
			isPrefix:       true,
			isStrictPrefix: false,
		},
		{
			name:           "different keys",
			keyA:           func(bf TokenConfiguration) Key { return ToKey([]byte{0xF7}) },
			keyB:           func(bf TokenConfiguration) Key { return ToKey([]byte{0xF0}) },
			isPrefix:       false,
			isStrictPrefix: false,
		},
		{
			name: "same bytes, different lengths",
			keyA: func(bf TokenConfiguration) Key {
				return ToKey([]byte{0x10, 0x00}).Take(bf.bitsPerToken)
			},
			keyB: func(bf TokenConfiguration) Key {
				return ToKey([]byte{0x10, 0x00}).Take(bf.bitsPerToken * 2)
			},
			isPrefix:       false,
			isStrictPrefix: false,
		},
	}

	for _, tt := range tests {
		for _, bf := range validTokenConfigurations {
			t.Run(tt.name+" bf "+fmt.Sprint(bf), func(t *testing.T) {
				require := require.New(t)
				keyA := tt.keyA(bf)
				keyB := tt.keyB(bf)

				require.Equal(tt.isPrefix, keyA.HasPrefix(keyB))
				require.Equal(tt.isPrefix, keyA.iteratedHasPrefix(keyB, 0, bf.bitsPerToken))
				require.Equal(tt.isStrictPrefix, keyA.HasStrictPrefix(keyB))
			})
		}
	}
}

func Test_Key_Skip(t *testing.T) {
	require := require.New(t)

	empty := Key{}
	require.Equal(ToKey([]byte{0}).Skip(8), empty)
	for _, bf := range validTokenConfigurations {
		if bf == BranchFactor256TokenConfig {
			continue
		}
		shortKey := ToKey([]byte{0b0101_0101})
		longKey := ToKey([]byte{0b0101_0101, 0b0101_0101})
		for shift := 0; shift < 8; shift += bf.bitsPerToken {
			skipKey := shortKey.Skip(shift)
			require.Equal(byte(0b0101_0101<<shift), skipKey.value[0])

			skipKey = longKey.Skip(shift)
			require.Equal(byte(0b0101_0101<<shift+0b0101_0101>>(8-shift)), skipKey.value[0])
			require.Equal(byte(0b0101_0101<<shift), skipKey.value[1])
		}
	}

	skip := ToKey([]byte{0b0101_0101, 0b1010_1010}).Skip(BranchFactor256TokenConfig.bitsPerToken)
	require.Len(skip.value, 1)
	require.Equal(byte(0b1010_1010), skip.value[0])

	skip = ToKey([]byte{0b0101_0101, 0b1010_1010, 0b0101_0101}).Skip(BranchFactor256TokenConfig.bitsPerToken)
	require.Len(skip.value, 2)
	require.Equal(byte(0b1010_1010), skip.value[0])
	require.Equal(byte(0b0101_0101), skip.value[1])
}

func Test_Key_Take(t *testing.T) {
	require := require.New(t)

	require.Equal(ToKey([]byte{0}).Take(0), Key{})

	for _, bf := range validTokenConfigurations {
		if bf == BranchFactor256TokenConfig {
			continue
		}
		key := ToKey([]byte{0b0101_0101})
		for length := bf.bitsPerToken; length <= 8; length += bf.bitsPerToken {
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
		branchFactor TokenConfiguration
		assertTokens func(*require.Assertions, Key)
	}

	tests := []test{
		{
			name:         "branch factor 2",
			inputBytes:   []byte{0b0_1_0_1_0_1_0_1, 0b1_0_1_0_1_0_1_0},
			branchFactor: BranchFactor2TokenConfig,
			assertTokens: func(require *require.Assertions, key Key) {
				require.Equal(byte(0), key.Token(0, BranchFactor2TokenConfig.bitsPerToken))
				require.Equal(byte(1), key.Token(1, BranchFactor2TokenConfig.bitsPerToken))
				require.Equal(byte(0), key.Token(2, BranchFactor2TokenConfig.bitsPerToken))
				require.Equal(byte(1), key.Token(3, BranchFactor2TokenConfig.bitsPerToken))
				require.Equal(byte(0), key.Token(4, BranchFactor2TokenConfig.bitsPerToken))
				require.Equal(byte(1), key.Token(5, BranchFactor2TokenConfig.bitsPerToken))
				require.Equal(byte(0), key.Token(6, BranchFactor2TokenConfig.bitsPerToken))
				require.Equal(byte(1), key.Token(7, BranchFactor2TokenConfig.bitsPerToken)) // end first byte
				require.Equal(byte(1), key.Token(8, BranchFactor2TokenConfig.bitsPerToken))
				require.Equal(byte(0), key.Token(9, BranchFactor2TokenConfig.bitsPerToken))
				require.Equal(byte(1), key.Token(10, BranchFactor2TokenConfig.bitsPerToken))
				require.Equal(byte(0), key.Token(11, BranchFactor2TokenConfig.bitsPerToken))
				require.Equal(byte(1), key.Token(12, BranchFactor2TokenConfig.bitsPerToken))
				require.Equal(byte(0), key.Token(13, BranchFactor2TokenConfig.bitsPerToken))
				require.Equal(byte(1), key.Token(14, BranchFactor2TokenConfig.bitsPerToken))
				require.Equal(byte(0), key.Token(15, BranchFactor2TokenConfig.bitsPerToken)) // end second byte
			},
		},
		{
			name:         "branch factor 4",
			inputBytes:   []byte{0b00_01_10_11, 0b11_10_01_00},
			branchFactor: BranchFactor4TokenConfig,
			assertTokens: func(require *require.Assertions, key Key) {
				require.Equal(byte(0), key.Token(0, BranchFactor4TokenConfig.bitsPerToken))  // 00
				require.Equal(byte(1), key.Token(2, BranchFactor4TokenConfig.bitsPerToken))  // 01
				require.Equal(byte(2), key.Token(4, BranchFactor4TokenConfig.bitsPerToken))  // 10
				require.Equal(byte(3), key.Token(6, BranchFactor4TokenConfig.bitsPerToken))  // 11 end first byte
				require.Equal(byte(3), key.Token(8, BranchFactor4TokenConfig.bitsPerToken))  // 11
				require.Equal(byte(2), key.Token(10, BranchFactor4TokenConfig.bitsPerToken)) // 10
				require.Equal(byte(1), key.Token(12, BranchFactor4TokenConfig.bitsPerToken)) // 01
				require.Equal(byte(0), key.Token(14, BranchFactor4TokenConfig.bitsPerToken)) // 00 end second byte
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
			branchFactor: BranchFactor16TokenConfig,
			assertTokens: func(require *require.Assertions, key Key) {
				for i := 0; i < 16; i++ {
					require.Equal(byte(i), key.Token(i*BranchFactor16TokenConfig.bitsPerToken, BranchFactor16TokenConfig.bitsPerToken))
				}
			},
		},
	}

	for i := 0; i < 256; i++ {
		i := i
		tests = append(tests, test{
			name:         fmt.Sprintf("branch factor 256, byte %d", i),
			inputBytes:   []byte{byte(i)},
			branchFactor: BranchFactor256TokenConfig,
			assertTokens: func(require *require.Assertions, key Key) {
				require.Equal(byte(i), key.Token(0, BranchFactor256TokenConfig.bitsPerToken))
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
	for _, bf := range validTokenConfigurations {
		for i := 0; i < bf.branchFactor; i++ {
			appendedKey := key.Append(bf.ToToken(byte(i))).Append(bf.ToToken(byte(i / 2)))
			require.Equal(byte(i), appendedKey.Token(0, bf.bitsPerToken))
			require.Equal(byte(i/2), appendedKey.Token(bf.bitsPerToken, bf.bitsPerToken))
		}
	}
}

func Test_Key_AppendExtend(t *testing.T) {
	require := require.New(t)

	key2 := ToKey([]byte{0b1000_0000}).Take(1)
	p := ToKey([]byte{0b01010101})
	extendedP := key2.AppendExtend(BranchFactor2TokenConfig.ToToken(0), p)
	require.Equal([]byte{0b10010101, 0b01000_000}, extendedP.Bytes())
	require.Equal(byte(1), extendedP.Token(0, BranchFactor2TokenConfig.bitsPerToken))
	require.Equal(byte(0), extendedP.Token(1, BranchFactor2TokenConfig.bitsPerToken))
	require.Equal(byte(0), extendedP.Token(2, BranchFactor2TokenConfig.bitsPerToken))
	require.Equal(byte(1), extendedP.Token(3, BranchFactor2TokenConfig.bitsPerToken))
	require.Equal(byte(0), extendedP.Token(4, BranchFactor2TokenConfig.bitsPerToken))
	require.Equal(byte(1), extendedP.Token(5, BranchFactor2TokenConfig.bitsPerToken))
	require.Equal(byte(0), extendedP.Token(6, BranchFactor2TokenConfig.bitsPerToken))
	require.Equal(byte(1), extendedP.Token(7, BranchFactor2TokenConfig.bitsPerToken))
	require.Equal(byte(0), extendedP.Token(8, BranchFactor2TokenConfig.bitsPerToken))
	require.Equal(byte(1), extendedP.Token(9, BranchFactor2TokenConfig.bitsPerToken))

	p = ToKey([]byte{0b0101_0101, 0b1000_0000}).Take(9)
	extendedP = key2.AppendExtend(BranchFactor2TokenConfig.ToToken(0), p)
	require.Equal([]byte{0b1001_0101, 0b0110_0000}, extendedP.Bytes())
	require.Equal(byte(1), extendedP.Token(0, BranchFactor2TokenConfig.bitsPerToken))
	require.Equal(byte(0), extendedP.Token(1, BranchFactor2TokenConfig.bitsPerToken))
	require.Equal(byte(0), extendedP.Token(2, BranchFactor2TokenConfig.bitsPerToken))
	require.Equal(byte(1), extendedP.Token(3, BranchFactor2TokenConfig.bitsPerToken))
	require.Equal(byte(0), extendedP.Token(4, BranchFactor2TokenConfig.bitsPerToken))
	require.Equal(byte(1), extendedP.Token(5, BranchFactor2TokenConfig.bitsPerToken))
	require.Equal(byte(0), extendedP.Token(6, BranchFactor2TokenConfig.bitsPerToken))
	require.Equal(byte(1), extendedP.Token(7, BranchFactor2TokenConfig.bitsPerToken))
	require.Equal(byte(0), extendedP.Token(8, BranchFactor2TokenConfig.bitsPerToken))
	require.Equal(byte(1), extendedP.Token(9, BranchFactor2TokenConfig.bitsPerToken))
	require.Equal(byte(1), extendedP.Token(10, BranchFactor2TokenConfig.bitsPerToken))

	key4 := ToKey([]byte{0b0100_0000}).Take(2)
	p = ToKey([]byte{0b0101_0101})
	extendedP = key4.AppendExtend(BranchFactor4TokenConfig.ToToken(0), p)
	require.Equal([]byte{0b0100_0101, 0b0101_0000}, extendedP.Bytes())
	require.Equal(byte(1), extendedP.Token(0, BranchFactor4TokenConfig.bitsPerToken))
	require.Equal(byte(0), extendedP.Token(2, BranchFactor4TokenConfig.bitsPerToken))
	require.Equal(byte(1), extendedP.Token(4, BranchFactor4TokenConfig.bitsPerToken))
	require.Equal(byte(1), extendedP.Token(6, BranchFactor4TokenConfig.bitsPerToken))
	require.Equal(byte(1), extendedP.Token(8, BranchFactor4TokenConfig.bitsPerToken))
	require.Equal(byte(1), extendedP.Token(10, BranchFactor4TokenConfig.bitsPerToken))

	key16 := ToKey([]byte{0b0001_0000}).Take(4)
	p = ToKey([]byte{0b0001_0001})
	extendedP = key16.AppendExtend(BranchFactor16TokenConfig.ToToken(0), p)
	require.Equal([]byte{0b0001_0000, 0b0001_0001}, extendedP.Bytes())
	require.Equal(byte(1), extendedP.Token(0, BranchFactor16TokenConfig.bitsPerToken))
	require.Equal(byte(0), extendedP.Token(4, BranchFactor16TokenConfig.bitsPerToken))
	require.Equal(byte(1), extendedP.Token(8, BranchFactor16TokenConfig.bitsPerToken))
	require.Equal(byte(1), extendedP.Token(12, BranchFactor16TokenConfig.bitsPerToken))

	p = ToKey([]byte{0b0001_0001, 0b0001_0001})
	extendedP = key16.AppendExtend(BranchFactor16TokenConfig.ToToken(0), p)
	require.Equal([]byte{0b0001_0000, 0b0001_0001, 0b0001_0001}, extendedP.Bytes())
	require.Equal(byte(1), extendedP.Token(0, BranchFactor16TokenConfig.bitsPerToken))
	require.Equal(byte(0), extendedP.Token(4, BranchFactor16TokenConfig.bitsPerToken))
	require.Equal(byte(1), extendedP.Token(8, BranchFactor16TokenConfig.bitsPerToken))
	require.Equal(byte(1), extendedP.Token(12, BranchFactor16TokenConfig.bitsPerToken))
	require.Equal(byte(1), extendedP.Token(16, BranchFactor16TokenConfig.bitsPerToken))
	require.Equal(byte(1), extendedP.Token(20, BranchFactor16TokenConfig.bitsPerToken))

	key256 := ToKey([]byte{0b0000_0001})
	p = ToKey([]byte{0b0000_0001})
	extendedP = key256.AppendExtend(BranchFactor256TokenConfig.ToToken(0), p)
	require.Equal([]byte{0b0000_0001, 0b0000_0000, 0b0000_0001}, extendedP.Bytes())
	require.Equal(byte(1), extendedP.Token(0, BranchFactor256TokenConfig.bitsPerToken))
	require.Equal(byte(0), extendedP.Token(8, BranchFactor256TokenConfig.bitsPerToken))
	require.Equal(byte(1), extendedP.Token(16, BranchFactor256TokenConfig.bitsPerToken))
}

func TestKeyBytesNeeded(t *testing.T) {
	type test struct {
		TokenConfiguration
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
		t.Run(fmt.Sprintf("branch factor %d, tokens length %d", tt.TokenConfiguration, tt.bitLength), func(t *testing.T) {
			require := require.New(t)
			require.Equal(tt.bytesNeeded, bytesNeeded(tt.bitLength))
		})
	}
}

func FuzzKeyAppendExtend(f *testing.F) {
	f.Fuzz(func(
		t *testing.T,
		first []byte,
		second []byte,
		tokenByte byte,
		forceFirstOdd bool,
		forceSecondOdd bool,
	) {
		require := require.New(t)
		for _, tokenConfig := range validTokenConfigurations {
			key1 := ToKey(first)
			if forceFirstOdd && key1.length > tokenConfig.bitsPerToken {
				key1 = key1.Take(key1.length - tokenConfig.bitsPerToken)
			}
			key2 := ToKey(second)
			if forceSecondOdd && key2.length > tokenConfig.bitsPerToken {
				key2 = key2.Take(key2.length - tokenConfig.bitsPerToken)
			}
			token := byte(int(tokenByte) % tokenConfig.branchFactor)
			extendedP := key1.AppendExtend(tokenConfig.ToToken(token), key2)
			require.Equal(key1.length+key2.length+tokenConfig.bitsPerToken, extendedP.length)
			firstIndex := 0
			for ; firstIndex < key1.length; firstIndex += tokenConfig.bitsPerToken {
				require.Equal(key1.Token(firstIndex, tokenConfig.bitsPerToken), extendedP.Token(firstIndex, tokenConfig.bitsPerToken))
			}
			require.Equal(token, extendedP.Token(firstIndex, tokenConfig.bitsPerToken))
			firstIndex += tokenConfig.bitsPerToken
			for secondIndex := 0; secondIndex < key2.length; secondIndex += tokenConfig.bitsPerToken {
				require.Equal(key2.Token(secondIndex, tokenConfig.bitsPerToken), extendedP.Token(firstIndex+secondIndex, tokenConfig.bitsPerToken))
			}
		}
	})
}

func FuzzKeySkip(f *testing.F) {
	f.Fuzz(func(
		t *testing.T,
		first []byte,
		ubitsToSkip uint,
	) {
		require := require.New(t)
		key1 := ToKey(first)
		for _, tokenConfig := range validTokenConfigurations {
			if ubitsToSkip >= uint(key1.length) {
				t.SkipNow()
			}
			bitsToSkip := int(ubitsToSkip)
			key2 := key1.Skip(bitsToSkip)
			require.Equal(key1.length-bitsToSkip, key2.length)
			for i := 0; i < key2.length; i += tokenConfig.bitsPerToken {
				require.Equal(key1.Token(bitsToSkip+i, tokenConfig.bitsPerToken), key2.Token(i, tokenConfig.bitsPerToken))
			}
		}
	})
}

func FuzzKeyTake(f *testing.F) {
	f.Fuzz(func(
		t *testing.T,
		first []byte,
		uBitsToTake uint,
	) {
		require := require.New(t)
		for _, branchFactor := range validTokenConfigurations {
			key1 := ToKey(first)
			if uBitsToTake >= uint(key1.length) {
				t.SkipNow()
			}
			bitsToTake := int(uBitsToTake)
			key2 := key1.Take(bitsToTake)
			require.Equal(bitsToTake, key2.length)
			if key2.hasPartialByte() {
				paddingMask := byte(0xFF >> key2.remainderBitCount())
				require.Zero(key2.value[len(key2.value)-1] & paddingMask)
			}
			for i := 0; i < bitsToTake; i += branchFactor.bitsPerToken {
				require.Equal(key1.Token(i, branchFactor.bitsPerToken), key2.Token(i, branchFactor.bitsPerToken))
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

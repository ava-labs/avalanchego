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
				key = key.Append(branchFactor, 1)
				require.False(key.hasPartialByte())
				key = key.Append(branchFactor, 0)
				require.False(key.hasPartialByte())
				return
			}

			// Fill all but the last token of the first byte.
			for i := 0; i < branchFactor.tokensPerByte-1; i++ {
				key = key.Append(branchFactor, 1)
				require.True(key.hasPartialByte())
			}

			// Fill the last token of the first byte.
			key = key.Append(branchFactor, 0)
			require.False(key.hasPartialByte())

			// Fill the first token of the second byte.
			key = key.Append(branchFactor, 0)
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
				return ToKey([]byte(key)).Take(bf, len(key)*bf.tokensPerByte-1)
			},
			isPrefix:       true,
			isStrictPrefix: true,
		},
		{
			name: "equal keys, both have one fewer token",
			keyA: func(bf TokenConfiguration) Key {
				return ToKey([]byte(key)).Take(bf, len(key)*bf.tokensPerByte-1)
			},
			keyB: func(bf TokenConfiguration) Key {
				return ToKey([]byte(key)).Take(bf, len(key)*bf.tokensPerByte-1)
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
				return ToKey([]byte{0x10, 0x00}).Take(bf, 1)
			},
			keyB: func(bf TokenConfiguration) Key {
				return ToKey([]byte{0x10, 0x00}).Take(bf, 2)
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
				require.Equal(tt.isPrefix, keyA.iteratedHasPrefix(bf, 0, keyB))
				require.Equal(tt.isStrictPrefix, keyA.HasStrictPrefix(keyB))
			})
		}
	}
}

func Test_Key_Skip(t *testing.T) {
	require := require.New(t)

	for _, bf := range validTokenConfigurations {
		empty := Key{}
		require.Equal(ToKey([]byte{0}).Skip(bf, bf.tokensPerByte), empty)
		if bf == BranchFactor256TokenConfig {
			continue
		}
		shortKey := ToKey([]byte{0b0101_0101})
		longKey := ToKey([]byte{0b0101_0101, 0b0101_0101})
		for i := 0; i < bf.tokensPerByte; i++ {
			shift := bf.BitLength(i)
			skipKey := shortKey.Skip(bf, i)
			require.Equal(byte(0b0101_0101<<shift), skipKey.value[0])

			skipKey = longKey.Skip(bf, i)
			require.Equal(byte(0b0101_0101<<shift+0b0101_0101>>(8-shift)), skipKey.value[0])
			require.Equal(byte(0b0101_0101<<shift), skipKey.value[1])
		}
	}

	skip := ToKey([]byte{0b0101_0101, 0b1010_1010}).Skip(BranchFactor256TokenConfig, 1)
	require.Len(skip.value, 1)
	require.Equal(byte(0b1010_1010), skip.value[0])

	skip = ToKey([]byte{0b0101_0101, 0b1010_1010, 0b0101_0101}).Skip(BranchFactor256TokenConfig, 1)
	require.Len(skip.value, 2)
	require.Equal(byte(0b1010_1010), skip.value[0])
	require.Equal(byte(0b0101_0101), skip.value[1])
}

func Test_Key_Take(t *testing.T) {
	require := require.New(t)

	for _, bf := range validTokenConfigurations {
		require.Equal(ToKey([]byte{0}).Take(bf, 0), Key{})
		if bf == BranchFactor256TokenConfig {
			continue
		}
		key := ToKey([]byte{0b0101_0101})
		for i := 1; i <= bf.tokensPerByte; i++ {
			take := key.Take(bf, i)
			length := i * bf.tokenBitSize
			require.Equal(length, take.bitLength)
			shift := 8 - length
			require.Equal(byte((0b0101_0101>>shift)<<shift), take.value[0])
		}
	}

	take := ToKey([]byte{0b0101_0101, 0b1010_1010}).Take(BranchFactor256TokenConfig, 1)
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
				require.Equal(byte(0), key.Token(BranchFactor2TokenConfig, 0))
				require.Equal(byte(1), key.Token(BranchFactor2TokenConfig, 1))
				require.Equal(byte(0), key.Token(BranchFactor2TokenConfig, 2))
				require.Equal(byte(1), key.Token(BranchFactor2TokenConfig, 3))
				require.Equal(byte(0), key.Token(BranchFactor2TokenConfig, 4))
				require.Equal(byte(1), key.Token(BranchFactor2TokenConfig, 5))
				require.Equal(byte(0), key.Token(BranchFactor2TokenConfig, 6))
				require.Equal(byte(1), key.Token(BranchFactor2TokenConfig, 7)) // end first byte
				require.Equal(byte(1), key.Token(BranchFactor2TokenConfig, 8))
				require.Equal(byte(0), key.Token(BranchFactor2TokenConfig, 9))
				require.Equal(byte(1), key.Token(BranchFactor2TokenConfig, 10))
				require.Equal(byte(0), key.Token(BranchFactor2TokenConfig, 11))
				require.Equal(byte(1), key.Token(BranchFactor2TokenConfig, 12))
				require.Equal(byte(0), key.Token(BranchFactor2TokenConfig, 13))
				require.Equal(byte(1), key.Token(BranchFactor2TokenConfig, 14))
				require.Equal(byte(0), key.Token(BranchFactor2TokenConfig, 15)) // end second byte
			},
		},
		{
			name:         "branch factor 4",
			inputBytes:   []byte{0b00_01_10_11, 0b11_10_01_00},
			branchFactor: BranchFactor4TokenConfig,
			assertTokens: func(require *require.Assertions, key Key) {
				require.Equal(byte(0), key.Token(BranchFactor4TokenConfig, 0)) // 00
				require.Equal(byte(1), key.Token(BranchFactor4TokenConfig, 1)) // 01
				require.Equal(byte(2), key.Token(BranchFactor4TokenConfig, 2)) // 10
				require.Equal(byte(3), key.Token(BranchFactor4TokenConfig, 3)) // 11 end first byte
				require.Equal(byte(3), key.Token(BranchFactor4TokenConfig, 4)) // 11
				require.Equal(byte(2), key.Token(BranchFactor4TokenConfig, 5)) // 10
				require.Equal(byte(1), key.Token(BranchFactor4TokenConfig, 6)) // 01
				require.Equal(byte(0), key.Token(BranchFactor4TokenConfig, 7)) // 00 end second byte
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
					require.Equal(byte(i), key.Token(BranchFactor16TokenConfig, i))
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
				require.Equal(byte(i), key.Token(BranchFactor256TokenConfig, 0))
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
			appendedKey := key.Append(bf, byte(i)).Append(bf, byte(i/2))
			require.Equal(byte(i), appendedKey.Token(bf, 0))
			require.Equal(byte(i/2), appendedKey.Token(bf, 1))
		}
	}
}

func Test_Key_AppendExtend(t *testing.T) {
	require := require.New(t)

	key2 := ToKey([]byte{0b1000_0000}).Take(BranchFactor2TokenConfig, 1)
	p := ToKey([]byte{0b01010101})
	extendedP := key2.AppendExtend(BranchFactor2TokenConfig, 0, p)
	require.Equal([]byte{0b10010101, 0b01000_000}, extendedP.Bytes())
	require.Equal(byte(1), extendedP.Token(BranchFactor2TokenConfig, 0))
	require.Equal(byte(0), extendedP.Token(BranchFactor2TokenConfig, 1))
	require.Equal(byte(0), extendedP.Token(BranchFactor2TokenConfig, 2))
	require.Equal(byte(1), extendedP.Token(BranchFactor2TokenConfig, 3))
	require.Equal(byte(0), extendedP.Token(BranchFactor2TokenConfig, 4))
	require.Equal(byte(1), extendedP.Token(BranchFactor2TokenConfig, 5))
	require.Equal(byte(0), extendedP.Token(BranchFactor2TokenConfig, 6))
	require.Equal(byte(1), extendedP.Token(BranchFactor2TokenConfig, 7))
	require.Equal(byte(0), extendedP.Token(BranchFactor2TokenConfig, 8))
	require.Equal(byte(1), extendedP.Token(BranchFactor2TokenConfig, 9))

	p = ToKey([]byte{0b0101_0101, 0b1000_0000}).Take(BranchFactor2TokenConfig, 9)
	extendedP = key2.AppendExtend(BranchFactor2TokenConfig, 0, p)
	require.Equal([]byte{0b1001_0101, 0b0110_0000}, extendedP.Bytes())
	require.Equal(byte(1), extendedP.Token(BranchFactor2TokenConfig, 0))
	require.Equal(byte(0), extendedP.Token(BranchFactor2TokenConfig, 1))
	require.Equal(byte(0), extendedP.Token(BranchFactor2TokenConfig, 2))
	require.Equal(byte(1), extendedP.Token(BranchFactor2TokenConfig, 3))
	require.Equal(byte(0), extendedP.Token(BranchFactor2TokenConfig, 4))
	require.Equal(byte(1), extendedP.Token(BranchFactor2TokenConfig, 5))
	require.Equal(byte(0), extendedP.Token(BranchFactor2TokenConfig, 6))
	require.Equal(byte(1), extendedP.Token(BranchFactor2TokenConfig, 7))
	require.Equal(byte(0), extendedP.Token(BranchFactor2TokenConfig, 8))
	require.Equal(byte(1), extendedP.Token(BranchFactor2TokenConfig, 9))
	require.Equal(byte(1), extendedP.Token(BranchFactor2TokenConfig, 10))

	key4 := ToKey([]byte{0b0100_0000}).Take(BranchFactor4TokenConfig, 1)
	p = ToKey([]byte{0b0101_0101})
	extendedP = key4.AppendExtend(BranchFactor4TokenConfig, 0, p)
	require.Equal([]byte{0b0100_0101, 0b0101_0000}, extendedP.Bytes())
	require.Equal(byte(1), extendedP.Token(BranchFactor4TokenConfig, 0))
	require.Equal(byte(0), extendedP.Token(BranchFactor4TokenConfig, 1))
	require.Equal(byte(1), extendedP.Token(BranchFactor4TokenConfig, 2))
	require.Equal(byte(1), extendedP.Token(BranchFactor4TokenConfig, 3))
	require.Equal(byte(1), extendedP.Token(BranchFactor4TokenConfig, 4))
	require.Equal(byte(1), extendedP.Token(BranchFactor4TokenConfig, 5))

	key16 := ToKey([]byte{0b0001_0000}).Take(BranchFactor16TokenConfig, 1)
	p = ToKey([]byte{0b0001_0001})
	extendedP = key16.AppendExtend(BranchFactor16TokenConfig, 0, p)
	require.Equal([]byte{0b0001_0000, 0b0001_0001}, extendedP.Bytes())
	require.Equal(byte(1), extendedP.Token(BranchFactor16TokenConfig, 0))
	require.Equal(byte(0), extendedP.Token(BranchFactor16TokenConfig, 1))
	require.Equal(byte(1), extendedP.Token(BranchFactor16TokenConfig, 2))
	require.Equal(byte(1), extendedP.Token(BranchFactor16TokenConfig, 3))

	p = ToKey([]byte{0b0001_0001, 0b0001_0001})
	extendedP = key16.AppendExtend(BranchFactor16TokenConfig, 0, p)
	require.Equal([]byte{0b0001_0000, 0b0001_0001, 0b0001_0001}, extendedP.Bytes())
	require.Equal(byte(1), extendedP.Token(BranchFactor16TokenConfig, 0))
	require.Equal(byte(0), extendedP.Token(BranchFactor16TokenConfig, 1))
	require.Equal(byte(1), extendedP.Token(BranchFactor16TokenConfig, 2))
	require.Equal(byte(1), extendedP.Token(BranchFactor16TokenConfig, 3))
	require.Equal(byte(1), extendedP.Token(BranchFactor16TokenConfig, 4))
	require.Equal(byte(1), extendedP.Token(BranchFactor16TokenConfig, 5))

	key256 := ToKey([]byte{0b0000_0001})
	p = ToKey([]byte{0b0000_0001})
	extendedP = key256.AppendExtend(BranchFactor256TokenConfig, 0, p)
	require.Equal([]byte{0b0000_0001, 0b0000_0000, 0b0000_0001}, extendedP.Bytes())
	require.Equal(byte(1), extendedP.Token(BranchFactor256TokenConfig, 0))
	require.Equal(byte(0), extendedP.Token(BranchFactor256TokenConfig, 1))
	require.Equal(byte(1), extendedP.Token(BranchFactor256TokenConfig, 2))
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
			tokenLength := tokenConfig.tokensPerByte * len(first)
			if forceFirstOdd && key1.bitLength > tokenConfig.tokenBitSize {
				key1 = key1.Take(tokenConfig, tokenLength-1)
			}
			key2 := ToKey(second)
			tokenLength = tokenConfig.tokensPerByte * len(second)
			if forceSecondOdd && key2.bitLength > tokenConfig.tokenBitSize {
				key2 = key2.Take(tokenConfig, tokenLength-1)
			}
			token := byte(int(tokenByte) % tokenConfig.branchFactor)
			extendedP := key1.AppendExtend(tokenConfig, token, key2)
			require.Equal(key1.bitLength+key2.bitLength+tokenConfig.tokenBitSize, extendedP.bitLength)
			firstIndex := 0
			for ; firstIndex < tokenConfig.TokenLength(key1); firstIndex++ {
				require.Equal(key1.Token(tokenConfig, firstIndex), extendedP.Token(tokenConfig, firstIndex))
			}
			require.Equal(token, extendedP.Token(tokenConfig, firstIndex))
			firstIndex++
			for secondIndex := 0; secondIndex < tokenConfig.TokenLength(key2); secondIndex++ {
				require.Equal(key2.Token(tokenConfig, secondIndex), extendedP.Token(tokenConfig, firstIndex+secondIndex))
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
		for _, tokenConfig := range validTokenConfigurations {
			bitsToSkip := tokenConfig.BitLength(int(tokensToSkip))
			if bitsToSkip >= key1.bitLength {
				t.SkipNow()
			}
			key2 := key1.Skip(tokenConfig, int(tokensToSkip))
			require.Equal(key1.bitLength-bitsToSkip, key2.bitLength)
			tokenLength := key2.bitLength / tokenConfig.tokenBitSize
			for i := 0; i < tokenLength; i++ {
				require.Equal(key1.Token(tokenConfig, int(tokensToSkip)+i), key2.Token(tokenConfig, i))
			}
		}
	})
}

func FuzzKeyTake(f *testing.F) {
	f.Fuzz(func(
		t *testing.T,
		first []byte,
		tokensToTake uint,
	) {
		require := require.New(t)
		for _, branchFactor := range validTokenConfigurations {
			key1 := ToKey(first)
			bitsToTake := branchFactor.BitLength(int(tokensToTake))
			if bitsToTake >= key1.bitLength {
				t.SkipNow()
			}
			key2 := key1.Take(branchFactor, int(tokensToTake))
			tokenLength := branchFactor.TokenLength(key2)
			require.Equal(int(tokensToTake), tokenLength)
			if key2.hasPartialByte() {
				paddingMask := byte(0xFF >> key2.remainderBitCount())
				require.Zero(key2.value[len(key2.value)-1] & paddingMask)
			}
			for i := 0; i < tokenLength; i++ {
				require.Equal(key1.Token(branchFactor, i), key2.Token(branchFactor, i))
			}
		}
	})
}

func TestShiftCopy(t *testing.T) {
	type test struct {
		dst      []byte
		src      []byte
		expected []byte
		shift    byte
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

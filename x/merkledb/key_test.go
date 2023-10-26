// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package merkledb

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

var branchFactors = []BranchFactor{
	BranchFactor2,
	BranchFactor4,
	BranchFactor16,
	BranchFactor256,
}

func TestHasPartialByte(t *testing.T) {
	for _, branchFactor := range branchFactors {
		t.Run(fmt.Sprint(branchFactor), func(t *testing.T) {
			require := require.New(t)

			key := emptyKey(branchFactor)
			require.False(key.hasPartialByte())

			if branchFactor == BranchFactor256 {
				// Tokens are an entire byte so
				// there is never a partial byte.
				key = key.Append(0)
				require.False(key.hasPartialByte())
				key = key.Append(0)
				require.False(key.hasPartialByte())
				return
			}

			// Fill all but the last token of the first byte.
			for i := 0; i < key.tokensPerByte-1; i++ {
				key = key.Append(0)
				require.True(key.hasPartialByte())
			}

			// Fill the last token of the first byte.
			key = key.Append(0)
			require.False(key.hasPartialByte())

			// Fill the first token of the second byte.
			key = key.Append(0)
			require.True(key.hasPartialByte())
		})
	}
}

func Test_Key_Has_Prefix(t *testing.T) {
	type test struct {
		name           string
		keyA           func(bf BranchFactor) Key
		keyB           func(bf BranchFactor) Key
		isStrictPrefix bool
		isPrefix       bool
	}

	key := "Key"
	keyLength := map[BranchFactor]int{}
	for _, branchFactor := range branchFactors {
		config := branchFactorToTokenConfig[branchFactor]
		keyLength[branchFactor] = len(key) * config.tokensPerByte
	}

	tests := []test{
		{
			name:           "equal keys",
			keyA:           func(bf BranchFactor) Key { return ToKey([]byte(key), bf) },
			keyB:           func(bf BranchFactor) Key { return ToKey([]byte(key), bf) },
			isPrefix:       true,
			isStrictPrefix: false,
		},
		{
			name:           "one key has one fewer token",
			keyA:           func(bf BranchFactor) Key { return ToKey([]byte(key), bf) },
			keyB:           func(bf BranchFactor) Key { return ToKey([]byte(key), bf).Take(keyLength[bf] - 1) },
			isPrefix:       true,
			isStrictPrefix: true,
		},
		{
			name:           "equal keys, both have one fewer token",
			keyA:           func(bf BranchFactor) Key { return ToKey([]byte(key), bf).Take(keyLength[bf] - 1) },
			keyB:           func(bf BranchFactor) Key { return ToKey([]byte(key), bf).Take(keyLength[bf] - 1) },
			isPrefix:       true,
			isStrictPrefix: false,
		},
		{
			name:           "different keys",
			keyA:           func(bf BranchFactor) Key { return ToKey([]byte{0xF7}, bf) },
			keyB:           func(bf BranchFactor) Key { return ToKey([]byte{0xF0}, bf) },
			isPrefix:       false,
			isStrictPrefix: false,
		},
		{
			name:           "same bytes, different lengths",
			keyA:           func(bf BranchFactor) Key { return ToKey([]byte{0x10, 0x00}, bf).Take(1) },
			keyB:           func(bf BranchFactor) Key { return ToKey([]byte{0x10, 0x00}, bf).Take(2) },
			isPrefix:       false,
			isStrictPrefix: false,
		},
	}

	for _, tt := range tests {
		for _, bf := range branchFactors {
			t.Run(tt.name+" bf "+fmt.Sprint(bf), func(t *testing.T) {
				require := require.New(t)
				keyA := tt.keyA(bf)
				keyB := tt.keyB(bf)

				require.Equal(tt.isPrefix, keyA.HasPrefix(keyB))
				require.Equal(tt.isPrefix, keyA.iteratedHasPrefix(0, keyB))
				require.Equal(tt.isStrictPrefix, keyA.HasStrictPrefix(keyB))
			})
		}
	}
}

func Test_Key_Skip(t *testing.T) {
	require := require.New(t)

	for _, bf := range branchFactors {
		empty := emptyKey(bf)
		require.Equal(ToKey([]byte{0}, bf).Skip(empty.tokensPerByte), empty)
		if bf == BranchFactor256 {
			continue
		}
		shortKey := ToKey([]byte{0b0101_0101}, bf)
		longKey := ToKey([]byte{0b0101_0101, 0b0101_0101}, bf)
		for i := 0; i < shortKey.tokensPerByte; i++ {
			shift := byte(i) * shortKey.tokenBitSize
			skipKey := shortKey.Skip(i)
			require.Equal(byte(0b0101_0101<<shift), skipKey.value[0])

			skipKey = longKey.Skip(i)
			require.Equal(byte(0b0101_0101<<shift+0b0101_0101>>(8-shift)), skipKey.value[0])
			require.Equal(byte(0b0101_0101<<shift), skipKey.value[1])
		}
	}

	skip := ToKey([]byte{0b0101_0101, 0b1010_1010}, BranchFactor256).Skip(1)
	require.Len(skip.value, 1)
	require.Equal(byte(0b1010_1010), skip.value[0])

	skip = ToKey([]byte{0b0101_0101, 0b1010_1010, 0b0101_0101}, BranchFactor256).Skip(1)
	require.Len(skip.value, 2)
	require.Equal(byte(0b1010_1010), skip.value[0])
	require.Equal(byte(0b0101_0101), skip.value[1])
}

func Test_Key_Take(t *testing.T) {
	require := require.New(t)

	for _, bf := range branchFactors {
		// take more tokens than the key has
		require.Equal(ToKey([]byte{0}, bf).Take(1000), ToKey([]byte{0}, bf))

		// take no tokens
		require.Equal(ToKey([]byte{0}, bf).Take(0), emptyKey(bf))
		if bf == BranchFactor256 {
			continue
		}
		key := ToKey([]byte{0b0101_0101}, bf)
		for i := 1; i <= key.tokensPerByte; i++ {
			shift := 8 - (byte(i) * key.tokenBitSize)
			take := key.Take(i)
			require.Equal(byte((0b0101_0101>>shift)<<shift), take.value[0])
		}
	}

	take := ToKey([]byte{0b0101_0101, 0b1010_1010}, BranchFactor256).Take(1)
	require.Len(take.value, 1)
	require.Equal(byte(0b0101_0101), take.value[0])
}

func Test_Key_Token(t *testing.T) {
	type test struct {
		name         string
		inputBytes   []byte
		branchFactor BranchFactor
		assertTokens func(*require.Assertions, Key)
	}

	tests := []test{
		{
			name:         "branch factor 2",
			inputBytes:   []byte{0b0_1_01_0_1_01, 0b1_0_1_0_1_0_1_0},
			branchFactor: BranchFactor2,
			assertTokens: func(require *require.Assertions, key Key) {
				require.Equal(byte(0), key.Token(0))
				require.Equal(byte(1), key.Token(1))
				require.Equal(byte(0), key.Token(2))
				require.Equal(byte(1), key.Token(3))
				require.Equal(byte(0), key.Token(4))
				require.Equal(byte(1), key.Token(5))
				require.Equal(byte(0), key.Token(6))
				require.Equal(byte(1), key.Token(7)) // end first byte
				require.Equal(byte(1), key.Token(8))
				require.Equal(byte(0), key.Token(9))
				require.Equal(byte(1), key.Token(10))
				require.Equal(byte(0), key.Token(11))
				require.Equal(byte(1), key.Token(12))
				require.Equal(byte(0), key.Token(13))
				require.Equal(byte(1), key.Token(14))
				require.Equal(byte(0), key.Token(15)) // end second byte
			},
		},
		{
			name:         "branch factor 4",
			inputBytes:   []byte{0b00_01_10_11, 0b11_10_01_00},
			branchFactor: BranchFactor4,
			assertTokens: func(require *require.Assertions, key Key) {
				require.Equal(byte(0), key.Token(0)) // 00
				require.Equal(byte(1), key.Token(1)) // 01
				require.Equal(byte(2), key.Token(2)) // 10
				require.Equal(byte(3), key.Token(3)) // 11 end first byte
				require.Equal(byte(3), key.Token(4)) // 11
				require.Equal(byte(2), key.Token(5)) // 10
				require.Equal(byte(1), key.Token(6)) // 01
				require.Equal(byte(0), key.Token(7)) // 00 end second byte
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
			branchFactor: BranchFactor16,
			assertTokens: func(require *require.Assertions, key Key) {
				for i := 0; i < 16; i++ {
					require.Equal(byte(i), key.Token(i))
				}
			},
		},
	}

	for i := 0; i < 256; i++ {
		i := i
		tests = append(tests, test{
			name:         fmt.Sprintf("branch factor 256, byte %d", i),
			inputBytes:   []byte{byte(i)},
			branchFactor: BranchFactor256,
			assertTokens: func(require *require.Assertions, key Key) {
				require.Equal(byte(i), key.Token(0))
			},
		})
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)
			key := ToKey(tt.inputBytes, tt.branchFactor)
			tt.assertTokens(require, key)
		})
	}
}

func Test_Key_Append(t *testing.T) {
	require := require.New(t)

	for _, bf := range branchFactors {
		key := ToKey([]byte{}, bf)
		for i := 0; i < int(key.branchFactor); i++ {
			appendedKey := key.Append(byte(i)).Append(byte(i / 2))
			require.Equal(byte(i), appendedKey.Token(0))
			require.Equal(byte(i/2), appendedKey.Token(1))
		}
	}
}

func Test_Key_AppendExtend(t *testing.T) {
	require := require.New(t)

	key2 := ToKey([]byte{0b1000_0000}, BranchFactor2).Take(1)
	p := ToKey([]byte{0b01010101}, BranchFactor2)
	extendedP := key2.AppendExtend(0, p)
	require.Equal([]byte{0b10010101, 0b01000_000}, extendedP.Bytes())
	require.Equal(byte(1), extendedP.Token(0))
	require.Equal(byte(0), extendedP.Token(1))
	require.Equal(byte(0), extendedP.Token(2))
	require.Equal(byte(1), extendedP.Token(3))
	require.Equal(byte(0), extendedP.Token(4))
	require.Equal(byte(1), extendedP.Token(5))
	require.Equal(byte(0), extendedP.Token(6))
	require.Equal(byte(1), extendedP.Token(7))
	require.Equal(byte(0), extendedP.Token(8))
	require.Equal(byte(1), extendedP.Token(9))

	p = ToKey([]byte{0b0101_0101, 0b1000_0000}, BranchFactor2).Take(9)
	extendedP = key2.AppendExtend(0, p)
	require.Equal([]byte{0b1001_0101, 0b0110_0000}, extendedP.Bytes())
	require.Equal(byte(1), extendedP.Token(0))
	require.Equal(byte(0), extendedP.Token(1))
	require.Equal(byte(0), extendedP.Token(2))
	require.Equal(byte(1), extendedP.Token(3))
	require.Equal(byte(0), extendedP.Token(4))
	require.Equal(byte(1), extendedP.Token(5))
	require.Equal(byte(0), extendedP.Token(6))
	require.Equal(byte(1), extendedP.Token(7))
	require.Equal(byte(0), extendedP.Token(8))
	require.Equal(byte(1), extendedP.Token(9))
	require.Equal(byte(1), extendedP.Token(10))

	key4 := ToKey([]byte{0b0100_0000}, BranchFactor4).Take(1)
	p = ToKey([]byte{0b0101_0101}, BranchFactor4)
	extendedP = key4.AppendExtend(0, p)
	require.Equal([]byte{0b0100_0101, 0b0101_0000}, extendedP.Bytes())
	require.Equal(byte(1), extendedP.Token(0))
	require.Equal(byte(0), extendedP.Token(1))
	require.Equal(byte(1), extendedP.Token(2))
	require.Equal(byte(1), extendedP.Token(3))
	require.Equal(byte(1), extendedP.Token(4))
	require.Equal(byte(1), extendedP.Token(5))

	key16 := ToKey([]byte{0b0001_0000}, BranchFactor16).Take(1)
	p = ToKey([]byte{0b0001_0001}, BranchFactor16)
	extendedP = key16.AppendExtend(0, p)
	require.Equal([]byte{0b0001_0000, 0b0001_0001}, extendedP.Bytes())
	require.Equal(byte(1), extendedP.Token(0))
	require.Equal(byte(0), extendedP.Token(1))
	require.Equal(byte(1), extendedP.Token(2))
	require.Equal(byte(1), extendedP.Token(3))

	p = ToKey([]byte{0b0001_0001, 0b0001_0001}, BranchFactor16)
	extendedP = key16.AppendExtend(0, p)
	require.Equal([]byte{0b0001_0000, 0b0001_0001, 0b0001_0001}, extendedP.Bytes())
	require.Equal(byte(1), extendedP.Token(0))
	require.Equal(byte(0), extendedP.Token(1))
	require.Equal(byte(1), extendedP.Token(2))
	require.Equal(byte(1), extendedP.Token(3))
	require.Equal(byte(1), extendedP.Token(4))
	require.Equal(byte(1), extendedP.Token(5))

	key256 := ToKey([]byte{0b0000_0001}, BranchFactor256)
	p = ToKey([]byte{0b0000_0001}, BranchFactor256)
	extendedP = key256.AppendExtend(0, p)
	require.Equal([]byte{0b0000_0001, 0b0000_0000, 0b0000_0001}, extendedP.Bytes())
	require.Equal(byte(1), extendedP.Token(0))
	require.Equal(byte(0), extendedP.Token(1))
	require.Equal(byte(1), extendedP.Token(2))
}

func TestKeyBytesNeeded(t *testing.T) {
	type test struct {
		BranchFactor
		tokenLength int
		bytesNeeded int
	}

	tests := []test{
		{
			BranchFactor: BranchFactor2,
			tokenLength:  7,
			bytesNeeded:  1,
		},
		{
			BranchFactor: BranchFactor2,
			tokenLength:  8,
			bytesNeeded:  1,
		},
		{
			BranchFactor: BranchFactor2,
			tokenLength:  9,
			bytesNeeded:  2,
		},
		{
			BranchFactor: BranchFactor2,
			tokenLength:  16,
			bytesNeeded:  2,
		},
		{
			BranchFactor: BranchFactor2,
			tokenLength:  17,
			bytesNeeded:  3,
		},
		{
			BranchFactor: BranchFactor4,
			tokenLength:  3,
			bytesNeeded:  1,
		},
		{
			BranchFactor: BranchFactor4,
			tokenLength:  4,
			bytesNeeded:  1,
		},
		{
			BranchFactor: BranchFactor4,
			tokenLength:  5,
			bytesNeeded:  2,
		},
		{
			BranchFactor: BranchFactor4,
			tokenLength:  8,
			bytesNeeded:  2,
		},
		{
			BranchFactor: BranchFactor4,
			tokenLength:  9,
			bytesNeeded:  3,
		},
		{
			BranchFactor: BranchFactor16,
			tokenLength:  2,
			bytesNeeded:  1,
		},
		{
			BranchFactor: BranchFactor16,
			tokenLength:  3,
			bytesNeeded:  2,
		},
		{
			BranchFactor: BranchFactor16,
			tokenLength:  4,
			bytesNeeded:  2,
		},
		{
			BranchFactor: BranchFactor16,
			tokenLength:  5,
			bytesNeeded:  3,
		},
		{
			BranchFactor: BranchFactor256,
			tokenLength:  2,
			bytesNeeded:  2,
		},
		{
			BranchFactor: BranchFactor256,
			tokenLength:  3,
			bytesNeeded:  3,
		},
		{
			BranchFactor: BranchFactor256,
			tokenLength:  4,
			bytesNeeded:  4,
		},
	}

	for _, branchFactor := range branchFactors {
		tests = append(tests, test{
			BranchFactor: branchFactor,
			tokenLength:  0,
			bytesNeeded:  0,
		})
		tests = append(tests, test{
			BranchFactor: branchFactor,
			tokenLength:  1,
			bytesNeeded:  1,
		})
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("branch factor %d, tokens length %d", tt.BranchFactor, tt.tokenLength), func(t *testing.T) {
			require := require.New(t)
			key := emptyKey(tt.BranchFactor)
			require.Equal(tt.bytesNeeded, key.bytesNeeded(tt.tokenLength))
		})
	}
}

func FuzzKeyAppendExtend(f *testing.F) {
	f.Fuzz(func(
		t *testing.T,
		first []byte,
		second []byte,
		token byte,
		forceFirstOdd bool,
		forceSecondOdd bool,
	) {
		require := require.New(t)
		for _, branchFactor := range branchFactors {
			key1 := ToKey(first, branchFactor)
			if forceFirstOdd && key1.tokenLength > 0 {
				key1 = key1.Take(key1.tokenLength - 1)
			}
			key2 := ToKey(second, branchFactor)
			if forceSecondOdd && key2.tokenLength > 0 {
				key2 = key2.Take(key2.tokenLength - 1)
			}
			token = byte(int(token) % int(branchFactor))
			extendedP := key1.AppendExtend(token, key2)
			require.Equal(key1.tokenLength+key2.tokenLength+1, extendedP.tokenLength)
			for i := 0; i < key1.tokenLength; i++ {
				require.Equal(key1.Token(i), extendedP.Token(i))
			}
			require.Equal(token, extendedP.Token(key1.tokenLength))
			for i := 0; i < key2.tokenLength; i++ {
				require.Equal(key2.Token(i), extendedP.Token(i+1+key1.tokenLength))
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
		for _, branchFactor := range branchFactors {
			key1 := ToKey(first, branchFactor)
			if int(tokensToSkip) >= key1.tokenLength {
				t.SkipNow()
			}
			key2 := key1.Skip(int(tokensToSkip))
			require.Equal(key1.tokenLength-int(tokensToSkip), key2.tokenLength)
			for i := 0; i < key2.tokenLength; i++ {
				require.Equal(key1.Token(int(tokensToSkip)+i), key2.Token(i))
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
		for _, branchFactor := range branchFactors {
			key1 := ToKey(first, branchFactor)
			if int(tokensToTake) >= key1.tokenLength {
				t.SkipNow()
			}
			key2 := key1.Take(int(tokensToTake))
			require.Equal(int(tokensToTake), key2.tokenLength)

			for i := 0; i < key2.tokenLength; i++ {
				require.Equal(key1.Token(i), key2.Token(i))
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

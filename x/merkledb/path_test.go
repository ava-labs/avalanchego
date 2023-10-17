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

			path := emptyPath(branchFactor)
			require.False(path.hasPartialByte())

			if branchFactor == BranchFactor256 {
				// Tokens are an entire byte so
				// there is never a partial byte.
				path = path.Append(0)
				require.False(path.hasPartialByte())
				path = path.Append(0)
				require.False(path.hasPartialByte())
				return
			}

			// Fill all but the last token of the first byte.
			for i := 0; i < path.tokensPerByte-1; i++ {
				path = path.Append(0)
				require.True(path.hasPartialByte())
			}

			// Fill the last token of the first byte.
			path = path.Append(0)
			require.False(path.hasPartialByte())

			// Fill the first token of the second byte.
			path = path.Append(0)
			require.True(path.hasPartialByte())
		})
	}
}

func Test_Path_Has_Prefix(t *testing.T) {
	type test struct {
		name           string
		pathA          func(bf BranchFactor) Path
		pathB          func(bf BranchFactor) Path
		isStrictPrefix bool
		isPrefix       bool
	}

	key := "Key"
	keyLength := map[BranchFactor]int{}
	for _, branchFactor := range branchFactors {
		config := branchFactorToPathConfig[branchFactor]
		keyLength[branchFactor] = len(key) * config.tokensPerByte
	}

	tests := []test{
		{
			name:           "equal keys",
			pathA:          func(bf BranchFactor) Path { return NewPath([]byte(key), bf) },
			pathB:          func(bf BranchFactor) Path { return NewPath([]byte(key), bf) },
			isPrefix:       true,
			isStrictPrefix: false,
		},
		{
			name:           "one key has one fewer token",
			pathA:          func(bf BranchFactor) Path { return NewPath([]byte(key), bf) },
			pathB:          func(bf BranchFactor) Path { return NewPath([]byte(key), bf).Take(keyLength[bf] - 1) },
			isPrefix:       true,
			isStrictPrefix: true,
		},
		{
			name:           "equal keys, both have one fewer token",
			pathA:          func(bf BranchFactor) Path { return NewPath([]byte(key), bf).Take(keyLength[bf] - 1) },
			pathB:          func(bf BranchFactor) Path { return NewPath([]byte(key), bf).Take(keyLength[bf] - 1) },
			isPrefix:       true,
			isStrictPrefix: false,
		},
		{
			name:           "different keys",
			pathA:          func(bf BranchFactor) Path { return NewPath([]byte{0xF7}, bf) },
			pathB:          func(bf BranchFactor) Path { return NewPath([]byte{0xF0}, bf) },
			isPrefix:       false,
			isStrictPrefix: false,
		},
		{
			name:           "same bytes, different lengths",
			pathA:          func(bf BranchFactor) Path { return NewPath([]byte{0x10, 0x00}, bf).Take(1) },
			pathB:          func(bf BranchFactor) Path { return NewPath([]byte{0x10, 0x00}, bf).Take(2) },
			isPrefix:       false,
			isStrictPrefix: false,
		},
	}

	for _, tt := range tests {
		for _, bf := range branchFactors {
			t.Run(tt.name+" bf "+fmt.Sprint(bf), func(t *testing.T) {
				require := require.New(t)
				pathA := tt.pathA(bf)
				pathB := tt.pathB(bf)

				require.Equal(tt.isPrefix, pathA.HasPrefix(pathB))
				require.Equal(tt.isPrefix, pathA.iteratedHasPrefix(0, pathB))
				require.Equal(tt.isStrictPrefix, pathA.HasStrictPrefix(pathB))
			})
		}
	}
}

func Test_Path_Skip(t *testing.T) {
	require := require.New(t)

	for _, bf := range branchFactors {
		empty := emptyPath(bf)
		require.Equal(NewPath([]byte{0}, bf).Skip(empty.tokensPerByte), empty)
		if bf == BranchFactor256 {
			continue
		}
		shortPath := NewPath([]byte{0b0101_0101}, bf)
		longPath := NewPath([]byte{0b0101_0101, 0b0101_0101}, bf)
		for i := 0; i < shortPath.tokensPerByte; i++ {
			shift := byte(i) * shortPath.tokenBitSize
			skipPath := shortPath.Skip(i)
			require.Equal(byte(0b0101_0101<<shift), skipPath.value[0])

			skipPath = longPath.Skip(i)
			require.Equal(byte(0b0101_0101<<shift+0b0101_0101>>(8-shift)), skipPath.value[0])
			require.Equal(byte(0b0101_0101<<shift), skipPath.value[1])
		}
	}

	skip := NewPath([]byte{0b0101_0101, 0b1010_1010}, BranchFactor256).Skip(1)
	require.Len(skip.value, 1)
	require.Equal(byte(0b1010_1010), skip.value[0])

	skip = NewPath([]byte{0b0101_0101, 0b1010_1010, 0b0101_0101}, BranchFactor256).Skip(1)
	require.Len(skip.value, 2)
	require.Equal(byte(0b1010_1010), skip.value[0])
	require.Equal(byte(0b0101_0101), skip.value[1])
}

func Test_Path_Take(t *testing.T) {
	require := require.New(t)

	for _, bf := range branchFactors {
		require.Equal(NewPath([]byte{0}, bf).Take(0), emptyPath(bf))
		if bf == BranchFactor256 {
			continue
		}
		path := NewPath([]byte{0b0101_0101}, bf)
		for i := 1; i <= path.tokensPerByte; i++ {
			shift := 8 - (byte(i) * path.tokenBitSize)
			take := path.Take(i)
			require.Equal(byte((0b0101_0101>>shift)<<shift), take.value[0])
		}
	}

	take := NewPath([]byte{0b0101_0101, 0b1010_1010}, BranchFactor256).Take(1)
	require.Len(take.value, 1)
	require.Equal(byte(0b0101_0101), take.value[0])
}

func Test_Path_Token(t *testing.T) {
	type test struct {
		name         string
		inputBytes   []byte
		branchFactor BranchFactor
		assertTokens func(*require.Assertions, Path)
	}

	tests := []test{
		{
			name:         "branch factor 2",
			inputBytes:   []byte{0b0_1_01_0_1_01, 0b1_0_1_0_1_0_1_0},
			branchFactor: BranchFactor2,
			assertTokens: func(require *require.Assertions, path Path) {
				require.Equal(byte(0), path.Token(0))
				require.Equal(byte(1), path.Token(1))
				require.Equal(byte(0), path.Token(2))
				require.Equal(byte(1), path.Token(3))
				require.Equal(byte(0), path.Token(4))
				require.Equal(byte(1), path.Token(5))
				require.Equal(byte(0), path.Token(6))
				require.Equal(byte(1), path.Token(7)) // end first byte
				require.Equal(byte(1), path.Token(8))
				require.Equal(byte(0), path.Token(9))
				require.Equal(byte(1), path.Token(10))
				require.Equal(byte(0), path.Token(11))
				require.Equal(byte(1), path.Token(12))
				require.Equal(byte(0), path.Token(13))
				require.Equal(byte(1), path.Token(14))
				require.Equal(byte(0), path.Token(15)) // end second byte
			},
		},
		{
			name:         "branch factor 4",
			inputBytes:   []byte{0b00_01_10_11, 0b11_10_01_00},
			branchFactor: BranchFactor4,
			assertTokens: func(require *require.Assertions, path Path) {
				require.Equal(byte(0), path.Token(0)) // 00
				require.Equal(byte(1), path.Token(1)) // 01
				require.Equal(byte(2), path.Token(2)) // 10
				require.Equal(byte(3), path.Token(3)) // 11 end first byte
				require.Equal(byte(3), path.Token(4)) // 11
				require.Equal(byte(2), path.Token(5)) // 10
				require.Equal(byte(1), path.Token(6)) // 01
				require.Equal(byte(0), path.Token(7)) // 00 end second byte
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
			assertTokens: func(require *require.Assertions, path Path) {
				for i := 0; i < 16; i++ {
					require.Equal(byte(i), path.Token(i))
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
			assertTokens: func(require *require.Assertions, path Path) {
				require.Equal(byte(i), path.Token(0))
			},
		})
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)
			path := NewPath(tt.inputBytes, tt.branchFactor)
			tt.assertTokens(require, path)
		})
	}
}

func Test_Path_Append(t *testing.T) {
	require := require.New(t)

	for _, bf := range branchFactors {
		path := NewPath([]byte{}, bf)
		for i := 0; i < int(path.branchFactor); i++ {
			appendedPath := path.Append(byte(i)).Append(byte(i / 2))
			require.Equal(byte(i), appendedPath.Token(0))
			require.Equal(byte(i/2), appendedPath.Token(1))
		}
	}
}

func Test_Path_AppendExtend(t *testing.T) {
	require := require.New(t)

	path2 := NewPath([]byte{0b1000_0000}, BranchFactor2).Take(1)
	p := NewPath([]byte{0b01010101}, BranchFactor2)
	extendedP := path2.AppendExtend(0, p)
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

	p = NewPath([]byte{0b0101_0101, 0b1000_0000}, BranchFactor2).Take(9)
	extendedP = path2.AppendExtend(0, p)
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

	path4 := NewPath([]byte{0b0100_0000}, BranchFactor4).Take(1)
	p = NewPath([]byte{0b0101_0101}, BranchFactor4)
	extendedP = path4.AppendExtend(0, p)
	require.Equal([]byte{0b0100_0101, 0b0101_0000}, extendedP.Bytes())
	require.Equal(byte(1), extendedP.Token(0))
	require.Equal(byte(0), extendedP.Token(1))
	require.Equal(byte(1), extendedP.Token(2))
	require.Equal(byte(1), extendedP.Token(3))
	require.Equal(byte(1), extendedP.Token(4))
	require.Equal(byte(1), extendedP.Token(5))

	path16 := NewPath([]byte{0b0001_0000}, BranchFactor16).Take(1)
	p = NewPath([]byte{0b0001_0001}, BranchFactor16)
	extendedP = path16.AppendExtend(0, p)
	require.Equal([]byte{0b0001_0000, 0b0001_0001}, extendedP.Bytes())
	require.Equal(byte(1), extendedP.Token(0))
	require.Equal(byte(0), extendedP.Token(1))
	require.Equal(byte(1), extendedP.Token(2))
	require.Equal(byte(1), extendedP.Token(3))

	p = NewPath([]byte{0b0001_0001, 0b0001_0001}, BranchFactor16)
	extendedP = path16.AppendExtend(0, p)
	require.Equal([]byte{0b0001_0000, 0b0001_0001, 0b0001_0001}, extendedP.Bytes())
	require.Equal(byte(1), extendedP.Token(0))
	require.Equal(byte(0), extendedP.Token(1))
	require.Equal(byte(1), extendedP.Token(2))
	require.Equal(byte(1), extendedP.Token(3))
	require.Equal(byte(1), extendedP.Token(4))
	require.Equal(byte(1), extendedP.Token(5))

	path256 := NewPath([]byte{0b0000_0001}, BranchFactor256)
	p = NewPath([]byte{0b0000_0001}, BranchFactor256)
	extendedP = path256.AppendExtend(0, p)
	require.Equal([]byte{0b0000_0001, 0b0000_0000, 0b0000_0001}, extendedP.Bytes())
	require.Equal(byte(1), extendedP.Token(0))
	require.Equal(byte(0), extendedP.Token(1))
	require.Equal(byte(1), extendedP.Token(2))
}

func TestPathBytesNeeded(t *testing.T) {
	type test struct {
		BranchFactor
		tokensLength int
		bytesNeeded  int
	}

	tests := []test{
		{
			BranchFactor: BranchFactor2,
			tokensLength: 7,
			bytesNeeded:  1,
		},
		{
			BranchFactor: BranchFactor2,
			tokensLength: 8,
			bytesNeeded:  1,
		},
		{
			BranchFactor: BranchFactor2,
			tokensLength: 9,
			bytesNeeded:  2,
		},
		{
			BranchFactor: BranchFactor2,
			tokensLength: 16,
			bytesNeeded:  2,
		},
		{
			BranchFactor: BranchFactor2,
			tokensLength: 17,
			bytesNeeded:  3,
		},
		{
			BranchFactor: BranchFactor4,
			tokensLength: 3,
			bytesNeeded:  1,
		},
		{
			BranchFactor: BranchFactor4,
			tokensLength: 4,
			bytesNeeded:  1,
		},
		{
			BranchFactor: BranchFactor4,
			tokensLength: 5,
			bytesNeeded:  2,
		},
		{
			BranchFactor: BranchFactor4,
			tokensLength: 8,
			bytesNeeded:  2,
		},
		{
			BranchFactor: BranchFactor4,
			tokensLength: 9,
			bytesNeeded:  3,
		},
		{
			BranchFactor: BranchFactor16,
			tokensLength: 2,
			bytesNeeded:  1,
		},
		{
			BranchFactor: BranchFactor16,
			tokensLength: 3,
			bytesNeeded:  2,
		},
		{
			BranchFactor: BranchFactor16,
			tokensLength: 4,
			bytesNeeded:  2,
		},
		{
			BranchFactor: BranchFactor16,
			tokensLength: 5,
			bytesNeeded:  3,
		},
		{
			BranchFactor: BranchFactor256,
			tokensLength: 2,
			bytesNeeded:  2,
		},
		{
			BranchFactor: BranchFactor256,
			tokensLength: 3,
			bytesNeeded:  3,
		},
		{
			BranchFactor: BranchFactor256,
			tokensLength: 4,
			bytesNeeded:  4,
		},
	}

	for _, branchFactor := range branchFactors {
		tests = append(tests, test{
			BranchFactor: branchFactor,
			tokensLength: 0,
			bytesNeeded:  0,
		})
		tests = append(tests, test{
			BranchFactor: branchFactor,
			tokensLength: 1,
			bytesNeeded:  1,
		})
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("branch factor %d, tokens length %d", tt.BranchFactor, tt.tokensLength), func(t *testing.T) {
			require := require.New(t)
			path := emptyPath(tt.BranchFactor)
			require.Equal(tt.bytesNeeded, path.bytesNeeded(tt.tokensLength))
		})
	}
}

func FuzzPathAppendExtend(f *testing.F) {
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
			path1 := NewPath(first, branchFactor)
			if forceFirstOdd && path1.tokensLength > 0 {
				path1 = path1.Take(path1.tokensLength - 1)
			}
			path2 := NewPath(second, branchFactor)
			if forceSecondOdd && path2.tokensLength > 0 {
				path2 = path2.Take(path2.tokensLength - 1)
			}
			token = byte(int(token) % int(branchFactor))
			extendedP := path1.AppendExtend(token, path2)
			require.Equal(path1.tokensLength+path2.tokensLength+1, extendedP.tokensLength)
			for i := 0; i < path1.tokensLength; i++ {
				require.Equal(path1.Token(i), extendedP.Token(i))
			}
			require.Equal(token, extendedP.Token(path1.tokensLength))
			for i := 0; i < path2.tokensLength; i++ {
				require.Equal(path2.Token(i), extendedP.Token(i+1+path1.tokensLength))
			}
		}
	})
}

func FuzzPathSkip(f *testing.F) {
	f.Fuzz(func(
		t *testing.T,
		first []byte,
		tokensToSkip uint,
	) {
		require := require.New(t)
		for _, branchFactor := range branchFactors {
			path1 := NewPath(first, branchFactor)
			if int(tokensToSkip) >= path1.tokensLength {
				t.SkipNow()
			}
			path2 := path1.Skip(int(tokensToSkip))
			require.Equal(path1.tokensLength-int(tokensToSkip), path2.tokensLength)
			for i := 0; i < path2.tokensLength; i++ {
				require.Equal(path1.Token(int(tokensToSkip)+i), path2.Token(i))
			}
		}
	})
}

func FuzzPathTake(f *testing.F) {
	f.Fuzz(func(
		t *testing.T,
		first []byte,
		tokensToTake uint,
	) {
		require := require.New(t)
		for _, branchFactor := range branchFactors {
			path1 := NewPath(first, branchFactor)
			if int(tokensToTake) >= path1.tokensLength {
				t.SkipNow()
			}
			path2 := path1.Take(int(tokensToTake))
			require.Equal(int(tokensToTake), path2.tokensLength)

			for i := 0; i < path2.tokensLength; i++ {
				require.Equal(path1.Token(i), path2.Token(i))
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

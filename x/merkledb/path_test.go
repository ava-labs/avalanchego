// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package merkledb

import (
	"testing"

	"github.com/stretchr/testify/require"
)

var branchFactors = []BranchFactor{
	BranchFactor2,
	BranchFactor4,
	BranchFactor16,
	BranchFactor256,
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
	keyLength := map[BranchFactor]int{
		BranchFactor2:   24,
		BranchFactor4:   12,
		BranchFactor16:  6,
		BranchFactor256: 3,
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
			name:           "one keys has one less token",
			pathA:          func(bf BranchFactor) Path { return NewPath([]byte(key), bf) },
			pathB:          func(bf BranchFactor) Path { return NewPath([]byte(key), bf).Take(keyLength[bf] - 1) },
			isPrefix:       true,
			isStrictPrefix: true,
		},
		{
			name:           "equal keys, both have one less token",
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
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)
			for _, bf := range branchFactors {
				require.Equal(tt.isPrefix, tt.pathA(bf).HasPrefix(tt.pathB(bf)))
				require.Equal(tt.isStrictPrefix, tt.pathA(bf).HasStrictPrefix(tt.pathB(bf)))
			}
		})
	}
}

func Test_Path_HasPrefix_BadInput(t *testing.T) {
	require := require.New(t)

	for _, bf := range branchFactors {
		a := Path{pathConfig: branchFactorToPathConfig[bf]}
		b := Path{tokensLength: 1, pathConfig: branchFactorToPathConfig[bf]}
		require.False(a.HasPrefix(b))

		a = Path{tokensLength: 10, pathConfig: branchFactorToPathConfig[bf]}
		b = Path{value: string([]byte{0x10}), tokensLength: 1, pathConfig: branchFactorToPathConfig[bf]}
		require.False(a.HasPrefix(b))
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
	require := require.New(t)

	path2 := NewPath([]byte{0b0101_0101}, BranchFactor2)
	require.Equal(byte(0), path2.Token(0))
	require.Equal(byte(1), path2.Token(1))
	require.Equal(byte(0), path2.Token(2))
	require.Equal(byte(1), path2.Token(3))
	require.Equal(byte(0), path2.Token(4))
	require.Equal(byte(1), path2.Token(5))
	require.Equal(byte(0), path2.Token(6))
	require.Equal(byte(1), path2.Token(7))

	path4 := NewPath([]byte{0b0110_0110}, BranchFactor4)
	require.Equal(byte(1), path4.Token(0))
	require.Equal(byte(2), path4.Token(1))
	require.Equal(byte(1), path4.Token(2))
	require.Equal(byte(2), path4.Token(3))

	path16 := NewPath([]byte{0x12}, BranchFactor16)
	require.Equal(byte(1), path16.Token(0))
	require.Equal(byte(2), path16.Token(1))

	path256 := NewPath([]byte{0x12}, BranchFactor256)
	require.Equal(byte(0x12), path256.Token(0))
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

func Test_Path_Extend(t *testing.T) {
	require := require.New(t)

	path2 := NewPath([]byte{0b1000_0000}, BranchFactor2).Take(1)
	p := NewPath([]byte{0b01010101}, BranchFactor2)
	extendedP := path2.Extend(p)
	require.Equal([]byte{0b10101010, 0b1000_0000}, extendedP.Bytes())
	require.Equal(byte(1), extendedP.Token(0))
	require.Equal(byte(0), extendedP.Token(1))
	require.Equal(byte(1), extendedP.Token(2))
	require.Equal(byte(0), extendedP.Token(3))
	require.Equal(byte(1), extendedP.Token(4))
	require.Equal(byte(0), extendedP.Token(5))
	require.Equal(byte(1), extendedP.Token(6))
	require.Equal(byte(0), extendedP.Token(7))
	require.Equal(byte(1), extendedP.Token(8))

	p = NewPath([]byte{0b01010101, 0b1000_0000}, BranchFactor2).Take(9)
	extendedP = path2.Extend(p)
	require.Equal([]byte{0b10101010, 0b1100_0000}, extendedP.Bytes())
	require.Equal(byte(1), extendedP.Token(0))
	require.Equal(byte(0), extendedP.Token(1))
	require.Equal(byte(1), extendedP.Token(2))
	require.Equal(byte(0), extendedP.Token(3))
	require.Equal(byte(1), extendedP.Token(4))
	require.Equal(byte(0), extendedP.Token(5))
	require.Equal(byte(1), extendedP.Token(6))
	require.Equal(byte(0), extendedP.Token(7))
	require.Equal(byte(1), extendedP.Token(8))
	require.Equal(byte(1), extendedP.Token(9))

	path4 := NewPath([]byte{0b0100_0000}, BranchFactor4).Take(1)
	p = NewPath([]byte{0b0101_0101}, BranchFactor4)
	extendedP = path4.Extend(p)
	require.Equal([]byte{0b0101_0101, 0b0100_0000}, extendedP.Bytes())
	require.Equal(byte(1), extendedP.Token(0))
	require.Equal(byte(1), extendedP.Token(1))
	require.Equal(byte(1), extendedP.Token(2))
	require.Equal(byte(1), extendedP.Token(3))
	require.Equal(byte(1), extendedP.Token(4))

	path16 := NewPath([]byte{0b0001_0000}, BranchFactor16).Take(1)
	p = NewPath([]byte{0b0001_0001}, BranchFactor16)
	extendedP = path16.Extend(p)
	require.Equal([]byte{0b0001_0001, 0b0001_0000}, extendedP.Bytes())
	require.Equal(byte(1), extendedP.Token(0))
	require.Equal(byte(1), extendedP.Token(1))
	require.Equal(byte(1), extendedP.Token(2))

	p = NewPath([]byte{0b0001_0001, 0b0001_0001}, BranchFactor16)
	extendedP = path16.Extend(p)
	require.Equal([]byte{0b0001_0001, 0b0001_0001, 0b0001_0000}, extendedP.Bytes())
	require.Equal(byte(1), extendedP.Token(0))
	require.Equal(byte(1), extendedP.Token(1))
	require.Equal(byte(1), extendedP.Token(2))
	require.Equal(byte(1), extendedP.Token(3))
	require.Equal(byte(1), extendedP.Token(4))

	path256 := NewPath([]byte{0b0000_0001}, BranchFactor256)
	p = NewPath([]byte{0b0000_0001}, BranchFactor256)
	extendedP = path256.Extend(p)
	require.Equal([]byte{0b0000_0001, 0b0000_0001}, extendedP.Bytes())
	require.Equal(byte(1), extendedP.Token(0))
	require.Equal(byte(1), extendedP.Token(1))
}

func FuzzPathExtend(f *testing.F) {
	f.Fuzz(func(
		t *testing.T,
		first []byte,
		second []byte,
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
			extendedP := path1.Extend(path2)
			require.Equal(path1.tokensLength+path2.tokensLength, extendedP.tokensLength)
			for i := 0; i < path1.tokensLength; i++ {
				require.Equal(path1.Token(i), extendedP.Token(i))
			}
			for i := 0; i < path2.tokensLength; i++ {
				require.Equal(path2.Token(i), extendedP.Token(i+path1.tokensLength))
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

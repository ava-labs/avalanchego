// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package merkledb

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func Test_Path_Skip(t *testing.T) {
	require := require.New(t)

	for i := 0; i < 8; i++ {
		skip := NewPath([]byte{0b0101_0101}, BranchFactor2).Skip(i)
		require.Equal(byte(0b0101_0101<<i), skip.value[0])
	}
	for i := 0; i < 4; i++ {
		skip := NewPath([]byte{0b0101_0101}, BranchFactor4).Skip(i)
		require.Equal(byte(0b0101_0101<<(i*2)), skip.value[0])
	}
	for i := 0; i < 2; i++ {
		skip := NewPath([]byte{0b0101_0101}, BranchFactor16).Skip(i)
		require.Equal(byte(0b0101_0101<<(i*4)), skip.value[0])
	}
	skip := NewPath([]byte{0b0101_0101, 0b1010_1010}, BranchFactor256).Skip(1)
	require.Len(skip.value, 1)
	require.Equal(byte(0b1010_1010), skip.value[0])

	for i := 0; i < 8; i++ {
		skip := NewPath([]byte{0b0101_0101, 0b0101_0101}, BranchFactor2).Skip(i)
		shift := i
		require.Equal(byte(0b0101_0101<<shift+0b0101_0101>>(8-shift)), skip.value[0])
		require.Equal(byte(0b0101_0101<<shift), skip.value[1])
	}
	for i := 0; i < 4; i++ {
		skip := NewPath([]byte{0b0101_0101, 0b0101_0101}, BranchFactor4).Skip(i)
		shift := i * 2
		require.Equal(byte(0b0101_0101<<shift+0b0101_0101>>(8-shift)), skip.value[0])
		require.Equal(byte(0b0101_0101<<shift), skip.value[1])
	}
	for i := 0; i < 2; i++ {
		shift := i * 4
		skip := NewPath([]byte{0b0101_0101, 0b0101_0101}, BranchFactor16).Skip(i)
		require.Equal(byte(0b0101_0101<<shift+0b0101_0101>>(8-shift)), skip.value[0])
		require.Equal(byte(0b0101_0101<<shift), skip.value[1])
	}

	skip = NewPath([]byte{0b0101_0101, 0b1010_1010, 0b0101_0101}, BranchFactor256).Skip(1)
	require.Len(skip.value, 2)
	require.Equal(byte(0b1010_1010), skip.value[0])
	require.Equal(byte(0b0101_0101), skip.value[1])
}

func Test_Path_Take(t *testing.T) {
	require := require.New(t)
	for i := 1; i <= 8; i++ {
		shift := 8 - i
		take := NewPath([]byte{0b0101_0101}, BranchFactor2).Take(i)
		require.Equal(byte((0b0101_0101>>shift)<<shift), take.value[0])
	}
	for i := 1; i <= 4; i++ {
		shift := 8 - (i * 2)
		take := NewPath([]byte{0b0101_0101}, BranchFactor4).Take(i)
		require.Equal(byte((0b0101_0101>>shift)<<shift), take.value[0])
	}
	for i := 1; i <= 2; i++ {
		shift := 8 - (i * 4)
		take := NewPath([]byte{0b0101_0101}, BranchFactor16).Take(i)
		require.Equal(byte((0b0101_0101>>shift)<<shift), take.value[0])
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

	path2 := NewPath([]byte{}, BranchFactor2)
	for i := 0; i < 2; i++ {
		require.Equal(byte(i), path2.Append(byte(i)).Token(0))
		require.Equal(byte(i/2), path2.Append(byte(i)).Append(byte(i/2)).Token(1))
	}

	path4 := NewPath([]byte{}, BranchFactor4)
	for i := 0; i < 4; i++ {
		require.Equal(byte(i), path4.Append(byte(i)).Token(0))
		require.Equal(byte(i/2), path4.Append(byte(i)).Append(byte(i/2)).Token(1))
	}

	path16 := NewPath([]byte{}, BranchFactor16)
	for i := 0; i < 16; i++ {
		require.Equal(byte(i), path16.Append(byte(i)).Token(0))
		require.Equal(byte(i/2), path16.Append(byte(i)).Append(byte(i/2)).Token(1))
	}

	path256 := NewPath([]byte{}, BranchFactor256)
	for i := 0; i < 256; i++ {
		require.Equal(byte(i), path256.Append(byte(i)).Token(0))
		require.Equal(byte(i/2), path256.Append(byte(i)).Append(byte(i/2)).Token(1))
	}
}

func Test_Path_Extend(t *testing.T) {
	require := require.New(t)

	path2 := NewPath([]byte{0b1000_0000}, BranchFactor2).Take(1)
	p := NewPath([]byte{0b01010101}, BranchFactor2)
	extendedP := path2.Extend(p)
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
	p = NewPath([]byte{0b01010101}, BranchFactor4)
	extendedP = path4.Extend(p)
	require.Equal(byte(1), extendedP.Token(0))
	require.Equal(byte(1), extendedP.Token(1))
	require.Equal(byte(1), extendedP.Token(2))
	require.Equal(byte(1), extendedP.Token(3))
	require.Equal(byte(1), extendedP.Token(4))

	path16 := NewPath([]byte{0b0001_0000}, BranchFactor16).Take(1)
	p = NewPath([]byte{0b0001_0001}, BranchFactor16)
	extendedP = path16.Extend(p)
	require.Equal(byte(1), extendedP.Token(0))
	require.Equal(byte(1), extendedP.Token(1))
	require.Equal(byte(1), extendedP.Token(2))

	path256 := NewPath([]byte{0b0000_0001}, BranchFactor256)
	p = NewPath([]byte{0b0000_0001}, BranchFactor16)
	extendedP = path256.Extend(p)
	require.Equal(byte(1), extendedP.Token(0))
	require.Equal(byte(1), extendedP.Token(1))
}

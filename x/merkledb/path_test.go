// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package merkledb

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/require"
)

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

func Test_SerializedPath_NibbleVal(t *testing.T) {
	require := require.New(t)

	path := SerializedPath{Value: []byte{240, 237}}
	require.Equal(byte(15), path.NibbleVal(0))
	require.Equal(byte(0), path.NibbleVal(1))
	require.Equal(byte(14), path.NibbleVal(2))
	require.Equal(byte(13), path.NibbleVal(3))
}

func Test_SerializedPath_AppendNibble(t *testing.T) {
	require := require.New(t)

	path := SerializedPath{Value: []byte{}}
	require.Zero(path.NibbleLength)

	path = path.AppendNibble(1)
	require.Equal(1, path.NibbleLength)
	require.Equal(byte(1), path.NibbleVal(0))

	path = path.AppendNibble(2)
	require.Equal(2, path.NibbleLength)
	require.Equal(byte(2), path.NibbleVal(1))
}

func Test_SerializedPath_Has_Prefix(t *testing.T) {
	require := require.New(t)

	first := SerializedPath{Value: []byte("FirstKey")}
	prefix := SerializedPath{Value: []byte("FirstKe")}
	require.True(first.HasPrefix(prefix))
	require.True(first.HasStrictPrefix(prefix))

	first = SerializedPath{Value: []byte("FirstKey"), NibbleLength: 16}
	prefix = SerializedPath{Value: []byte("FirstKey"), NibbleLength: 15}
	require.True(first.HasPrefix(prefix))
	require.True(first.HasStrictPrefix(prefix))

	first = SerializedPath{Value: []byte("FirstKey"), NibbleLength: 15}
	prefix = SerializedPath{Value: []byte("FirstKey"), NibbleLength: 15}
	require.True(first.HasPrefix(prefix))
	require.False(first.HasStrictPrefix(prefix))

	first = SerializedPath{Value: []byte{247}, NibbleLength: 2}
	prefix = SerializedPath{Value: []byte{240}, NibbleLength: 2}
	require.False(first.HasPrefix(prefix))
	require.False(first.HasStrictPrefix(prefix))

	first = SerializedPath{Value: []byte{247}, NibbleLength: 2}
	prefix = SerializedPath{Value: []byte{240}, NibbleLength: 1}
	require.True(first.HasPrefix(prefix))
	require.True(first.HasStrictPrefix(prefix))

	first = SerializedPath{Value: []byte{}, NibbleLength: 0}
	prefix = SerializedPath{Value: []byte{}, NibbleLength: 0}
	require.True(first.HasPrefix(prefix))
	require.False(first.HasStrictPrefix(prefix))

	a := SerializedPath{Value: []byte{0x10}, NibbleLength: 1}
	b := SerializedPath{Value: []byte{0x10}, NibbleLength: 2}
	require.False(a.HasPrefix(b))
}

func Test_SerializedPath_HasPrefix_BadInput(t *testing.T) {
	require := require.New(t)

	a := SerializedPath{Value: []byte{}}
	b := SerializedPath{Value: []byte{}, NibbleLength: 1}
	require.False(a.HasPrefix(b))

	a = SerializedPath{Value: []byte{}, NibbleLength: 10}
	b = SerializedPath{Value: []byte{0x10}, NibbleLength: 1}
	require.False(a.HasPrefix(b))
}

func Test_SerializedPath_Equal(t *testing.T) {
	require := require.New(t)

	first := SerializedPath{Value: []byte("FirstKey"), NibbleLength: 16}
	prefix := SerializedPath{Value: []byte("FirstKey"), NibbleLength: 16}
	require.True(first.Equal(prefix))

	first = SerializedPath{Value: []byte("FirstKey"), NibbleLength: 16}
	prefix = SerializedPath{Value: []byte("FirstKey"), NibbleLength: 15}
	require.False(first.Equal(prefix))

	first = SerializedPath{Value: []byte("FirstKey"), NibbleLength: 15}
	prefix = SerializedPath{Value: []byte("FirstKey"), NibbleLength: 15}
	require.True(first.Equal(prefix))
}

func FuzzPath(f *testing.F) {
	for bit0 := 0; bit0 < 2; bit0++ {
		for bit1 := 0; bit1 < 2; bit1++ {
			f.Add([]byte{0x01, 0xF0, 0x0F, 0x00}, bit0 == 0, bit1 == 0)
			f.Add([]byte{}, bit0 == 0, bit1 == 0)
			f.Add([]byte(nil), bit0 == 0, bit1 == 0)
		}
	}
	f.Fuzz(
		func(
			t *testing.T,
			pathBytes []byte,
			branchingBit0 bool,
			branchingBit1 bool,
		) {
			var branching BranchFactor
			var tokensPerByte int
			switch {
			case branchingBit0 && branchingBit1:
				branching = BranchFactor256
				tokensPerByte = 1
			case branchingBit0 && !branchingBit1:
				branching = BranchFactor16
				tokensPerByte = 2
			case !branchingBit0 && branchingBit1:
				branching = BranchFactor4
				tokensPerByte = 4
			case !branchingBit0 && !branchingBit1:
				branching = BranchFactor2
				tokensPerByte = 8
			}

			require := require.New(t)

			p := NewPath(pathBytes, branching)

			serializedPath := p.Serialize()
			require.True(bytes.Equal(pathBytes, serializedPath.Value))
			require.Equal(tokensPerByte*len(pathBytes), serializedPath.NibbleLength)

			deserializedPath := serializedPath.Deserialize(branching)
			require.Equal(p, deserializedPath)

			reserializedPath := deserializedPath.Serialize()
			require.Equal(serializedPath, reserializedPath)
		})
}

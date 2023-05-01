// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package merkledb

import (
	"testing"

	"github.com/stretchr/testify/require"
)

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
	require.Equal(0, path.NibbleLength)

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

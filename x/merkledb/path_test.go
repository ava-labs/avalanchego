// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package merkledb

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func Test_SerializedPath_NibbleVal(t *testing.T) {
	path := SerializedPath{Value: []byte{240, 237}}
	require.Equal(t, byte(15), path.NibbleVal(0))
	require.Equal(t, byte(0), path.NibbleVal(1))
	require.Equal(t, byte(14), path.NibbleVal(2))
	require.Equal(t, byte(13), path.NibbleVal(3))
}

func Test_SerializedPath_AppendNibble(t *testing.T) {
	path := SerializedPath{Value: []byte{}}
	require.Zero(t, path.NibbleLength)

	path = path.AppendNibble(1)
	require.Equal(t, 1, path.NibbleLength)
	require.Equal(t, byte(1), path.NibbleVal(0))

	path = path.AppendNibble(2)
	require.Equal(t, 2, path.NibbleLength)
	require.Equal(t, byte(2), path.NibbleVal(1))
}

func Test_SerializedPath_Has_Prefix(t *testing.T) {
	first := SerializedPath{Value: []byte("FirstKey")}
	prefix := SerializedPath{Value: []byte("FirstKe")}
	require.True(t, first.HasPrefix(prefix))
	require.True(t, first.HasStrictPrefix(prefix))

	first = SerializedPath{Value: []byte("FirstKey"), NibbleLength: 16}
	prefix = SerializedPath{Value: []byte("FirstKey"), NibbleLength: 15}
	require.True(t, first.HasPrefix(prefix))
	require.True(t, first.HasStrictPrefix(prefix))

	first = SerializedPath{Value: []byte("FirstKey"), NibbleLength: 15}
	prefix = SerializedPath{Value: []byte("FirstKey"), NibbleLength: 15}
	require.True(t, first.HasPrefix(prefix))
	require.False(t, first.HasStrictPrefix(prefix))

	first = SerializedPath{Value: []byte{247}, NibbleLength: 2}
	prefix = SerializedPath{Value: []byte{240}, NibbleLength: 2}
	require.False(t, first.HasPrefix(prefix))
	require.False(t, first.HasStrictPrefix(prefix))

	first = SerializedPath{Value: []byte{247}, NibbleLength: 2}
	prefix = SerializedPath{Value: []byte{240}, NibbleLength: 1}
	require.True(t, first.HasPrefix(prefix))
	require.True(t, first.HasStrictPrefix(prefix))

	first = SerializedPath{Value: []byte{}, NibbleLength: 0}
	prefix = SerializedPath{Value: []byte{}, NibbleLength: 0}
	require.True(t, first.HasPrefix(prefix))
	require.False(t, first.HasStrictPrefix(prefix))

	a := SerializedPath{Value: []byte{0x10}, NibbleLength: 1}
	b := SerializedPath{Value: []byte{0x10}, NibbleLength: 2}
	require.False(t, a.HasPrefix(b))
}

func Test_SerializedPath_HasPrefix_BadInput(t *testing.T) {
	a := SerializedPath{Value: []byte{}}
	b := SerializedPath{Value: []byte{}, NibbleLength: 1}
	require.False(t, a.HasPrefix(b))

	a = SerializedPath{Value: []byte{}, NibbleLength: 10}
	b = SerializedPath{Value: []byte{0x10}, NibbleLength: 1}
	require.False(t, a.HasPrefix(b))
}

func Test_SerializedPath_Equal(t *testing.T) {
	first := SerializedPath{Value: []byte("FirstKey"), NibbleLength: 16}
	prefix := SerializedPath{Value: []byte("FirstKey"), NibbleLength: 16}
	require.True(t, first.Equal(prefix))

	first = SerializedPath{Value: []byte("FirstKey"), NibbleLength: 16}
	prefix = SerializedPath{Value: []byte("FirstKey"), NibbleLength: 15}
	require.False(t, first.Equal(prefix))

	first = SerializedPath{Value: []byte("FirstKey"), NibbleLength: 15}
	prefix = SerializedPath{Value: []byte("FirstKey"), NibbleLength: 15}
	require.True(t, first.Equal(prefix))
}

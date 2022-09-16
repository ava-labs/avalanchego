// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package utils

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAtomicInterface(t *testing.T) {
	iface := NewAtomicInterface(nil)
	require.Nil(t, iface.GetValue())
	iface.SetValue(nil)
	require.Nil(t, iface.GetValue())
	val, ok := iface.GetValue().([]byte)
	require.False(t, ok)
	require.Nil(t, val)
	iface.SetValue([]byte("test"))
	require.Equal(t, []byte("test"), iface.GetValue().([]byte))
}

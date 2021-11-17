// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package utils

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestAtomicInterface(t *testing.T) {
	iface := NewAtomicInterface(nil)
	assert.Nil(t, iface.GetValue())
	iface.SetValue(nil)
	assert.Nil(t, iface.GetValue())
	val, ok := iface.GetValue().([]byte)
	assert.False(t, ok)
	assert.Nil(t, val)
	iface.SetValue([]byte("test"))
	assert.Equal(t, []byte("test"), iface.GetValue().([]byte))
}

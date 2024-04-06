// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package utils

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBytesPool(t *testing.T) {
	require := require.New(t)

	p := NewBytesPool()
	for i := 127; i < 128; i++ {
		p.Put(make([]byte, i))
	}
	// for j := 0; j < 256; j++ {
	for i := 127; i < 128; i++ {
		bytes := p.Get(i)
		require.Len(bytes, i)
		p.Put(bytes)
	}
	// }
}

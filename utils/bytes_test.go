// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package utils

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCopyBytesNil(t *testing.T) {
	result := CopyBytes(nil)
	require.Nil(t, result, "CopyBytes(nil) should have returned nil")
}

func TestCopyBytes(t *testing.T) {
	input := []byte{1}
	result := CopyBytes(input)
	require.Equal(t, input, result, "CopyBytes should have returned equal bytes")

	input[0] = 0
	require.NotEqual(t, input, result, "CopyBytes should have returned independent bytes")
}

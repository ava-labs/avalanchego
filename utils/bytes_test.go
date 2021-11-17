// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package utils

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCopyBytesNil(t *testing.T) {
	result := CopyBytes(nil)
	assert.Nil(t, result, "CopyBytes(nil) should have returned nil")
}

func TestCopyBytes(t *testing.T) {
	input := []byte{1}
	result := CopyBytes(input)
	assert.Equal(t, input, result, "CopyBytes should have returned equal bytes")

	input[0] = 0
	assert.NotEqual(t, input, result, "CopyBytes should have returned independent bytes")
}

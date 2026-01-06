// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package utils

import (
	"bytes"
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/utils"
)

func TestIncrOne(t *testing.T) {
	type test struct {
		input    []byte
		expected []byte
	}
	for name, test := range map[string]test{
		"increment no overflow no carry": {
			input:    []byte{0, 0},
			expected: []byte{0, 1},
		},
		"increment overflow": {
			input:    []byte{255, 255},
			expected: []byte{0, 0},
		},
		"increment carry": {
			input:    []byte{0, 255},
			expected: []byte{1, 0},
		},
	} {
		t.Run(name, func(t *testing.T) {
			output := common.CopyBytes(test.input)
			IncrOne(output)
			require.Equal(t, test.expected, output)
		})
	}
}

func testBytesToHashSlice(t testing.TB, b []byte) {
	hashSlice := BytesToHashSlice(b)

	copiedBytes := HashSliceToBytes(hashSlice)

	if len(b)%32 == 0 {
		require.Equal(t, b, copiedBytes)
	} else {
		require.Equal(t, b, copiedBytes[:len(b)])
		// Require that any additional padding is all zeroes
		padding := copiedBytes[len(b):]
		require.Equal(t, bytes.Repeat([]byte{0x00}, len(padding)), padding)
	}
}

func FuzzHashSliceToBytes(f *testing.F) {
	for i := 0; i < 100; i++ {
		f.Add(utils.RandomBytes(i))
	}

	f.Fuzz(func(t *testing.T, b []byte) {
		testBytesToHashSlice(t, b)
	})
}

// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package utils

import (
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/stretchr/testify/require"
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

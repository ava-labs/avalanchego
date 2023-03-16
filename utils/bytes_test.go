// (c) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package utils

import (
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/assert"
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
			assert.Equal(t, output, test.expected)
		})
	}
}

func TestHashSliceToBytes(t *testing.T) {
	type test struct {
		input    []common.Hash
		expected []byte
	}
	for name, test := range map[string]test{
		"empty slice": {
			input:    []common.Hash{},
			expected: []byte{},
		},
		"convert single hash": {
			input: []common.Hash{
				common.BytesToHash([]byte{1, 2, 3}),
			},
			expected: []byte{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1, 2, 3},
		},
		"convert hash slice": {
			input: []common.Hash{
				common.BytesToHash([]byte{1, 2, 3}),
				common.BytesToHash([]byte{4, 5, 6}),
			},
			expected: []byte{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1, 2, 3, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 4, 5, 6},
		},
	} {
		t.Run(name, func(t *testing.T) {
			output := HashSliceToBytes(test.input)
			assert.Equal(t, output, test.expected)
		})
	}
}

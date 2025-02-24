// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package utils

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDeleteIndex(t *testing.T) {
	tests := []struct {
		name     string
		s        []int
		i        int
		expected []int
	}{
		{
			name:     "delete only element",
			s:        []int{0},
			i:        0,
			expected: []int{},
		},
		{
			name:     "delete first element",
			s:        []int{0, 1},
			i:        0,
			expected: []int{1},
		},
		{
			name:     "delete middle element",
			s:        []int{0, 1, 2, 3},
			i:        1,
			expected: []int{0, 3, 2},
		},
		{
			name:     "delete last element",
			s:        []int{0, 1, 2, 3},
			i:        3,
			expected: []int{0, 1, 2},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			s := DeleteIndex(test.s, test.i)
			require.Equal(t, test.expected, s)
		})
	}
}

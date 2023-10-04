// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package heap

import (
	"testing"

	require "github.com/stretchr/testify/require"
)

func TestMap(t *testing.T) {
	tests := []struct {
		name     string
		setup    func(h Map[int, int])
		expected []entry[int, int]
	}{
		{
			name: "only push",
			setup: func(h Map[int, int]) {
				h.Push(1, 10)
				h.Push(2, 20)
				h.Push(3, 30)
			},
			expected: []entry[int, int]{
				{k: 1, v: 10},
				{k: 2, v: 20},
				{k: 3, v: 30},
			},
		},
		{
			name: "out of order pushes",
			setup: func(h Map[int, int]) {
				h.Push(1, 10)
				h.Push(5, 50)
				h.Push(2, 20)
				h.Push(4, 40)
				h.Push(3, 30)
			},
			expected: []entry[int, int]{
				{1, 10},
				{2, 20},
				{3, 30},
				{4, 40},
				{5, 50},
			},
		},
		{
			name: "push and pop",
			setup: func(h Map[int, int]) {
				h.Push(1, 10)
				h.Push(5, 50)
				h.Push(2, 20)
				h.Push(4, 40)
				h.Push(3, 30)
				h.Pop()
				h.Pop()
				h.Pop()
			},
			expected: []entry[int, int]{
				{4, 40},
				{5, 50},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)

			h := NewMap[int, int](func(a, b int) bool {
				return a < b
			})

			tt.setup(h)

			require.Equal(len(tt.expected), h.Len())
			for _, expected := range tt.expected {
				k, v, ok := h.Pop()
				require.True(ok)
				require.Equal(expected.k, k)
				require.Equal(expected.v, v)
			}
		})
	}
}

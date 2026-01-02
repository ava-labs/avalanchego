// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package heap

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestHeap(t *testing.T) {
	tests := []struct {
		name     string
		setup    func(h Queue[int])
		expected []int
	}{
		{
			name: "only push",
			setup: func(h Queue[int]) {
				h.Push(1)
				h.Push(2)
				h.Push(3)
			},
			expected: []int{1, 2, 3},
		},
		{
			name: "out of order pushes",
			setup: func(h Queue[int]) {
				h.Push(1)
				h.Push(5)
				h.Push(2)
				h.Push(4)
				h.Push(3)
			},
			expected: []int{1, 2, 3, 4, 5},
		},
		{
			name: "push and pop",
			setup: func(h Queue[int]) {
				h.Push(1)
				h.Push(5)
				h.Push(2)
				h.Push(4)
				h.Push(3)
				h.Pop()
				h.Pop()
				h.Pop()
			},
			expected: []int{4, 5},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)

			h := NewQueue[int](func(a, b int) bool {
				return a < b
			})

			tt.setup(h)

			require.Equal(len(tt.expected), h.Len())
			for _, expected := range tt.expected {
				got, ok := h.Pop()
				require.True(ok)
				require.Equal(expected, got)
			}
		})
	}
}

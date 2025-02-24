// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package heap

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMap(t *testing.T) {
	tests := []struct {
		name     string
		setup    func(h Map[string, int])
		expected []entry[string, int]
	}{
		{
			name: "only push",
			setup: func(h Map[string, int]) {
				h.Push("a", 1)
				h.Push("b", 2)
				h.Push("c", 3)
			},
			expected: []entry[string, int]{
				{k: "a", v: 1},
				{k: "b", v: 2},
				{k: "c", v: 3},
			},
		},
		{
			name: "out of order pushes",
			setup: func(h Map[string, int]) {
				h.Push("a", 1)
				h.Push("e", 5)
				h.Push("b", 2)
				h.Push("d", 4)
				h.Push("c", 3)
			},
			expected: []entry[string, int]{
				{"a", 1},
				{"b", 2},
				{"c", 3},
				{"d", 4},
				{"e", 5},
			},
		},
		{
			name: "push and pop",
			setup: func(m Map[string, int]) {
				m.Push("a", 1)
				m.Push("e", 5)
				m.Push("b", 2)
				m.Push("d", 4)
				m.Push("c", 3)
				m.Pop()
				m.Pop()
				m.Pop()
			},
			expected: []entry[string, int]{
				{"d", 4},
				{"e", 5},
			},
		},
		{
			name: "duplicate key is overridden",
			setup: func(h Map[string, int]) {
				h.Push("a", 1)
				h.Push("a", 2)
			},
			expected: []entry[string, int]{
				{k: "a", v: 2},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)

			h := NewMap[string, int](func(a, b int) bool {
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

// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package set

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSet(t *testing.T) {
	require := require.New(t)
	id1 := 1

	s := Set[int]{id1: struct{}{}}

	s.Add(id1)
	require.True(s.Contains(id1))

	s.Remove(id1)
	require.False(s.Contains(id1))

	s.Add(id1)
	require.True(s.Contains(id1))
	require.Len(s.List(), 1)
	require.Equal(id1, s.List()[0])

	s.Clear()
	require.False(s.Contains(id1))

	s.Add(id1)

	s2 := Set[int]{}

	require.False(s.Overlaps(s2))

	s2.Union(s)
	require.True(s2.Contains(id1))
	require.True(s.Overlaps(s2))

	s2.Difference(s)
	require.False(s2.Contains(id1))
	require.False(s.Overlaps(s2))
}

func TestOf(t *testing.T) {
	tests := []struct {
		name     string
		elements []int
		expected []int
	}{
		{
			name:     "nil",
			elements: nil,
			expected: []int{},
		},
		{
			name:     "empty",
			elements: []int{},
			expected: []int{},
		},
		{
			name:     "unique elements",
			elements: []int{1, 2, 3},
			expected: []int{1, 2, 3},
		},
		{
			name:     "duplicate elements",
			elements: []int{1, 2, 3, 1, 2, 3},
			expected: []int{1, 2, 3},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)

			s := Of(tt.elements...)

			require.Len(s, len(tt.expected))
			for _, expected := range tt.expected {
				require.True(s.Contains(expected))
			}
		})
	}
}

func TestSetClear(t *testing.T) {
	require := require.New(t)

	set := Set[int]{}
	for i := 0; i < 25; i++ {
		set.Add(i)
	}
	set.Clear()
	require.Empty(set)
	set.Add(1337)
	require.Len(set, 1)
}

func TestSetPop(t *testing.T) {
	require := require.New(t)

	var s Set[int]
	_, ok := s.Pop()
	require.False(ok)

	s = make(Set[int])
	_, ok = s.Pop()
	require.False(ok)

	id1, id2 := 0, 1
	s.Add(id1, id2)

	got, ok := s.Pop()
	require.True(ok)
	require.True(got == id1 || got == id2)
	require.Equal(1, s.Len())

	got, ok = s.Pop()
	require.True(ok)
	require.True(got == id1 || got == id2)
	require.Zero(s.Len())

	_, ok = s.Pop()
	require.False(ok)
}

func TestSetMarshalJSON(t *testing.T) {
	require := require.New(t)
	set := Set[int]{}
	{
		asJSON, err := set.MarshalJSON()
		require.NoError(err)
		require.JSONEq("[]", string(asJSON))
	}
	id1, id2 := 1, 2
	id1JSON, err := json.Marshal(id1)
	require.NoError(err)
	id2JSON, err := json.Marshal(id2)
	require.NoError(err)
	set.Add(id1)
	{
		asJSON, err := set.MarshalJSON()
		require.NoError(err)
		require.JSONEq(fmt.Sprintf("[%s]", string(id1JSON)), string(asJSON))
	}
	set.Add(id2)
	{
		asJSON, err := set.MarshalJSON()
		require.NoError(err)
		require.JSONEq(fmt.Sprintf("[%s,%s]", string(id1JSON), string(id2JSON)), string(asJSON))
	}
}

func TestSetUnmarshalJSON(t *testing.T) {
	require := require.New(t)
	set := Set[int]{}
	{
		require.NoError(set.UnmarshalJSON([]byte("[]")))
		require.Empty(set)
	}
	id1, id2 := 1, 2
	id1JSON, err := json.Marshal(id1)
	require.NoError(err)
	id2JSON, err := json.Marshal(id2)
	require.NoError(err)
	{
		require.NoError(set.UnmarshalJSON(fmt.Appendf(nil, "[%s]", string(id1JSON))))
		require.Len(set, 1)
		require.Contains(set, id1)
	}
	{
		require.NoError(set.UnmarshalJSON(fmt.Appendf(nil, "[%s,%s]", string(id1JSON), string(id2JSON))))
		require.Len(set, 2)
		require.Contains(set, id1)
		require.Contains(set, id2)
	}
	{
		require.NoError(set.UnmarshalJSON(fmt.Appendf(nil, "[%d,%d,%d]", 3, 4, 5)))
		require.Len(set, 3)
		require.Contains(set, 3)
		require.Contains(set, 4)
		require.Contains(set, 5)
	}
	{
		require.NoError(set.UnmarshalJSON(fmt.Appendf(nil, "[%d,%d,%d, %d]", 3, 4, 5, 3)))
		require.Len(set, 3)
		require.Contains(set, 3)
		require.Contains(set, 4)
		require.Contains(set, 5)
	}
	{
		set1 := Set[int]{}
		set2 := Set[int]{}
		require.NoError(set1.UnmarshalJSON(fmt.Appendf(nil, "[%s,%s]", string(id1JSON), string(id2JSON))))
		require.NoError(set2.UnmarshalJSON(fmt.Appendf(nil, "[%s,%s]", string(id2JSON), string(id1JSON))))
		require.Equal(set1, set2)
	}
}

func TestSetReflectJSONMarshal(t *testing.T) {
	require := require.New(t)
	set := Set[int]{}
	{
		asJSON, err := json.Marshal(set)
		require.NoError(err)
		require.JSONEq("[]", string(asJSON))
	}
	id1JSON, err := json.Marshal(1)
	require.NoError(err)
	id2JSON, err := json.Marshal(2)
	require.NoError(err)
	set.Add(1)
	{
		asJSON, err := json.Marshal(set)
		require.NoError(err)
		require.JSONEq(fmt.Sprintf("[%s]", string(id1JSON)), string(asJSON))
	}
	set.Add(2)
	{
		asJSON, err := json.Marshal(set)
		require.NoError(err)
		require.JSONEq(fmt.Sprintf("[%s,%s]", string(id1JSON), string(id2JSON)), string(asJSON))
	}
}

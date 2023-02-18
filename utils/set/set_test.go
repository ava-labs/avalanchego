// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
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
	require.Equal(len(s.List()), 1)
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

func TestSetCappedList(t *testing.T) {
	require := require.New(t)
	s := Set[int]{}

	id := 0

	require.Len(s.CappedList(0), 0)

	s.Add(id)

	require.Len(s.CappedList(0), 0)
	require.Len(s.CappedList(1), 1)
	require.Equal(s.CappedList(1)[0], id)
	require.Len(s.CappedList(2), 1)
	require.Equal(s.CappedList(2)[0], id)

	id2 := 1
	s.Add(id2)

	require.Len(s.CappedList(0), 0)
	require.Len(s.CappedList(1), 1)
	require.Len(s.CappedList(2), 2)
	require.Len(s.CappedList(3), 2)
	gotList := s.CappedList(2)
	require.Contains(gotList, id)
	require.Contains(gotList, id2)
	require.NotEqual(gotList[0], gotList[1])
}

func TestSetClear(t *testing.T) {
	set := Set[int]{}
	for i := 0; i < 25; i++ {
		set.Add(i)
	}
	set.Clear()
	require.Len(t, set, 0)
	set.Add(1337)
	require.Len(t, set, 1)
}

func TestSetPop(t *testing.T) {
	var s Set[int]
	_, ok := s.Pop()
	require.False(t, ok)

	s = make(Set[int])
	_, ok = s.Pop()
	require.False(t, ok)

	id1, id2 := 0, 1
	s.Add(id1, id2)

	got, ok := s.Pop()
	require.True(t, ok)
	require.True(t, got == id1 || got == id2)
	require.EqualValues(t, 1, s.Len())

	got, ok = s.Pop()
	require.True(t, ok)
	require.True(t, got == id1 || got == id2)
	require.EqualValues(t, 0, s.Len())

	_, ok = s.Pop()
	require.False(t, ok)
}

func TestSetMarshalJSON(t *testing.T) {
	require := require.New(t)
	set := Set[int]{}
	{
		asJSON, err := set.MarshalJSON()
		require.NoError(err)
		require.Equal("[]", string(asJSON))
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
		require.Equal(fmt.Sprintf("[%s]", string(id1JSON)), string(asJSON))
	}
	set.Add(id2)
	{
		asJSON, err := set.MarshalJSON()
		require.NoError(err)
		require.Equal(fmt.Sprintf("[%s,%s]", string(id1JSON), string(id2JSON)), string(asJSON))
	}
}

func TestSetUnmarshalJSON(t *testing.T) {
	require := require.New(t)
	set := Set[int]{}
	{
		err := set.UnmarshalJSON([]byte("[]"))
		require.NoError(err)
		require.Empty(set)
	}
	id1, id2 := 1, 2
	id1JSON, err := json.Marshal(id1)
	require.NoError(err)
	id2JSON, err := json.Marshal(id2)
	require.NoError(err)
	{
		err := set.UnmarshalJSON([]byte(fmt.Sprintf("[%s]", string(id1JSON))))
		require.NoError(err)
		require.Len(set, 1)
		require.Contains(set, id1)
	}
	{
		err := set.UnmarshalJSON([]byte(fmt.Sprintf("[%s,%s]", string(id1JSON), string(id2JSON))))
		require.NoError(err)
		require.Len(set, 2)
		require.Contains(set, id1)
		require.Contains(set, id2)
	}
	{
		err := set.UnmarshalJSON([]byte(fmt.Sprintf("[%d,%d,%d]", 3, 4, 5)))
		require.NoError(err)
		require.Len(set, 3)
		require.Contains(set, 3)
		require.Contains(set, 4)
		require.Contains(set, 5)
	}
	{
		err := set.UnmarshalJSON([]byte(fmt.Sprintf("[%d,%d,%d, %d]", 3, 4, 5, 3)))
		require.NoError(err)
		require.Len(set, 3)
		require.Contains(set, 3)
		require.Contains(set, 4)
		require.Contains(set, 5)
	}
	{
		set1 := Set[int]{}
		set2 := Set[int]{}
		err := set1.UnmarshalJSON([]byte(fmt.Sprintf("[%s,%s]", string(id1JSON), string(id2JSON))))
		require.NoError(err)
		err = set2.UnmarshalJSON([]byte(fmt.Sprintf("[%s,%s]", string(id2JSON), string(id1JSON))))
		require.NoError(err)
		require.Equal(set1, set2)
	}
}

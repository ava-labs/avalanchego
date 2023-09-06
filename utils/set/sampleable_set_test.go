// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package set

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSampleableSet(t *testing.T) {
	require := require.New(t)
	id1 := 1

	s := SampleableSet[int]{}

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

	s2 := SampleableSet[int]{}

	require.False(s.Overlaps(s2))

	s2.Union(s)
	require.True(s2.Contains(id1))
	require.True(s.Overlaps(s2))

	s2.Difference(s)
	require.False(s2.Contains(id1))
	require.False(s.Overlaps(s2))
}

func TestSampleableSetClear(t *testing.T) {
	require := require.New(t)

	set := SampleableSet[int]{}
	for i := 0; i < 25; i++ {
		set.Add(i)
	}
	set.Clear()
	require.Zero(set.Len())
	set.Add(1337)
	require.Equal(1, set.Len())
}

func TestSampleableSetMarshalJSON(t *testing.T) {
	require := require.New(t)
	set := SampleableSet[int]{}
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

func TestSampleableSetUnmarshalJSON(t *testing.T) {
	require := require.New(t)
	set := SampleableSet[int]{}
	{
		require.NoError(set.UnmarshalJSON([]byte("[]")))
		require.Zero(set.Len())
	}
	id1, id2 := 1, 2
	id1JSON, err := json.Marshal(id1)
	require.NoError(err)
	id2JSON, err := json.Marshal(id2)
	require.NoError(err)
	{
		require.NoError(set.UnmarshalJSON([]byte(fmt.Sprintf("[%s]", string(id1JSON)))))
		require.Equal(1, set.Len())
		require.True(set.Contains(id1))
	}
	{
		require.NoError(set.UnmarshalJSON([]byte(fmt.Sprintf("[%s,%s]", string(id1JSON), string(id2JSON)))))
		require.Equal(2, set.Len())
		require.True(set.Contains(id1))
		require.True(set.Contains(id2))
	}
	{
		require.NoError(set.UnmarshalJSON([]byte(fmt.Sprintf("[%d,%d,%d]", 3, 4, 5))))
		require.Equal(3, set.Len())
		require.True(set.Contains(3))
		require.True(set.Contains(4))
		require.True(set.Contains(5))
	}
	{
		require.NoError(set.UnmarshalJSON([]byte(fmt.Sprintf("[%d,%d,%d, %d]", 3, 4, 5, 3))))
		require.Equal(3, set.Len())
		require.True(set.Contains(3))
		require.True(set.Contains(4))
		require.True(set.Contains(5))
	}
	{
		set1 := SampleableSet[int]{}
		set2 := SampleableSet[int]{}
		require.NoError(set1.UnmarshalJSON([]byte(fmt.Sprintf("[%s,%s]", string(id1JSON), string(id2JSON)))))
		require.NoError(set2.UnmarshalJSON([]byte(fmt.Sprintf("[%s,%s]", string(id2JSON), string(id1JSON)))))
		require.True(set1.Equals(set2))
	}
}

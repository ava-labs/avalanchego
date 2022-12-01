// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package set

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"testing"

	"github.com/stretchr/testify/require"
)

func init() {
	rand.Seed(1337) // For determinism in generateTestSettable
}

type testSettable [20]byte

func (s testSettable) String() string {
	return fmt.Sprintf("%v", [20]byte(s))
}

func generateTestSettable() testSettable {
	var s testSettable
	_, _ = rand.Read(s[:]) // #nosec G404
	return s
}

func TestSet(t *testing.T) {
	require := require.New(t)
	id1 := testSettable{1}

	s := Set[testSettable]{id1: struct{}{}}

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

	s2 := Set[testSettable]{}

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
	s := Set[testSettable]{}

	var id testSettable

	require.Len(s.CappedList(0), 0)

	s.Add(id)

	require.Len(s.CappedList(0), 0)
	require.Len(s.CappedList(1), 1)
	require.Equal(s.CappedList(1)[0], id)
	require.Len(s.CappedList(2), 1)
	require.Equal(s.CappedList(2)[0], id)

	id2 := testSettable{1}
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
	set := Set[testSettable]{}
	for i := 0; i < 25; i++ {
		set.Add(generateTestSettable())
	}
	set.Clear()
	require.Len(t, set, 0)
	set.Add(generateTestSettable())
	require.Len(t, set, 1)
}

func TestSetPop(t *testing.T) {
	var s Set[testSettable]
	_, ok := s.Pop()
	require.False(t, ok)

	s = make(Set[testSettable])
	_, ok = s.Pop()
	require.False(t, ok)

	id1, id2 := generateTestSettable(), generateTestSettable()
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
	set := Set[testSettable]{}
	{
		asJSON, err := set.MarshalJSON()
		require.NoError(err)
		require.Equal("[]", string(asJSON))
	}
	id1, id2 := testSettable{1}, testSettable{2}
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

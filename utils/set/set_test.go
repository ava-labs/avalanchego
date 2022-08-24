// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package set

import (
	"crypto/rand"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

type settable [20]byte

func (s settable) String() string {
	return ""
}

func generateTestSettable() settable {
	var s settable
	_, _ = rand.Read(s[:])
	return s
}

func TestSet(t *testing.T) {
	id1 := settable{1}

	s := Set[settable]{id1: struct{}{}}

	s.Add(id1)
	if !s.Contains(id1) {
		t.Fatalf("Initial value not set correctly")
	}

	s.Remove(id1)
	if s.Contains(id1) {
		t.Fatalf("Value not removed correctly")
	}

	s.Add(id1)
	if !s.Contains(id1) {
		t.Fatalf("Initial value not set correctly")
	} else if s.Len() != 1 {
		t.Fatalf("Bad set size")
	} else if list := s.List(); len(list) != 1 {
		t.Fatalf("Bad list size")
	} else if list[0] != id1 {
		t.Fatalf("Set value not correct")
	}

	s.Clear()
	if s.Contains(id1) {
		t.Fatalf("Value not removed correctly")
	}

	s.Add(id1)

	s2 := Set[settable]{}

	if s.Overlaps(s2) {
		t.Fatalf("Empty set shouldn't overlap")
	}

	s2.Union(s)
	if !s2.Contains(id1) {
		t.Fatalf("Value not union added correctly")
	}

	if !s.Overlaps(s2) {
		t.Fatalf("Sets overlap")
	}

	s2.Difference(s)
	if s2.Contains(id1) {
		t.Fatalf("Value not difference removed correctly")
	}

	if s.Overlaps(s2) {
		t.Fatalf("Sets don't overlap")
	}
}

func TestSetCappedList(t *testing.T) {
	s := Set[settable]{}

	var id settable

	if list := s.CappedList(0); len(list) != 0 {
		t.Fatalf("List should have been empty but was %v", list)
	}

	s.Add(id)

	if list := s.CappedList(0); len(list) != 0 {
		t.Fatalf("List should have been empty but was %v", list)
	} else if list := s.CappedList(1); len(list) != 1 {
		t.Fatalf("List should have had length %d but had %d", 1, len(list))
	} else if returnedID := list[0]; id != returnedID {
		t.Fatalf("List should have been %s but was %s", id, returnedID)
	} else if list := s.CappedList(2); len(list) != 1 {
		t.Fatalf("List should have had length %d but had %d", 1, len(list))
	} else if returnedID := list[0]; id != returnedID {
		t.Fatalf("List should have been %s but was %s", id, returnedID)
	}

	id2 := settable{1}
	s.Add(id2)

	if list := s.CappedList(0); len(list) != 0 {
		t.Fatalf("List should have been empty but was %v", list)
	} else if list := s.CappedList(1); len(list) != 1 {
		t.Fatalf("List should have had length %d but had %d", 1, len(list))
	} else if returnedID := list[0]; id != returnedID && id2 != returnedID {
		t.Fatalf("List should have been %s but was %s", id, returnedID)
	} else if list := s.CappedList(2); len(list) != 2 {
		t.Fatalf("List should have had length %d but had %d", 2, len(list))
	} else if list := s.CappedList(3); len(list) != 2 {
		t.Fatalf("List should have had length %d but had %d", 2, len(list))
	} else if returnedID := list[0]; id != returnedID && id2 != returnedID {
		t.Fatalf("list contains unexpected element %s", returnedID)
	} else if returnedID := list[1]; id != returnedID && id2 != returnedID {
		t.Fatalf("list contains unexpected element %s", returnedID)
	}
}

// Test that Clear() works with both the iterative and set-to-nil path
func TestSetClearLarge(t *testing.T) {
	// Using iterative clear path
	set := Set[settable]{}
	for i := 0; i < clearSizeThreshold; i++ {
		set.Add(generateTestSettable())
	}
	set.Clear()
	if set.Len() != 0 {
		t.Fatal("length should be 0")
	}
	set.Add(generateTestSettable())
	if set.Len() != 1 {
		t.Fatal("length should be 1")
	}

	// Using bulk (set map to nil) path
	set = Set[settable]{}
	for i := 0; i < clearSizeThreshold+1; i++ {
		set.Add(generateTestSettable())
	}
	set.Clear()
	if set.Len() != 0 {
		t.Fatal("length should be 0")
	}
	set.Add(generateTestSettable())
	if set.Len() != 1 {
		t.Fatal("length should be 1")
	}
}

func TestSetPop(t *testing.T) {
	var s Set[settable]
	_, ok := s.Pop()
	require.False(t, ok)

	s = make(Set[settable])
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
	set := Set[settable]{}
	{
		asJSON, err := set.MarshalJSON()
		require.NoError(err)
		require.Equal("[]", string(asJSON))
	}
	id1, id2 := generateTestSettable(), generateTestSettable()
	set.Add(id1)
	{
		asJSON, err := set.MarshalJSON()
		require.NoError(err)
		require.Equal(fmt.Sprintf("[\"%s\"]", id1), string(asJSON))
	}
	set.Add(id2)
	{
		asJSON, err := set.MarshalJSON()
		require.NoError(err)
		require.Equal(fmt.Sprintf("[\"%s\",\"%s\"]", id1, id2), string(asJSON))
	}
}

// TODO remove if we can't implement this
// func TestSortedList(t *testing.T) {
// 	require := require.New(t)

// 	set := Set[settable]{}
// 	require.Len(set.SortedList(), 0)

// 	set.Add(ID{0})
// 	sorted := set.SortedList()
// 	require.Len(sorted, 1)
// 	require.Equal(ID{0}, sorted[0])

// 	set.Add(ID{1})
// 	sorted = set.SortedList()
// 	require.Len(sorted, 2)
// 	require.Equal(ID{0}, sorted[0])
// 	require.Equal(ID{1}, sorted[1])

// 	set.Add(ID{2})
// 	sorted = set.SortedList()
// 	require.Len(sorted, 3)
// 	require.Equal(ID{0}, sorted[0])
// 	require.Equal(ID{1}, sorted[1])
// 	require.Equal(ID{2}, sorted[2])
// }

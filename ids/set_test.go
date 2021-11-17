// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package ids

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSet(t *testing.T) {
	id1 := ID{1}

	ids := Set{}

	ids.Add(id1)
	if !ids.Contains(id1) {
		t.Fatalf("Initial value not set correctly")
	}

	ids.Remove(id1)
	if ids.Contains(id1) {
		t.Fatalf("Value not removed correctly")
	}

	ids.Add(id1)
	if !ids.Contains(id1) {
		t.Fatalf("Initial value not set correctly")
	} else if ids.Len() != 1 {
		t.Fatalf("Bad set size")
	} else if list := ids.List(); len(list) != 1 {
		t.Fatalf("Bad list size")
	} else if list[0] != id1 {
		t.Fatalf("Set value not correct")
	}

	ids.Clear()
	if ids.Contains(id1) {
		t.Fatalf("Value not removed correctly")
	}

	ids.Add(id1)

	ids2 := Set{}

	if ids.Overlaps(ids2) {
		t.Fatalf("Empty set shouldn't overlap")
	}

	ids2.Union(ids)
	if !ids2.Contains(id1) {
		t.Fatalf("Value not union added correctly")
	}

	if !ids.Overlaps(ids2) {
		t.Fatalf("Sets overlap")
	}

	ids2.Difference(ids)
	if ids2.Contains(id1) {
		t.Fatalf("Value not difference removed correctly")
	}

	if ids.Overlaps(ids2) {
		t.Fatalf("Sets don't overlap")
	}
}

func TestSetCappedList(t *testing.T) {
	set := Set{}

	id := Empty

	if list := set.CappedList(0); len(list) != 0 {
		t.Fatalf("List should have been empty but was %v", list)
	}

	set.Add(id)

	if list := set.CappedList(0); len(list) != 0 {
		t.Fatalf("List should have been empty but was %v", list)
	} else if list := set.CappedList(1); len(list) != 1 {
		t.Fatalf("List should have had length %d but had %d", 1, len(list))
	} else if returnedID := list[0]; id != returnedID {
		t.Fatalf("List should have been %s but was %s", id, returnedID)
	} else if list := set.CappedList(2); len(list) != 1 {
		t.Fatalf("List should have had length %d but had %d", 1, len(list))
	} else if returnedID := list[0]; id != returnedID {
		t.Fatalf("List should have been %s but was %s", id, returnedID)
	}

	id2 := ID{1}
	set.Add(id2)

	if list := set.CappedList(0); len(list) != 0 {
		t.Fatalf("List should have been empty but was %v", list)
	} else if list := set.CappedList(1); len(list) != 1 {
		t.Fatalf("List should have had length %d but had %d", 1, len(list))
	} else if returnedID := list[0]; id != returnedID && id2 != returnedID {
		t.Fatalf("List should have been %s but was %s", id, returnedID)
	} else if list := set.CappedList(2); len(list) != 2 {
		t.Fatalf("List should have had length %d but had %d", 2, len(list))
	} else if list := set.CappedList(3); len(list) != 2 {
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
	set := Set{}
	for i := 0; i < clearSizeThreshold; i++ {
		set.Add(GenerateTestID())
	}
	set.Clear()
	if set.Len() != 0 {
		t.Fatal("length should be 0")
	}
	set.Add(GenerateTestID())
	if set.Len() != 1 {
		t.Fatal("length should be 1")
	}

	// Using bulk (set map to nil) path
	set = Set{}
	for i := 0; i < clearSizeThreshold+1; i++ {
		set.Add(GenerateTestID())
	}
	set.Clear()
	if set.Len() != 0 {
		t.Fatal("length should be 0")
	}
	set.Add(GenerateTestID())
	if set.Len() != 1 {
		t.Fatal("length should be 1")
	}
}

func TestSetPop(t *testing.T) {
	var s Set
	_, ok := s.Pop()
	assert.False(t, ok)

	s = make(Set)
	_, ok = s.Pop()
	assert.False(t, ok)

	id1, id2 := GenerateTestID(), GenerateTestID()
	s.Add(id1, id2)

	got, ok := s.Pop()
	assert.True(t, ok)
	assert.True(t, got == id1 || got == id2)
	assert.EqualValues(t, 1, s.Len())

	got, ok = s.Pop()
	assert.True(t, ok)
	assert.True(t, got == id1 || got == id2)
	assert.EqualValues(t, 0, s.Len())

	_, ok = s.Pop()
	assert.False(t, ok)
}

func TestSetMarshalJSON(t *testing.T) {
	assert := assert.New(t)
	set := Set{}
	{
		asJSON, err := set.MarshalJSON()
		assert.NoError(err)
		assert.Equal("[]", string(asJSON))
	}
	id1, id2 := GenerateTestID(), GenerateTestID()
	set.Add(id1)
	{
		asJSON, err := set.MarshalJSON()
		assert.NoError(err)
		assert.Equal(fmt.Sprintf("[\"%s\"]", id1), string(asJSON))
	}
	set.Add(id2)
	{
		asJSON, err := set.MarshalJSON()
		assert.NoError(err)
		assert.Equal(fmt.Sprintf("[\"%s\",\"%s\"]", id1, id2), string(asJSON))
	}
}

func TestSortedList(t *testing.T) {
	assert := assert.New(t)

	set := Set{}
	assert.Len(set.SortedList(), 0)

	set.Add(ID{0})
	sorted := set.SortedList()
	assert.Len(sorted, 1)
	assert.Equal(ID{0}, sorted[0])

	set.Add(ID{1})
	sorted = set.SortedList()
	assert.Len(sorted, 2)
	assert.Equal(ID{0}, sorted[0])
	assert.Equal(ID{1}, sorted[1])

	set.Add(ID{2})
	sorted = set.SortedList()
	assert.Len(sorted, 3)
	assert.Equal(ID{0}, sorted[0])
	assert.Equal(ID{1}, sorted[1])
	assert.Equal(ID{2}, sorted[2])
}

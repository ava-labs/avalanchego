// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package ids

import (
	"testing"
)

func TestSet(t *testing.T) {
	id1 := ID([32]byte{1})

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
	} else if !list[0].Equals(id1) {
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
	} else if returnedID := list[0]; !id.Equals(returnedID) {
		t.Fatalf("List should have been %s but was %s", id, returnedID)
	} else if list := set.CappedList(2); len(list) != 1 {
		t.Fatalf("List should have had length %d but had %d", 1, len(list))
	} else if returnedID := list[0]; !id.Equals(returnedID) {
		t.Fatalf("List should have been %s but was %s", id, returnedID)
	}

	id2 := ID([32]byte{1})
	set.Add(id2)

	if list := set.CappedList(0); len(list) != 0 {
		t.Fatalf("List should have been empty but was %v", list)
	} else if list := set.CappedList(1); len(list) != 1 {
		t.Fatalf("List should have had length %d but had %d", 1, len(list))
	} else if returnedID := list[0]; !id.Equals(returnedID) && !id2.Equals(returnedID) {
		t.Fatalf("List should have been %s but was %s", id, returnedID)
	} else if list := set.CappedList(2); len(list) != 2 {
		t.Fatalf("List should have had length %d but had %d", 2, len(list))
	} else if list := set.CappedList(3); len(list) != 2 {
		t.Fatalf("List should have had length %d but had %d", 2, len(list))
	} else if returnedID := list[0]; !id.Equals(returnedID) && !id2.Equals(returnedID) {
		t.Fatalf("list contains unexpected element %s", returnedID)
	} else if returnedID := list[1]; !id.Equals(returnedID) && !id2.Equals(returnedID) {
		t.Fatalf("list contains unexpected element %s", returnedID)
	}
}

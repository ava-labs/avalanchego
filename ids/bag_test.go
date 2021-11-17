// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package ids

import (
	"testing"
)

func TestBagAdd(t *testing.T) {
	id0 := Empty
	id1 := ID{1}

	bag := Bag{}

	if count := bag.Count(id0); count != 0 {
		t.Fatalf("Bag.Count returned %d expected %d", count, 0)
	} else if count := bag.Count(id1); count != 0 {
		t.Fatalf("Bag.Count returned %d expected %d", count, 0)
	} else if size := bag.Len(); size != 0 {
		t.Fatalf("Bag.Len returned %d elements expected %d", count, 0)
	} else if list := bag.List(); len(list) != 0 {
		t.Fatalf("Bag.List returned %v expected %v", list, nil)
	} else if mode, freq := bag.Mode(); mode != Empty {
		t.Fatalf("Bag.Mode[0] returned %s expected %s", mode, ID{})
	} else if freq != 0 {
		t.Fatalf("Bag.Mode[1] returned %d expected %d", freq, 0)
	} else if threshold := bag.Threshold(); threshold.Len() != 0 {
		t.Fatalf("Bag.Threshold returned %s expected %s", threshold, Set{})
	}

	bag.Add(id0)

	if count := bag.Count(id0); count != 1 {
		t.Fatalf("Bag.Count returned %d expected %d", count, 1)
	} else if count := bag.Count(id1); count != 0 {
		t.Fatalf("Bag.Count returned %d expected %d", count, 0)
	} else if size := bag.Len(); size != 1 {
		t.Fatalf("Bag.Len returned %d expected %d", count, 1)
	} else if list := bag.List(); len(list) != 1 {
		t.Fatalf("Bag.List returned %d expected %d", len(list), 1)
	} else if mode, freq := bag.Mode(); mode != id0 {
		t.Fatalf("Bag.Mode[0] returned %s expected %s", mode, id0)
	} else if freq != 1 {
		t.Fatalf("Bag.Mode[1] returned %d expected %d", freq, 1)
	} else if threshold := bag.Threshold(); threshold.Len() != 1 {
		t.Fatalf("Bag.Threshold returned %d expected %d", len(threshold), 1)
	}

	bag.Add(id0)

	if count := bag.Count(id0); count != 2 {
		t.Fatalf("Bag.Count returned %d expected %d", count, 2)
	} else if count := bag.Count(id1); count != 0 {
		t.Fatalf("Bag.Count returned %d expected %d", count, 0)
	} else if size := bag.Len(); size != 2 {
		t.Fatalf("Bag.Len returned %d expected %d", count, 2)
	} else if list := bag.List(); len(list) != 1 {
		t.Fatalf("Bag.List returned %d expected %d", len(list), 1)
	} else if mode, freq := bag.Mode(); mode != id0 {
		t.Fatalf("Bag.Mode[0] returned %s expected %s", mode, id0)
	} else if freq != 2 {
		t.Fatalf("Bag.Mode[1] returned %d expected %d", freq, 2)
	} else if threshold := bag.Threshold(); threshold.Len() != 1 {
		t.Fatalf("Bag.Threshold returned %d expected %d", len(threshold), 1)
	}

	bag.AddCount(id1, 3)

	if count := bag.Count(id0); count != 2 {
		t.Fatalf("Bag.Count returned %d expected %d", count, 2)
	} else if count := bag.Count(id1); count != 3 {
		t.Fatalf("Bag.Count returned %d expected %d", count, 3)
	} else if size := bag.Len(); size != 5 {
		t.Fatalf("Bag.Len returned %d expected %d", count, 5)
	} else if list := bag.List(); len(list) != 2 {
		t.Fatalf("Bag.List returned %d expected %d", len(list), 2)
	} else if mode, freq := bag.Mode(); mode != id1 {
		t.Fatalf("Bag.Mode[0] returned %s expected %s", mode, id1)
	} else if freq != 3 {
		t.Fatalf("Bag.Mode[1] returned %d expected %d", freq, 3)
	} else if threshold := bag.Threshold(); threshold.Len() != 2 {
		t.Fatalf("Bag.Threshold returned %d expected %d", len(threshold), 2)
	}
}

func TestBagSetThreshold(t *testing.T) {
	id0 := Empty
	id1 := ID{1}

	bag := Bag{}

	bag.AddCount(id0, 2)
	bag.AddCount(id1, 3)

	bag.SetThreshold(0)

	if count := bag.Count(id0); count != 2 {
		t.Fatalf("Bag.Count returned %d expected %d", count, 2)
	} else if count := bag.Count(id1); count != 3 {
		t.Fatalf("Bag.Count returned %d expected %d", count, 3)
	} else if size := bag.Len(); size != 5 {
		t.Fatalf("Bag.Len returned %d expected %d", count, 5)
	} else if list := bag.List(); len(list) != 2 {
		t.Fatalf("Bag.List returned %d expected %d", len(list), 2)
	} else if mode, freq := bag.Mode(); mode != id1 {
		t.Fatalf("Bag.Mode[0] returned %s expected %s", mode, id1)
	} else if freq != 3 {
		t.Fatalf("Bag.Mode[1] returned %d expected %d", freq, 3)
	} else if threshold := bag.Threshold(); threshold.Len() != 2 {
		t.Fatalf("Bag.Threshold returned %d expected %d", len(threshold), 2)
	}

	bag.SetThreshold(3)

	if count := bag.Count(id0); count != 2 {
		t.Fatalf("Bag.Count returned %d expected %d", count, 2)
	} else if count := bag.Count(id1); count != 3 {
		t.Fatalf("Bag.Count returned %d expected %d", count, 3)
	} else if size := bag.Len(); size != 5 {
		t.Fatalf("Bag.Len returned %d expected %d", count, 5)
	} else if list := bag.List(); len(list) != 2 {
		t.Fatalf("Bag.List returned %d expected %d", len(list), 2)
	} else if mode, freq := bag.Mode(); mode != id1 {
		t.Fatalf("Bag.Mode[0] returned %s expected %s", mode, id1)
	} else if freq != 3 {
		t.Fatalf("Bag.Mode[1] returned %d expected %d", freq, 3)
	} else if threshold := bag.Threshold(); threshold.Len() != 1 {
		t.Fatalf("Bag.Threshold returned %d expected %d", len(threshold), 1)
	} else if !threshold.Contains(id1) {
		t.Fatalf("Bag.Threshold doesn't contain %s", id1)
	}
}

func TestBagFilter(t *testing.T) {
	id0 := Empty
	id1 := ID{1}
	id2 := ID{2}

	bag := Bag{}

	bag.AddCount(id0, 1)
	bag.AddCount(id1, 3)
	bag.AddCount(id2, 5)

	even := bag.Filter(0, 1, id0)

	if count := even.Count(id0); count != 1 {
		t.Fatalf("Bag.Count returned %d expected %d", count, 1)
	} else if count := even.Count(id1); count != 0 {
		t.Fatalf("Bag.Count returned %d expected %d", count, 0)
	} else if count := even.Count(id2); count != 5 {
		t.Fatalf("Bag.Count returned %d expected %d", count, 5)
	}
}

func TestBagSplit(t *testing.T) {
	id0 := Empty
	id1 := ID{1}
	id2 := ID{2}

	bag := Bag{}

	bag.AddCount(id0, 1)
	bag.AddCount(id1, 3)
	bag.AddCount(id2, 5)

	bags := bag.Split(0)

	evens := bags[0]
	odds := bags[1]

	if count := evens.Count(id0); count != 1 {
		t.Fatalf("Bag.Count returned %d expected %d", count, 1)
	} else if count := evens.Count(id1); count != 0 {
		t.Fatalf("Bag.Count returned %d expected %d", count, 0)
	} else if count := evens.Count(id2); count != 5 {
		t.Fatalf("Bag.Count returned %d expected %d", count, 5)
	} else if count := odds.Count(id0); count != 0 {
		t.Fatalf("Bag.Count returned %d expected %d", count, 0)
	} else if count := odds.Count(id1); count != 3 {
		t.Fatalf("Bag.Count returned %d expected %d", count, 3)
	} else if count := odds.Count(id2); count != 0 {
		t.Fatalf("Bag.Count returned %d expected %d", count, 0)
	}
}

func TestBagString(t *testing.T) {
	id0 := Empty

	bag := Bag{}

	bag.AddCount(id0, 1337)

	expected := "Bag: (Size = 1337)\n" +
		"    ID[11111111111111111111111111111111LpoYY]: Count = 1337"

	if bagString := bag.String(); bagString != expected {
		t.Fatalf("Bag.String:\nReturned:\n%s\nExpected:\n%s", bagString, expected)
	}
}

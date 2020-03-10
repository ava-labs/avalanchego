// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package ids

import (
	"testing"
)

func TestSet(t *testing.T) {
	id1 := NewID([32]byte{1})

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

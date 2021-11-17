// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package ids

import (
	"fmt"
	"testing"
)

func TestShortString(t *testing.T) {
	id := ShortID{1}

	xPrefixedID := id.PrefixedString("X-")
	pPrefixedID := id.PrefixedString("P-")

	newID, err := ShortFromPrefixedString(xPrefixedID, "X-")
	if err != nil {
		t.Fatal(err)
	}
	if newID != id {
		t.Fatalf("ShortFromPrefixedString did not produce the identical ID")
	}

	_, err = ShortFromPrefixedString(pPrefixedID, "X-")
	if err == nil {
		t.Fatal("Using the incorrect prefix did not cause an error")
	}

	tooLongPrefix := "hasnfaurnourieurn3eiur3nriu3nri34iurni34unr3iunrasfounaeouern3ur"
	_, err = ShortFromPrefixedString(xPrefixedID, tooLongPrefix)
	if err == nil {
		t.Fatal("Using the incorrect prefix did not cause an error")
	}
}

func TestIsUniqueShortIDs(t *testing.T) {
	ids := []ShortID{}
	if IsUniqueShortIDs(ids) == false {
		t.Fatal("should be unique")
	}
	id1 := GenerateTestShortID()
	ids = append(ids, id1)
	if IsUniqueShortIDs(ids) == false {
		t.Fatal("should be unique")
	}
	ids = append(ids, GenerateTestShortID())
	if IsUniqueShortIDs(ids) == false {
		t.Fatal("should be unique")
	}
	ids = append(ids, id1)
	if IsUniqueShortIDs(ids) == true {
		t.Fatal("should not be unique")
	}
}

func TestIsSortedAndUniqueShortIDs(t *testing.T) {
	id0 := ShortID{0}
	id1 := ShortID{1}
	id2 := ShortID{2}

	tests := []struct {
		arr      []ShortID
		isSorted bool
	}{
		{
			arr:      nil,
			isSorted: true,
		},
		{
			arr:      []ShortID{},
			isSorted: true,
		},
		{
			arr:      []ShortID{GenerateTestShortID()},
			isSorted: true,
		},
		{
			arr:      []ShortID{id0, id0},
			isSorted: false,
		},
		{
			arr:      []ShortID{id0, id1},
			isSorted: true,
		},
		{
			arr:      []ShortID{id1, id0},
			isSorted: false,
		},
		{
			arr:      []ShortID{id0, id1, id2},
			isSorted: true,
		},
		{
			arr:      []ShortID{id0, id1, id2, id2},
			isSorted: false,
		},
		{
			arr:      []ShortID{id0, id1, id1},
			isSorted: false,
		},
		{
			arr:      []ShortID{id0, id0, id1},
			isSorted: false,
		},
		{
			arr:      []ShortID{id2, id1, id0},
			isSorted: false,
		},
		{
			arr:      []ShortID{id2, id1, id2},
			isSorted: false,
		},
	}
	for _, test := range tests {
		t.Run(fmt.Sprintf("%v", test.arr), func(t *testing.T) {
			if test.isSorted {
				if !IsSortedAndUniqueShortIDs(test.arr) {
					t.Fatal("should have been marked as sorted and unique")
				}
			} else if IsSortedAndUniqueShortIDs(test.arr) {
				t.Fatal("shouldn't have been marked as sorted and unique")
			}
		})
	}
}

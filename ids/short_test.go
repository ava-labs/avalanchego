// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package ids

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
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

func TestShortIDMapMarshalling(t *testing.T) {
	originalMap := map[ShortID]int{
		{'e', 'v', 'a', ' ', 'l', 'a', 'b', 's'}: 1,
		{'a', 'v', 'a', ' ', 'l', 'a', 'b', 's'}: 2,
	}
	mapJSON, err := json.Marshal(originalMap)
	if err != nil {
		t.Fatal(err)
	}

	var unmarshalledMap map[ShortID]int
	err = json.Unmarshal(mapJSON, &unmarshalledMap)
	if err != nil {
		t.Fatal(err)
	}

	if len(originalMap) != len(unmarshalledMap) {
		t.Fatalf("wrong map lengths")
	}
	for originalID, num := range originalMap {
		if unmarshalledMap[originalID] != num {
			t.Fatalf("map was incorrectly Unmarshalled")
		}
	}
}

func TestShortIDsToStrings(t *testing.T) {
	shortIDs := []ShortID{{1}, {2}, {2}}
	expected := []string{"6HgC8KRBEhXYbF4riJyJFLSHt37UNuRt", "BaMPFdqMUQ46BV8iRcwbVfsam55kMqcp", "BaMPFdqMUQ46BV8iRcwbVfsam55kMqcp"}
	shortStrings := ShortIDsToStrings(shortIDs)
	require.EqualValues(t, expected, shortStrings)
}

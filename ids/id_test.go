// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package ids

import (
	"bytes"
	"reflect"
	"testing"
)

func TestID(t *testing.T) {
	id := ID{24}
	idCopy := ID{24}
	prefixed := id.Prefix(0)

	if id != idCopy {
		t.Fatalf("ID.Prefix mutated the ID")
	}
	if nextPrefix := id.Prefix(0); prefixed != nextPrefix {
		t.Fatalf("ID.Prefix not consistent")
	}
}

func TestIDBit(t *testing.T) {
	id0 := ID{1 << 0}
	id1 := ID{1 << 1}
	id2 := ID{1 << 2}
	id3 := ID{1 << 3}
	id4 := ID{1 << 4}
	id5 := ID{1 << 5}
	id6 := ID{1 << 6}
	id7 := ID{1 << 7}
	id8 := ID{0, 1 << 0}

	switch {
	case id0.Bit(0) != 1:
		t.Fatalf("Wrong bit")
	case id1.Bit(1) != 1:
		t.Fatalf("Wrong bit")
	case id2.Bit(2) != 1:
		t.Fatalf("Wrong bit")
	case id3.Bit(3) != 1:
		t.Fatalf("Wrong bit")
	case id4.Bit(4) != 1:
		t.Fatalf("Wrong bit")
	case id5.Bit(5) != 1:
		t.Fatalf("Wrong bit")
	case id6.Bit(6) != 1:
		t.Fatalf("Wrong bit")
	case id7.Bit(7) != 1:
		t.Fatalf("Wrong bit")
	case id8.Bit(8) != 1:
		t.Fatalf("Wrong bit")
	}
}

func TestFromString(t *testing.T) {
	id := ID{'a', 'v', 'a', ' ', 'l', 'a', 'b', 's'}
	idStr := id.String()
	id2, err := FromString(idStr)
	if err != nil {
		t.Fatal(err)
	}
	if id != id2 {
		t.Fatal("Expected FromString to be inverse of String but it wasn't")
	}
}

func TestIDFromStringError(t *testing.T) {
	tests := []struct {
		in string
	}{
		{""},
		{"foo"},
		{"foobar"},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			_, err := FromString(tt.in)
			if err == nil {
				t.Error("Unexpected success")
			}
		})
	}
}

func TestIDMarshalJSON(t *testing.T) {
	tests := []struct {
		label string
		in    ID
		out   []byte
		err   error
	}{
		{"ID{}", ID{}, []byte("\"11111111111111111111111111111111LpoYY\""), nil},
		{
			"ID(\"ava labs\")",
			ID{'a', 'v', 'a', ' ', 'l', 'a', 'b', 's'},
			[]byte("\"jvYi6Tn9idMi7BaymUVi9zWjg5tpmW7trfKG1AYJLKZJ2fsU7\""),
			nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.label, func(t *testing.T) {
			out, err := tt.in.MarshalJSON()
			if err != tt.err {
				t.Errorf("Expected err %s, got error %v", tt.err, err)
			} else if !bytes.Equal(out, tt.out) {
				t.Errorf("got %q, expected %q", out, tt.out)
			}
		})
	}
}

func TestIDUnmarshalJSON(t *testing.T) {
	tests := []struct {
		label string
		in    []byte
		out   ID
		err   error
	}{
		{"ID{}", []byte("null"), ID{}, nil},
		{
			"ID(\"ava labs\")",
			[]byte("\"jvYi6Tn9idMi7BaymUVi9zWjg5tpmW7trfKG1AYJLKZJ2fsU7\""),
			ID{'a', 'v', 'a', ' ', 'l', 'a', 'b', 's'},
			nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.label, func(t *testing.T) {
			foo := ID{}
			err := foo.UnmarshalJSON(tt.in)
			if err != tt.err {
				t.Errorf("Expected err %s, got error %v", tt.err, err)
			} else if foo != tt.out {
				t.Errorf("got %q, expected %q", foo, tt.out)
			}
		})
	}
}

func TestIDHex(t *testing.T) {
	id := ID{'a', 'v', 'a', ' ', 'l', 'a', 'b', 's'}
	expected := "617661206c616273000000000000000000000000000000000000000000000000"
	actual := id.Hex()
	if actual != expected {
		t.Fatalf("got %s, expected %s", actual, expected)
	}
}

func TestIDString(t *testing.T) {
	tests := []struct {
		label    string
		id       ID
		expected string
	}{
		{"ID{}", ID{}, "11111111111111111111111111111111LpoYY"},
		{"ID{24}", ID{24}, "Ba3mm8Ra8JYYebeZ9p7zw1ayorDbeD1euwxhgzSLsncKqGoNt"},
	}
	for _, tt := range tests {
		t.Run(tt.label, func(t *testing.T) {
			result := tt.id.String()
			if result != tt.expected {
				t.Errorf("got %q, expected %q", result, tt.expected)
			}
		})
	}
}

func TestSortIDs(t *testing.T) {
	ids := []ID{
		{'e', 'v', 'a', ' ', 'l', 'a', 'b', 's'},
		{'W', 'a', 'l', 'l', 'e', ' ', 'l', 'a', 'b', 's'},
		{'a', 'v', 'a', ' ', 'l', 'a', 'b', 's'},
	}
	SortIDs(ids)
	expected := []ID{
		{'W', 'a', 'l', 'l', 'e', ' ', 'l', 'a', 'b', 's'},
		{'a', 'v', 'a', ' ', 'l', 'a', 'b', 's'},
		{'e', 'v', 'a', ' ', 'l', 'a', 'b', 's'},
	}
	if !reflect.DeepEqual(ids, expected) {
		t.Fatal("[]ID was not sorted lexographically")
	}
}

func TestIsSortedAndUnique(t *testing.T) {
	unsorted := []ID{
		{'e', 'v', 'a', ' ', 'l', 'a', 'b', 's'},
		{'a', 'v', 'a', ' ', 'l', 'a', 'b', 's'},
	}
	if IsSortedAndUniqueIDs(unsorted) {
		t.Fatal("Wrongly accepted unsorted IDs")
	}
	duplicated := []ID{
		{'a', 'v', 'a', ' ', 'l', 'a', 'b', 's'},
		{'a', 'v', 'a', ' ', 'l', 'a', 'b', 's'},
	}
	if IsSortedAndUniqueIDs(duplicated) {
		t.Fatal("Wrongly accepted duplicated IDs")
	}
	sorted := []ID{
		{'a', 'v', 'a', ' ', 'l', 'a', 'b', 's'},
		{'e', 'v', 'a', ' ', 'l', 'a', 'b', 's'},
	}
	if !IsSortedAndUniqueIDs(sorted) {
		t.Fatal("Wrongly rejected sorted, unique IDs")
	}
}

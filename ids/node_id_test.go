// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package ids

import (
	"bytes"
	"encoding/json"
	"reflect"
	"testing"
)

func TestNodeIDEquality(t *testing.T) {
	id := NodeID{24}
	idCopy := NodeID{24}
	if id != idCopy {
		t.Fatalf("ID.Prefix mutated the ID")
	}
	id2 := NodeID{}
	if id == id2 {
		t.Fatal("expected Node IDs to be unequal")
	}
}

func TestNodeIDFromString(t *testing.T) {
	id := NodeID{'a', 'v', 'a', ' ', 'l', 'a', 'b', 's'}
	idStr := id.String()
	id2, err := NodeIDFromString(idStr)
	if err != nil {
		t.Fatal(err)
	}
	if id != id2 {
		t.Fatal("Expected FromString to be inverse of String but it wasn't")
	}
	expected := "NodeID-9tLMkeWFhWXd8QZc4rSiS5meuVXF5kRsz"
	if idStr != expected {
		t.Fatalf("expected %s but got %s", expected, idStr)
	}
}

func TestNodeIDFromStringError(t *testing.T) {
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

func TestNodeIDMarshalJSON(t *testing.T) {
	tests := []struct {
		label string
		in    NodeID
		out   []byte
		err   error
	}{
		{"NodeID{}", NodeID{}, []byte("\"NodeID-111111111111111111116DBWJs\""), nil},
		{
			"ID(\"ava labs\")",
			NodeID{'a', 'v', 'a', ' ', 'l', 'a', 'b', 's'},
			[]byte("\"NodeID-9tLMkeWFhWXd8QZc4rSiS5meuVXF5kRsz\""),
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

func TestNodeIDUnmarshalJSON(t *testing.T) {
	tests := []struct {
		label     string
		in        []byte
		out       NodeID
		shouldErr bool
	}{
		{"NodeID{}", []byte("null"), NodeID{}, false},
		{
			"NodeID(\"ava labs\")",
			[]byte("\"NodeID-9tLMkeWFhWXd8QZc4rSiS5meuVXF5kRsz\""),
			NodeID{'a', 'v', 'a', ' ', 'l', 'a', 'b', 's'},
			false,
		},
		{
			"missing start quote",
			[]byte("NodeID-9tLMkeWFhWXd8QZc4rSiS5meuVXF5kRsz\""),
			NodeID{},
			true,
		},
		{
			"missing end quote",
			[]byte("\"NodeID-9tLMkeWFhWXd8QZc4rSiS5meuVXF5kRsz"),
			NodeID{},
			true,
		},
		{
			"NodeID-",
			[]byte("\"NodeID-\""),
			NodeID{},
			true,
		},
		{
			"NodeID-1",
			[]byte("\"NodeID-1\""),
			NodeID{},
			true,
		},
		{
			"NodeID-9tLMkeWFhWXd8QZc4rSiS5meuVXF5kRsz1",
			[]byte("\"NodeID-1\""),
			NodeID{},
			true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.label, func(t *testing.T) {
			foo := NodeID{}
			err := foo.UnmarshalJSON(tt.in)
			switch {
			case err == nil && tt.shouldErr:
				t.Errorf("Expected no error but got error %v", err)
			case err != nil && !tt.shouldErr:
				t.Errorf("unxpected error: %v", err)
			case foo != tt.out:
				t.Errorf("got %q, expected %q", foo, tt.out)
			}
		})
	}
}

func TestNodeIDString(t *testing.T) {
	tests := []struct {
		label    string
		id       NodeID
		expected string
	}{
		{"NodeID{}", NodeID{}, "NodeID-111111111111111111116DBWJs"},
		{"NodeID{24}", NodeID{24}, "NodeID-3BuDc2d1Efme5Apba6SJ8w3Tz7qeh6mHt"},
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

func TestSortNodeIDs(t *testing.T) {
	ids := []NodeID{
		{'e', 'v', 'a', ' ', 'l', 'a', 'b', 's'},
		{'W', 'a', 'l', 'l', 'e', ' ', 'l', 'a', 'b', 's'},
		{'a', 'v', 'a', ' ', 'l', 'a', 'b', 's'},
	}
	SortNodeIDs(ids)
	expected := []NodeID{
		{'W', 'a', 'l', 'l', 'e', ' ', 'l', 'a', 'b', 's'},
		{'a', 'v', 'a', ' ', 'l', 'a', 'b', 's'},
		{'e', 'v', 'a', ' ', 'l', 'a', 'b', 's'},
	}
	if !reflect.DeepEqual(ids, expected) {
		t.Fatal("[]NodeID was not sorted lexographically")
	}
}

func TestNodeIDMapMarshalling(t *testing.T) {
	originalMap := map[NodeID]int{
		{'e', 'v', 'a', ' ', 'l', 'a', 'b', 's'}: 1,
		{'a', 'v', 'a', ' ', 'l', 'a', 'b', 's'}: 2,
	}
	mapJSON, err := json.Marshal(originalMap)
	if err != nil {
		t.Fatal(err)
	}

	var unmarshalledMap map[NodeID]int
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

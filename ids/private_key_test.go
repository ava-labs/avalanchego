// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package ids

import (
	//"bytes"
	//"encoding/json"
	//"reflect"
	"testing"
	"github.com/stretchr/testify/assert"
)

func TestPrivateKeyEquality(t *testing.T) {
    assert := assert.New(t)
	pk := PrivateKey{24}
	pkCopy := PrivateKey{24}
	assert.Equal(pk, pkCopy)
	pk2 := PrivateKey{}
	assert.NotEqual(pk, pk2)
}
func TestPrivateKeyFromString(t *testing.T) {
    assert := assert.New(t)
	pk := PrivateKey{'a', 'v', 'a', ' ', 'l', 'a', 'b', 's'}
	pkStr := pk.String()
	pk2, err := PrivateKeyFromString(pkStr)
    assert.NoError(err)
	assert.Equal(pk, pk2)
    expected := "PrivateKey-2qg4x8qM2s2qGNSXG"
    assert.Equal(expected, pkStr)
}

func TestPrivateKeyFromStringError(t *testing.T) {
    assert := assert.New(t)
	tests := []struct {
		in string
	}{
		{""},
		{"foo"},
		{"foobar"},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			_, err := PrivateKeyFromString(tt.in)
            assert.Error(err)
		})
	}
}

func TestPrivateKeyMarshalJSON(t *testing.T) {
    assert := assert.New(t)
	tests := []struct {
		label string
		in    PrivateKey
		out   []byte
		err   error
	}{
		{"PrivateKey{}", PrivateKey{}, []byte("\"PrivateKey-45PJLL\""), nil},
		{
			"PrivateKey(\"ava labs\")",
			PrivateKey{'a', 'v', 'a', ' ', 'l', 'a', 'b', 's'},
			[]byte("\"PrivateKey-2qg4x8qM2s2qGNSXG\""),
			nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.label, func(t *testing.T) {
			out, err := tt.in.MarshalJSON()
            assert.Equal(err, tt.err)
            assert.Equal(out, tt.out)
		})
	}
}
/*

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
*/

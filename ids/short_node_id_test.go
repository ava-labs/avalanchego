// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package ids

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/utils/cb58"
)

func TestNodeIDEquality(t *testing.T) {
	require := require.New(t)

	id := ShortNodeID{24}
	idCopy := ShortNodeID{24}
	require.Equal(id, idCopy)
	id2 := EmptyNodeID
	require.NotEqual(id, id2)
}

func TestShortIDBytesAndWritableIsolation(t *testing.T) {
	require := require.New(t)

	id := ShortID{24}
	idBytes := id.Bytes()
	idBytes[0] = 25
	require.Equal(ShortID{24}, id)

	idBytes = WritableShort(&id)
	idBytes[0] = 25
	require.Equal(ShortID{25}, id)
}

func TestNodeIDBytesAndWritableIsolation(t *testing.T) {
	require := require.New(t)

	id := ShortNodeID{24}
	idBytes := id.Bytes()
	idBytes[0] = 25
	require.Equal(ShortNodeID{24}, id)

	idBytes = WritableNode(&id)
	idBytes[0] = 25
	require.Equal(ShortNodeID{25}, id)
}

func TestNodeIDFromString(t *testing.T) {
	require := require.New(t)

	id := ShortNodeID{'a', 'v', 'a', ' ', 'l', 'a', 'b', 's'}
	idStr := id.String()
	id2, err := NodeIDFromString(idStr)
	require.NoError(err)
	require.Equal(id, id2)
	expected := "NodeID-9tLMkeWFhWXd8QZc4rSiS5meuVXF5kRsz"
	require.Equal(expected, idStr)
}

func TestNodeIDFromStringError(t *testing.T) {
	tests := []struct {
		in          string
		expectedErr error
	}{
		{
			in:          "",
			expectedErr: cb58.ErrBase58Decoding,
		},
		{
			in:          "foo",
			expectedErr: cb58.ErrMissingChecksum,
		},
		{
			in:          "foobar",
			expectedErr: cb58.ErrBadChecksum,
		},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			_, err := FromString(tt.in)
			require.ErrorIs(t, err, tt.expectedErr)
		})
	}
}

func TestNodeIDMarshalJSON(t *testing.T) {
	tests := []struct {
		label string
		in    ShortNodeID
		out   []byte
		err   error
	}{
		{"NodeID{}", EmptyNodeID, []byte("\"NodeID-111111111111111111116DBWJs\""), nil},
		{
			"ID(\"ava labs\")",
			ShortNodeID{'a', 'v', 'a', ' ', 'l', 'a', 'b', 's'},
			[]byte("\"NodeID-9tLMkeWFhWXd8QZc4rSiS5meuVXF5kRsz\""),
			nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.label, func(t *testing.T) {
			require := require.New(t)

			out, err := tt.in.MarshalJSON()
			require.ErrorIs(err, tt.err)
			require.Equal(tt.out, out)
		})
	}
}

func TestNodeIDUnmarshalJSON(t *testing.T) {
	tests := []struct {
		label       string
		in          []byte
		out         ShortNodeID
		expectedErr error
	}{
		{"NodeID{}", []byte("null"), EmptyNodeID, nil},
		{
			"NodeID(\"ava labs\")",
			[]byte("\"NodeID-9tLMkeWFhWXd8QZc4rSiS5meuVXF5kRsz\""),
			ShortNodeID{'a', 'v', 'a', ' ', 'l', 'a', 'b', 's'},
			nil,
		},
		{
			"missing start quote",
			[]byte("NodeID-9tLMkeWFhWXd8QZc4rSiS5meuVXF5kRsz\""),
			EmptyNodeID,
			errMissingQuotes,
		},
		{
			"missing end quote",
			[]byte("\"NodeID-9tLMkeWFhWXd8QZc4rSiS5meuVXF5kRsz"),
			EmptyNodeID,
			errMissingQuotes,
		},
		{
			"NodeID-",
			[]byte("\"NodeID-\""),
			EmptyNodeID,
			errShortNodeID,
		},
		{
			"NodeID-1",
			[]byte("\"NodeID-1\""),
			EmptyNodeID,
			cb58.ErrMissingChecksum,
		},
		{
			"NodeID-9tLMkeWFhWXd8QZc4rSiS5meuVXF5kRsz1",
			[]byte("\"NodeID-1\""),
			EmptyNodeID,
			cb58.ErrMissingChecksum,
		},
	}
	for _, tt := range tests {
		t.Run(tt.label, func(t *testing.T) {
			require := require.New(t)

			foo := EmptyNodeID
			err := foo.UnmarshalJSON(tt.in)
			require.ErrorIs(err, tt.expectedErr)
			require.Equal(tt.out, foo)
		})
	}
}

func TestNodeIDString(t *testing.T) {
	tests := []struct {
		label    string
		id       ShortNodeID
		expected string
	}{
		{"NodeID{}", EmptyNodeID, "NodeID-111111111111111111116DBWJs"},
		{"NodeID{24}", ShortNodeID{24}, "NodeID-3BuDc2d1Efme5Apba6SJ8w3Tz7qeh6mHt"},
	}
	for _, tt := range tests {
		t.Run(tt.label, func(t *testing.T) {
			require.Equal(t, tt.expected, tt.id.String())
		})
	}
}

func TestNodeIDMapMarshalling(t *testing.T) {
	require := require.New(t)

	originalMap := map[ShortNodeID]int{
		{'e', 'v', 'a', ' ', 'l', 'a', 'b', 's'}: 1,
		{'a', 'v', 'a', ' ', 'l', 'a', 'b', 's'}: 2,
	}
	mapJSON, err := json.Marshal(originalMap)
	require.NoError(err)

	var unmarshalledMap map[ShortNodeID]int
	require.NoError(json.Unmarshal(mapJSON, &unmarshalledMap))
	require.Equal(originalMap, unmarshalledMap)
}

func TestNodeIDLess(t *testing.T) {
	require := require.New(t)

	id1 := EmptyNodeID
	id2 := EmptyNodeID
	require.False(id1.Less(id2))
	require.False(id2.Less(id1))

	id1 = ShortNodeID{1}
	id2 = EmptyNodeID
	require.False(id1.Less(id2))
	require.True(id2.Less(id1))

	id1 = ShortNodeID{1}
	id2 = ShortNodeID{1}
	require.False(id1.Less(id2))
	require.False(id2.Less(id1))

	id1 = ShortNodeID{1}
	id2 = ShortNodeID{1, 2}
	require.True(id1.Less(id2))
	require.False(id2.Less(id1))
}

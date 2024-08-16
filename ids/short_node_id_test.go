// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package ids

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/utils/cb58"
)

func TestShortNodeIDEquality(t *testing.T) {
	require := require.New(t)

	id := ShortNodeID{24}
	idCopy := ShortNodeID{24}
	require.Equal(id, idCopy)
	id2 := EmptyShortNodeID
	require.NotEqual(id, id2)
}

func TestShortIDBytesIsolation(t *testing.T) {
	require := require.New(t)

	id := ShortID{24}
	idBytes := id.Bytes()
	idBytes[0] = 25
	require.Equal(ShortID{24}, id)
}

func TestShortNodeIDBytesIsolation(t *testing.T) {
	require := require.New(t)

	id := ShortNodeID{24}
	idBytes := id.Bytes()
	idBytes[0] = 25
	require.Equal(ShortNodeID{24}, id)
}

func TestShortNodeIDFromString(t *testing.T) {
	require := require.New(t)

	id := ShortNodeID{'a', 'v', 'a', ' ', 'l', 'a', 'b', 's'}
	idStr := id.String()
	id2, err := ShortNodeIDFromString(idStr)
	require.NoError(err)
	require.Equal(id, id2)
	expected := "NodeID-9tLMkeWFhWXd8QZc4rSiS5meuVXF5kRsz"
	require.Equal(expected, idStr)
}

func TestShortNodeIDFromStringError(t *testing.T) {
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

func TestShortNodeIDMarshalJSON(t *testing.T) {
	tests := []struct {
		label string
		in    ShortNodeID
		out   []byte
		err   error
	}{
		{
			"NodeID{}",
			ShortNodeID{},
			[]byte(`"NodeID-111111111111111111116DBWJs"`),
			nil,
		},
		{
			`ID("ava labs")`,
			ShortNodeID{'a', 'v', 'a', ' ', 'l', 'a', 'b', 's'},
			[]byte(`"NodeID-9tLMkeWFhWXd8QZc4rSiS5meuVXF5kRsz"`),
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

func TestShortNodeIDUnmarshalJSON(t *testing.T) {
	tests := []struct {
		label       string
		in          []byte
		out         ShortNodeID
		expectedErr error
	}{
		{
			"NodeID{}",
			[]byte("null"),
			ShortNodeID{},
			nil,
		},
		{
			`NodeID("ava labs")`,
			[]byte(`"NodeID-9tLMkeWFhWXd8QZc4rSiS5meuVXF5kRsz"`),
			ShortNodeID{'a', 'v', 'a', ' ', 'l', 'a', 'b', 's'},
			nil,
		},
		{
			"missing start quote",
			[]byte(`NodeID-9tLMkeWFhWXd8QZc4rSiS5meuVXF5kRsz"`),
			ShortNodeID{},
			errMissingQuotes,
		},
		{
			"missing end quote",
			[]byte(`"NodeID-9tLMkeWFhWXd8QZc4rSiS5meuVXF5kRsz`),
			ShortNodeID{},
			errMissingQuotes,
		},
		{
			"NodeID-",
			[]byte(`"NodeID-"`),
			ShortNodeID{},
			errShortNodeID,
		},
		{
			"NodeID-1",
			[]byte(`"NodeID-1"`),
			ShortNodeID{},
			cb58.ErrMissingChecksum,
		},
		{
			"NodeID-9tLMkeWFhWXd8QZc4rSiS5meuVXF5kRsz1",
			[]byte(`"NodeID-1"`),
			ShortNodeID{},
			cb58.ErrMissingChecksum,
		},
	}
	for _, tt := range tests {
		t.Run(tt.label, func(t *testing.T) {
			require := require.New(t)

			foo := ShortNodeID{}
			err := foo.UnmarshalJSON(tt.in)
			require.ErrorIs(err, tt.expectedErr)
			require.Equal(tt.out, foo)
		})
	}
}

func TestShortNodeIDString(t *testing.T) {
	tests := []struct {
		label    string
		id       ShortNodeID
		expected string
	}{
		{"NodeID{}", EmptyShortNodeID, "NodeID-111111111111111111116DBWJs"},
		{"NodeID{24}", ShortNodeID{24}, "NodeID-3BuDc2d1Efme5Apba6SJ8w3Tz7qeh6mHt"},
	}
	for _, tt := range tests {
		t.Run(tt.label, func(t *testing.T) {
			require.Equal(t, tt.expected, tt.id.String())
		})
	}
}

func TestShortNodeIDMapMarshalling(t *testing.T) {
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

func TestShortNodeIDCompare(t *testing.T) {
	tests := []struct {
		a        ShortNodeID
		b        ShortNodeID
		expected int
	}{
		{
			a:        ShortNodeID{1},
			b:        ShortNodeID{0},
			expected: 1,
		},
		{
			a:        ShortNodeID{1},
			b:        ShortNodeID{1},
			expected: 0,
		},
		{
			a:        ShortNodeID{1, 0},
			b:        ShortNodeID{1, 2},
			expected: -1,
		},
	}
	for _, test := range tests {
		t.Run(fmt.Sprintf("%s_%s_%d", test.a, test.b, test.expected), func(t *testing.T) {
			require := require.New(t)

			require.Equal(test.expected, test.a.Compare(test.b))
			require.Equal(-test.expected, test.b.Compare(test.a))
		})
	}
}

func TestShortNodeIDToShortNodeIDConversion(t *testing.T) {
	require := require.New(t)

	// EmptyNodeID --> EmptyShortNodeID
	outputShort, err := ShortNodeIDFromNodeID(EmptyNodeID)
	require.NoError(err)
	require.Equal(EmptyShortNodeID, outputShort)

	// EmptyShortNodeID --> EmptyNodeID
	output := NodeIDFromShortNodeID(EmptyShortNodeID)
	require.Equal(EmptyNodeID, output)
}

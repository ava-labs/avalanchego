// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package ids

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/utils/cb58"
)

func TestNodeIDEquality(t *testing.T) {
	require := require.New(t)

	id := NodeID{24}
	idCopy := NodeID{24}
	require.Equal(id, idCopy)
	id2 := NodeID{}
	require.NotEqual(id, id2)
}

func TestNodeIDFromString(t *testing.T) {
	require := require.New(t)

	id := NodeID{'a', 'v', 'a', ' ', 'l', 'a', 'b', 's'}
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
		in    NodeID
		out   []byte
		err   error
	}{
		{
			"NodeID{}",
			NodeID{},
			[]byte(`"NodeID-111111111111111111116DBWJs"`),
			nil,
		},
		{
			`ID("ava labs")`,
			NodeID{'a', 'v', 'a', ' ', 'l', 'a', 'b', 's'},
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

func TestNodeIDUnmarshalJSON(t *testing.T) {
	tests := []struct {
		label       string
		in          []byte
		out         NodeID
		expectedErr error
	}{
		{
			"NodeID{}",
			[]byte("null"),
			NodeID{},
			nil,
		},
		{
			`NodeID("ava labs")`,
			[]byte(`"NodeID-9tLMkeWFhWXd8QZc4rSiS5meuVXF5kRsz"`),
			NodeID{'a', 'v', 'a', ' ', 'l', 'a', 'b', 's'},
			nil,
		},
		{
			"missing start quote",
			[]byte(`NodeID-9tLMkeWFhWXd8QZc4rSiS5meuVXF5kRsz"`),
			NodeID{},
			errMissingQuotes,
		},
		{
			"missing end quote",
			[]byte(`"NodeID-9tLMkeWFhWXd8QZc4rSiS5meuVXF5kRsz`),
			NodeID{},
			errMissingQuotes,
		},
		{
			"NodeID-",
			[]byte(`"NodeID-"`),
			NodeID{},
			errShortNodeID,
		},
		{
			"NodeID-1",
			[]byte(`"NodeID-1"`),
			NodeID{},
			cb58.ErrMissingChecksum,
		},
		{
			"NodeID-9tLMkeWFhWXd8QZc4rSiS5meuVXF5kRsz1",
			[]byte(`"NodeID-1"`),
			NodeID{},
			cb58.ErrMissingChecksum,
		},
	}
	for _, tt := range tests {
		t.Run(tt.label, func(t *testing.T) {
			require := require.New(t)

			foo := NodeID{}
			err := foo.UnmarshalJSON(tt.in)
			require.ErrorIs(err, tt.expectedErr)
			require.Equal(tt.out, foo)
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
			require.Equal(t, tt.expected, tt.id.String())
		})
	}
}

func TestNodeIDMapMarshalling(t *testing.T) {
	require := require.New(t)

	originalMap := map[NodeID]int{
		{'e', 'v', 'a', ' ', 'l', 'a', 'b', 's'}: 1,
		{'a', 'v', 'a', ' ', 'l', 'a', 'b', 's'}: 2,
	}
	mapJSON, err := json.Marshal(originalMap)
	require.NoError(err)

	var unmarshalledMap map[NodeID]int
	require.NoError(json.Unmarshal(mapJSON, &unmarshalledMap))
	require.Equal(originalMap, unmarshalledMap)
}

func TestNodeIDCompare(t *testing.T) {
	tests := []struct {
		a        NodeID
		b        NodeID
		expected int
	}{
		{
			a:        NodeID{1},
			b:        NodeID{0},
			expected: 1,
		},
		{
			a:        NodeID{1},
			b:        NodeID{1},
			expected: 0,
		},
		{
			a:        NodeID{1, 0},
			b:        NodeID{1, 2},
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

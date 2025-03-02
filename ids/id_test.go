// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package ids

import (
	"encoding/json"
	"fmt"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/cb58"
	"github.com/ava-labs/avalanchego/utils/hashing"
)

func TestID(t *testing.T) {
	require := require.New(t)

	id := ID{24}
	idCopy := ID{24}
	prefixed := id.Prefix(0)

	require.Equal(idCopy, id)
	require.Equal(prefixed, id.Prefix(0))
}

func TestIDPrefix(t *testing.T) {
	id := GenerateTestID()
	tests := []struct {
		name             string
		id               ID
		prefix           []uint64
		expectedPreimage []byte
	}{
		{
			name:             "empty prefix",
			id:               id,
			prefix:           []uint64{},
			expectedPreimage: id[:],
		},
		{
			name:   "1 prefix",
			id:     id,
			prefix: []uint64{1},
			expectedPreimage: slices.Concat(
				[]byte{0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x01},
				id[:],
			),
		},
		{
			name:   "multiple prefixes",
			id:     id,
			prefix: []uint64{1, 256},
			expectedPreimage: slices.Concat(
				[]byte{0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x01},
				[]byte{0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x01, 0x00},
				id[:],
			),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			expected := ID(hashing.ComputeHash256Array(test.expectedPreimage))
			require.Equal(t, expected, test.id.Prefix(test.prefix...))
		})
	}
}

func TestIDAppend(t *testing.T) {
	id := GenerateTestID()
	tests := []struct {
		name             string
		id               ID
		suffix           []uint32
		expectedPreimage []byte
	}{
		{
			name:             "empty suffix",
			id:               id,
			suffix:           []uint32{},
			expectedPreimage: id[:],
		},
		{
			name:   "1 suffix",
			id:     id,
			suffix: []uint32{1},
			expectedPreimage: slices.Concat(
				id[:],
				[]byte{0x00, 0x00, 0x00, 0x01},
			),
		},
		{
			name:   "multiple suffixes",
			id:     id,
			suffix: []uint32{1, 256},
			expectedPreimage: slices.Concat(
				id[:],
				[]byte{0x00, 0x00, 0x00, 0x01},
				[]byte{0x00, 0x00, 0x01, 0x00},
			),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			expected := ID(hashing.ComputeHash256Array(test.expectedPreimage))
			require.Equal(t, expected, test.id.Append(test.suffix...))
		})
	}
}

func TestIDXOR(t *testing.T) {
	require := require.New(t)

	id1 := ID{1}
	id3 := ID{3}

	require.Equal(ID{2}, id1.XOR(id3))
	require.Equal(ID{1}, id1)
}

func TestIDBit(t *testing.T) {
	require := require.New(t)

	id0 := ID{1 << 0}
	id1 := ID{1 << 1}
	id2 := ID{1 << 2}
	id3 := ID{1 << 3}
	id4 := ID{1 << 4}
	id5 := ID{1 << 5}
	id6 := ID{1 << 6}
	id7 := ID{1 << 7}
	id8 := ID{0, 1 << 0}

	require.Equal(1, id0.Bit(0))
	require.Equal(1, id1.Bit(1))
	require.Equal(1, id2.Bit(2))
	require.Equal(1, id3.Bit(3))
	require.Equal(1, id4.Bit(4))
	require.Equal(1, id5.Bit(5))
	require.Equal(1, id6.Bit(6))
	require.Equal(1, id7.Bit(7))
	require.Equal(1, id8.Bit(8))
}

func TestFromString(t *testing.T) {
	require := require.New(t)

	id := ID{'a', 'v', 'a', ' ', 'l', 'a', 'b', 's'}
	idStr := id.String()
	id2, err := FromString(idStr)
	require.NoError(err)
	require.Equal(id, id2)
}

func TestIDFromStringError(t *testing.T) {
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

func TestIDMarshalJSON(t *testing.T) {
	tests := []struct {
		label string
		in    ID
		out   []byte
		err   error
	}{
		{
			"ID{}",
			ID{},
			[]byte(`"11111111111111111111111111111111LpoYY"`),
			nil,
		},
		{
			`ID("ava labs")`,
			ID{'a', 'v', 'a', ' ', 'l', 'a', 'b', 's'},
			[]byte(`"jvYi6Tn9idMi7BaymUVi9zWjg5tpmW7trfKG1AYJLKZJ2fsU7"`),
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

func TestIDUnmarshalJSON(t *testing.T) {
	tests := []struct {
		label string
		in    []byte
		out   ID
		err   error
	}{
		{
			"ID{}",
			[]byte("null"),
			ID{},
			nil,
		},
		{
			`ID("ava labs")`,
			[]byte(`"jvYi6Tn9idMi7BaymUVi9zWjg5tpmW7trfKG1AYJLKZJ2fsU7"`),
			ID{'a', 'v', 'a', ' ', 'l', 'a', 'b', 's'},
			nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.label, func(t *testing.T) {
			require := require.New(t)

			foo := ID{}
			err := foo.UnmarshalJSON(tt.in)
			require.ErrorIs(err, tt.err)
			require.Equal(tt.out, foo)
		})
	}
}

func TestIDHex(t *testing.T) {
	id := ID{'a', 'v', 'a', ' ', 'l', 'a', 'b', 's'}
	expected := "617661206c616273000000000000000000000000000000000000000000000000"
	require.Equal(t, expected, id.Hex())
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
			require.Equal(t, tt.expected, tt.id.String())
		})
	}
}

func TestSortIDs(t *testing.T) {
	ids := []ID{
		{'e', 'v', 'a', ' ', 'l', 'a', 'b', 's'},
		{'W', 'a', 'l', 'l', 'e', ' ', 'l', 'a', 'b', 's'},
		{'a', 'v', 'a', ' ', 'l', 'a', 'b', 's'},
	}
	utils.Sort(ids)
	expected := []ID{
		{'W', 'a', 'l', 'l', 'e', ' ', 'l', 'a', 'b', 's'},
		{'a', 'v', 'a', ' ', 'l', 'a', 'b', 's'},
		{'e', 'v', 'a', ' ', 'l', 'a', 'b', 's'},
	}
	require.Equal(t, expected, ids)
}

func TestIDMapMarshalling(t *testing.T) {
	require := require.New(t)

	originalMap := map[ID]int{
		{'e', 'v', 'a', ' ', 'l', 'a', 'b', 's'}: 1,
		{'a', 'v', 'a', ' ', 'l', 'a', 'b', 's'}: 2,
	}
	mapJSON, err := json.Marshal(originalMap)
	require.NoError(err)

	var unmarshalledMap map[ID]int
	require.NoError(json.Unmarshal(mapJSON, &unmarshalledMap))

	require.Equal(originalMap, unmarshalledMap)
}

func TestIDCompare(t *testing.T) {
	tests := []struct {
		a        ID
		b        ID
		expected int
	}{
		{
			a:        ID{1},
			b:        ID{0},
			expected: 1,
		},
		{
			a:        ID{1},
			b:        ID{1},
			expected: 0,
		},
		{
			a:        ID{1, 0},
			b:        ID{1, 2},
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

// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package predicate

import (
	"bytes"
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/types"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/utils/set"
)

func TestNew(t *testing.T) {
	tests := []struct {
		name  string
		input []byte
		want  Predicate
	}{
		{
			name:  "empty_predicate",
			input: nil,
			want: Predicate{
				{delimiter},
			},
		},
		{
			name:  "single_byte",
			input: []byte{0x42},
			want: Predicate{
				{0x42, delimiter},
			},
		},
		{
			name:  "31_bytes",
			input: make([]byte, 31),
			want: Predicate{
				{31: delimiter},
			},
		},
		{
			name:  "32_bytes",
			input: make([]byte, 32),
			want: Predicate{
				{},
				{delimiter},
			},
		},
		{
			name: "40_bytes",
			input: []byte{
				0x00, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07,
				0x10, 0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17,
				0x20, 0x21, 0x22, 0x23, 0x24, 0x25, 0x26, 0x27,
				0x30, 0x31, 0x32, 0x33, 0x34, 0x35, 0x36, 0x37,
				0x40, 0x41, 0x42, 0x43, 0x44, 0x45, 0x46, 0x47,
			},
			want: Predicate{
				{
					0x00, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07,
					0x10, 0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17,
					0x20, 0x21, 0x22, 0x23, 0x24, 0x25, 0x26, 0x27,
					0x30, 0x31, 0x32, 0x33, 0x34, 0x35, 0x36, 0x37,
				},
				{
					0x40, 0x41, 0x42, 0x43, 0x44, 0x45, 0x46, 0x47,
					delimiter,
				},
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := New(test.input)
			require.Equal(t, test.want, got)
		})
	}
}

func TestPredicateBytes(t *testing.T) {
	tests := []struct {
		name    string
		input   Predicate
		want    []byte
		wantErr error
	}{
		// Valid test cases
		{
			name:    "empty_input",
			input:   New(nil),
			want:    []byte{},
			wantErr: nil,
		},
		{
			name:    "single_byte",
			input:   New([]byte{0xbb}),
			want:    []byte{0xbb},
			wantErr: nil,
		},
		{
			name:    "31_bytes",
			input:   New(bytes.Repeat([]byte{0xaa}, 31)),
			want:    bytes.Repeat([]byte{0xaa}, 31),
			wantErr: nil,
		},
		{
			name:    "32_bytes",
			input:   New(bytes.Repeat([]byte{0xdd}, 32)),
			want:    bytes.Repeat([]byte{0xdd}, 32),
			wantErr: nil,
		},
		{
			name:    "33_bytes",
			input:   New(bytes.Repeat([]byte{0xcc}, 33)),
			want:    bytes.Repeat([]byte{0xcc}, 33),
			wantErr: nil,
		},
		{
			name:    "48_bytes",
			input:   New(bytes.Repeat([]byte{0x00}, 48)),
			want:    bytes.Repeat([]byte{0x00}, 48),
			wantErr: nil,
		},
		{
			name:    "63_bytes",
			input:   New(bytes.Repeat([]byte{0xdd}, 63)),
			want:    bytes.Repeat([]byte{0xdd}, 63),
			wantErr: nil,
		},
		{
			name:    "64_bytes",
			input:   New(bytes.Repeat([]byte{0x33}, 64)),
			want:    bytes.Repeat([]byte{0x33}, 64),
			wantErr: nil,
		},
		{
			name:    "65_bytes",
			input:   New(bytes.Repeat([]byte{0xdd}, 65)),
			want:    bytes.Repeat([]byte{0xdd}, 65),
			wantErr: nil,
		},
		// Invalid test cases
		{
			name:    "all_zeros_empty",
			input:   nil,
			want:    nil,
			wantErr: errMissingDelimiter,
		},
		{
			name:    "all_zeros",
			input:   Predicate{{}},
			want:    nil,
			wantErr: errMissingDelimiter,
		},
		{
			name:    "wrong_delimiter",
			input:   Predicate{{0x42}},
			want:    nil,
			wantErr: errWrongDelimiter,
		},
		{
			name:    "wrong_delimiter",
			input:   Predicate{{delimiter}, {}},
			want:    nil,
			wantErr: errExcessPadding,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)

			unpacked, err := tt.input.Bytes()
			require.ErrorIs(err, tt.wantErr)
			require.Equal(tt.want, unpacked)
		})
	}
}

func FuzzNewBytesEqual(f *testing.F) {
	f.Fuzz(func(t *testing.T, input []byte) {
		packed := New(input)
		unpacked, err := packed.Bytes()
		require.NoError(t, err)
		require.Equal(t, input, unpacked)
	})
}

func FuzzBytesNewEqual(f *testing.F) {
	f.Fuzz(func(t *testing.T, original []byte) {
		var predicate Predicate
		for i := 0; i < len(original); i += common.HashLength {
			var key common.Hash
			copy(key[:], original[i:])
			predicate = append(predicate, key)
		}

		unpacked, err := predicate.Bytes()
		if err != nil {
			t.Skip("invalid predicate")
		}

		repacked := New(unpacked)
		require.Equal(t, predicate, repacked)
	})
}

type allowSet set.Set[common.Address]

func (a allowSet) HasPredicate(addr common.Address) bool {
	_, ok := a[addr]
	return ok
}

func TestFromAccessList(t *testing.T) {
	addrA := common.Address{0xAA}
	addrB := common.Address{0xBB}
	addrC := common.Address{0xCC}

	h1 := common.Hash{1}
	h2 := common.Hash{2}
	h3 := common.Hash{3}
	h4 := common.Hash{4}

	tests := []struct {
		name  string
		rules set.Set[common.Address]
		list  types.AccessList
		want  map[common.Address][]Predicate
	}{
		{
			name:  "empty_list",
			rules: set.Of(addrA),
			list:  types.AccessList{},
			want:  map[common.Address][]Predicate{},
		},
		{
			name:  "no_allowed_addresses",
			rules: set.Of(addrB),
			list:  types.AccessList{{Address: addrA, StorageKeys: []common.Hash{h1}}},
			want:  map[common.Address][]Predicate{},
		},
		{
			name:  "single_tuple_allowed",
			rules: set.Of(addrA),
			list:  types.AccessList{{Address: addrA, StorageKeys: []common.Hash{h1, h2}}},
			want:  map[common.Address][]Predicate{addrA: {{h1, h2}}},
		},
		{
			name:  "repeated_address_accumulates",
			rules: set.Of(addrA),
			list: types.AccessList{
				{Address: addrA, StorageKeys: []common.Hash{h1, h2}},
				{Address: addrA, StorageKeys: []common.Hash{h3}},
			},
			want: map[common.Address][]Predicate{addrA: {{h1, h2}, {h3}}},
		},
		{
			name:  "mixed_addresses_filtered",
			rules: set.Of(addrA, addrC),
			list: types.AccessList{
				{Address: addrA, StorageKeys: []common.Hash{h1}},
				{Address: addrB, StorageKeys: []common.Hash{h2}},
				{Address: addrC, StorageKeys: []common.Hash{h3, h4}},
			},
			want: map[common.Address][]Predicate{
				addrA: {{h1}},
				addrC: {{h3, h4}},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := require.New(t)
			got := FromAccessList(allowSet(tc.rules), tc.list)
			req.Equal(tc.want, got)
		})
	}
}

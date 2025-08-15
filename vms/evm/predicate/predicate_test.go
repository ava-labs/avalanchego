// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package predicate_test

import (
	"bytes"
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/types"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/vms/evm/predicate/predicatetest"

	. "github.com/ava-labs/avalanchego/vms/evm/predicate"
)

type allowSet map[common.Address]bool

func (a allowSet) HasPredicate(addr common.Address) bool { return a[addr] }

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
		rules allowSet
		list  types.AccessList
		want  map[common.Address][]Predicate
	}{
		{
			name:  "empty list",
			rules: allowSet{addrA: true},
			list:  types.AccessList{},
			want:  map[common.Address][]Predicate{},
		},
		{
			name:  "no allowed addresses",
			rules: allowSet{addrB: true},
			list:  types.AccessList{{Address: addrA, StorageKeys: []common.Hash{h1}}},
			want:  map[common.Address][]Predicate{},
		},
		{
			name:  "single tuple allowed",
			rules: allowSet{addrA: true},
			list:  types.AccessList{{Address: addrA, StorageKeys: []common.Hash{h1, h2}}},
			want:  map[common.Address][]Predicate{addrA: {{h1, h2}}},
		},
		{
			name:  "repeated address accumulates",
			rules: allowSet{addrA: true},
			list: types.AccessList{
				{Address: addrA, StorageKeys: []common.Hash{h1, h2}},
				{Address: addrA, StorageKeys: []common.Hash{h3}},
			},
			want: map[common.Address][]Predicate{addrA: {{h1, h2}, {h3}}},
		},
		{
			name:  "mixed addresses filtered",
			rules: allowSet{addrA: true, addrC: true},
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
			got := FromAccessList(tc.rules, tc.list)
			req.Equal(tc.want, got)
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
			name:    "empty input",
			input:   predicatetest.New(nil),
			want:    []byte{},
			wantErr: nil,
		},
		{
			name:    "single byte",
			input:   predicatetest.New([]byte{0xbb}),
			want:    []byte{0xbb},
			wantErr: nil,
		},
		{
			name:    "31 bytes",
			input:   predicatetest.New(bytes.Repeat([]byte{0xaa}, 31)),
			want:    bytes.Repeat([]byte{0xaa}, 31),
			wantErr: nil,
		},
		{
			name:    "32 bytes",
			input:   predicatetest.New(bytes.Repeat([]byte{0xdd}, 32)),
			want:    bytes.Repeat([]byte{0xdd}, 32),
			wantErr: nil,
		},
		{
			name:    "33 bytes",
			input:   predicatetest.New(bytes.Repeat([]byte{0xcc}, 33)),
			want:    bytes.Repeat([]byte{0xcc}, 33),
			wantErr: nil,
		},
		{
			name:    "48 bytes",
			input:   predicatetest.New(bytes.Repeat([]byte{0x00}, 48)),
			want:    bytes.Repeat([]byte{0x00}, 48),
			wantErr: nil,
		},
		{
			name:    "63 bytes",
			input:   predicatetest.New(bytes.Repeat([]byte{0xdd}, 63)),
			want:    bytes.Repeat([]byte{0xdd}, 63),
			wantErr: nil,
		},
		{
			name:    "64 bytes",
			input:   predicatetest.New(bytes.Repeat([]byte{0x33}, 64)),
			want:    bytes.Repeat([]byte{0x33}, 64),
			wantErr: nil,
		},
		{
			name:    "65 bytes",
			input:   predicatetest.New(bytes.Repeat([]byte{0xdd}, 65)),
			want:    bytes.Repeat([]byte{0xdd}, 65),
			wantErr: nil,
		},
		// Invalid test cases
		{
			name:    "all zeros empty",
			input:   nil,
			want:    nil,
			wantErr: ErrMissingDelimiter,
		},
		{
			name:    "all zeros",
			input:   Predicate{{}},
			want:    nil,
			wantErr: ErrMissingDelimiter,
		},
		{
			name:    "wrong delimiter",
			input:   Predicate{{0x42}},
			want:    nil,
			wantErr: ErrWrongEndDelimiter,
		},
		{
			name:    "wrong delimiter",
			input:   Predicate{{Delimiter}, {}},
			want:    nil,
			wantErr: ErrExcessPadding,
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
		packed := predicatetest.New(input)
		unpacked, err := packed.Bytes()
		require.NoError(t, err)
		require.Equal(t, input, unpacked)
	})
}

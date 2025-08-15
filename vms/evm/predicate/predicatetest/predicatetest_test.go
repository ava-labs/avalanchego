// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package predicatetest

import (
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/vms/evm/predicate"
)

func TestNew(t *testing.T) {
	tests := []struct {
		name  string
		input []byte
		want  predicate.Predicate
	}{
		{
			name:  "empty predicate",
			input: nil,
			want: predicate.Predicate{
				{predicate.Delimiter},
			},
		},
		{
			name:  "single byte",
			input: []byte{0x42},
			want: predicate.Predicate{
				{0x42, predicate.Delimiter},
			},
		},
		{
			name:  "31 bytes",
			input: make([]byte, 31),
			want: predicate.Predicate{
				{31: predicate.Delimiter},
			},
		},
		{
			name:  "32 bytes",
			input: make([]byte, 32),
			want: predicate.Predicate{
				{},
				{predicate.Delimiter},
			},
		},
		{
			name: "40 bytes",
			input: []byte{
				0x00, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07,
				0x10, 0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17,
				0x20, 0x21, 0x22, 0x23, 0x24, 0x25, 0x26, 0x27,
				0x30, 0x31, 0x32, 0x33, 0x34, 0x35, 0x36, 0x37,
				0x40, 0x41, 0x42, 0x43, 0x44, 0x45, 0x46, 0x47,
			},
			want: predicate.Predicate{
				{
					0x00, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07,
					0x10, 0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17,
					0x20, 0x21, 0x22, 0x23, 0x24, 0x25, 0x26, 0x27,
					0x30, 0x31, 0x32, 0x33, 0x34, 0x35, 0x36, 0x37,
				},
				{
					0x40, 0x41, 0x42, 0x43, 0x44, 0x45, 0x46, 0x47,
					predicate.Delimiter,
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

func FuzzBytesNewEqual(f *testing.F) {
	f.Fuzz(func(t *testing.T, original []byte) {
		var predicate predicate.Predicate
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

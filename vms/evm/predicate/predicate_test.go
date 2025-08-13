// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package predicate

import (
	"bytes"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPackUnpack(t *testing.T) {
	tests := []struct {
		name  string
		input []byte
		want  []byte
	}{
		{
			name:  "empty input",
			input: []byte{},
			want:  []byte{0xff},
		},
		{
			name:  "single byte",
			input: []byte{0x42},
			want:  []byte{0x42, 0xff},
		},
		{
			name:  "exactly 31 bytes",
			input: bytes.Repeat([]byte{0xaa}, 31),
			want:  append(bytes.Repeat([]byte{0xaa}, 31), 0xff),
		},
		{
			name:  "exactly 32 bytes",
			input: bytes.Repeat([]byte{0xbb}, 32),
			want:  append(bytes.Repeat([]byte{0xbb}, 32), 0xff),
		},
		{
			name:  "exactly 63 bytes",
			input: bytes.Repeat([]byte{0xcc}, 63),
			want:  append(bytes.Repeat([]byte{0xcc}, 63), 0xff),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)

			packed := New(tt.input)
			unpacked, err := Unpack(packed)

			require.NoError(err)
			require.Equal(tt.input, unpacked)

			// Verify the packed result has the correct structure
			require.Equal(tt.want, []byte(packed)[:len(tt.want)])

			// Verify padding is all zeros
			for i := len(tt.want); i < len(packed); i++ {
				require.Equal(byte(0), packed[i])
			}
		})
	}
}

func TestPackUnpackPadding(t *testing.T) {
	require := require.New(t)

	// Test that padding is correctly applied to reach multiple of 32
	testCases := []int{0, 1, 31, 32, 33, 63, 64, 65}

	for _, length := range testCases {
		t.Run(fmt.Sprintf("length_%d", length), func(_ *testing.T) {
			input := make([]byte, length)
			for i := range input {
				input[i] = byte(i + 1)
			}
			packed := New(input)
			unpacked, err := Unpack(packed)

			require.NoError(err)
			require.Equal(input, unpacked)

			// Verify packed length is a multiple of 32
			require.Equal(0, len(packed)%32)
		})
	}
}

func TestUnpackInvalid(t *testing.T) {
	tests := []struct {
		name    string
		input   Predicate
		wantErr error
	}{
		{
			name:    "empty input",
			input:   Predicate{},
			wantErr: errEmptyPredicate,
		},
		{
			name:    "all zeros",
			input:   Predicate(bytes.Repeat([]byte{0}, 32)),
			wantErr: errAllZeroBytes,
		},
		{
			name:    "missing delimiter",
			input:   Predicate(bytes.Repeat([]byte{0x42}, 32)),
			wantErr: errWrongEndDelimiter,
		},
		{
			name:    "wrong delimiter",
			input:   Predicate(append(bytes.Repeat([]byte{0x42}, 31), 0x00)),
			wantErr: errWrongEndDelimiter,
		},
		{
			name:    "excess padding",
			input:   Predicate(append(append([]byte{0x42, 0xff}, bytes.Repeat([]byte{0}, 30)...), 0x01)),
			wantErr: errExcessPadding,
		},
		{
			name:    "non-zero padding",
			input:   Predicate(append(append([]byte{0x42, 0xff}, bytes.Repeat([]byte{0}, 29)...), 0x01, 0x00)),
			wantErr: errExcessPadding,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)

			_, err := Unpack(tt.input)
			require.ErrorIs(err, tt.wantErr)
		})
	}
}

func FuzzPackUnpack(f *testing.F) {
	f.Fuzz(func(t *testing.T, input []byte) {
		packed := New(input)
		unpacked, err := Unpack(packed)
		require.NoError(t, err)
		require.Equal(t, input, unpacked)
	})
}

func FuzzUnpackPackEqual(f *testing.F) {
	// Seed with valid predicates
	for i := range 100 {
		input := make([]byte, i)
		for j := range input {
			input[j] = byte(j + 1)
		}
		validPredicate := New(input)
		f.Add([]byte(validPredicate))
	}

	f.Fuzz(func(t *testing.T, original []byte) {
		unpacked, err := Unpack(original)
		if err != nil {
			t.Skip("invalid predicate")
		}

		packed := New(unpacked)
		require.Equal(t, original, []byte(packed))
	})
}

// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package predicate

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNew(t *testing.T) {
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
			input: []byte{0xbb},
			want:  []byte{0xbb, 0xff},
		},
		{
			name:  "31 bytes",
			input: bytes.Repeat([]byte{0xaa}, 31),
			want:  append(bytes.Repeat([]byte{0xaa}, 31), 0xff),
		},
		{
			name:  "32 bytes",
			input: bytes.Repeat([]byte{0xdd}, 32),
			want:  append(bytes.Repeat([]byte{0xdd}, 32), 0xff),
		},
		{
			name:  "33 bytes",
			input: bytes.Repeat([]byte{0xcc}, 33),
			want:  append(bytes.Repeat([]byte{0xcc}, 33), 0xff),
		},
		{
			name:  "48 bytes",
			input: bytes.Repeat([]byte{0x00}, 48),
			want:  append(bytes.Repeat([]byte{0x00}, 48), 0xff),
		},
		{
			name:  "63 bytes",
			input: bytes.Repeat([]byte{0xdd}, 63),
			want:  append(bytes.Repeat([]byte{0xdd}, 63), 0xff),
		},
		{
			name:  "64 bytes",
			input: bytes.Repeat([]byte{0x33}, 64),
			want:  append(bytes.Repeat([]byte{0x33}, 64), 0xff),
		},
		{
			name:  "65 bytes",
			input: bytes.Repeat([]byte{0xdd}, 65),
			want:  append(bytes.Repeat([]byte{0xdd}, 65), 0xff),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)

			packed := new(tt.input)

			// Verify the packed result has the correct structure
			require.Equal(tt.want, packed[:len(tt.want)])

			// Verify padding is all zeros
			for i := len(tt.want); i < len(packed); i++ {
				require.Equal(byte(0), packed[i])
			}

			// Verify packed length is a multiple of 32
			require.Equal(0, len(packed)%32)
		})
	}
}

func TestUnpack(t *testing.T) {
	tests := []struct {
		name    string
		input   []byte
		want    []byte
		wantErr error
	}{
		// Valid test cases
		{
			name:    "empty input",
			input:   []byte{},
			want:    nil,
			wantErr: errEmptyPredicate,
		},
		{
			name:    "single byte",
			input:   new([]byte{0xbb}),
			want:    []byte{0xbb},
			wantErr: nil,
		},
		{
			name:    "31 bytes",
			input:   new(bytes.Repeat([]byte{0xaa}, 31)),
			want:    bytes.Repeat([]byte{0xaa}, 31),
			wantErr: nil,
		},
		{
			name:    "32 bytes",
			input:   new(bytes.Repeat([]byte{0xdd}, 32)),
			want:    bytes.Repeat([]byte{0xdd}, 32),
			wantErr: nil,
		},
		{
			name:    "33 bytes",
			input:   new(bytes.Repeat([]byte{0xcc}, 33)),
			want:    bytes.Repeat([]byte{0xcc}, 33),
			wantErr: nil,
		},
		{
			name:    "48 bytes",
			input:   new(bytes.Repeat([]byte{0x00}, 48)),
			want:    bytes.Repeat([]byte{0x00}, 48),
			wantErr: nil,
		},
		{
			name:    "63 bytes",
			input:   new(bytes.Repeat([]byte{0xdd}, 63)),
			want:    bytes.Repeat([]byte{0xdd}, 63),
			wantErr: nil,
		},
		{
			name:    "64 bytes",
			input:   new(bytes.Repeat([]byte{0x33}, 64)),
			want:    bytes.Repeat([]byte{0x33}, 64),
			wantErr: nil,
		},
		{
			name:    "65 bytes",
			input:   new(bytes.Repeat([]byte{0xdd}, 65)),
			want:    bytes.Repeat([]byte{0xdd}, 65),
			wantErr: nil,
		},
		// Invalid test cases
		{
			name:    "all zeros",
			input:   bytes.Repeat([]byte{0}, 32),
			want:    nil,
			wantErr: errAllZeroBytes,
		},
		{
			name:    "missing delimiter",
			input:   bytes.Repeat([]byte{0x42}, 32),
			want:    nil,
			wantErr: errWrongEndDelimiter,
		},
		{
			name:    "wrong delimiter",
			input:   append(bytes.Repeat([]byte{0x42}, 31), 0x00),
			want:    nil,
			wantErr: errWrongEndDelimiter,
		},
		{
			name:    "excess padding",
			input:   append(append([]byte{0x42, 0xff}, bytes.Repeat([]byte{0}, 30)...), 0x01),
			want:    nil,
			wantErr: errExcessPadding,
		},
		{
			name:    "non-zero padding",
			input:   append(append([]byte{0x42, 0xff}, bytes.Repeat([]byte{0}, 29)...), 0x01, 0x00),
			want:    nil,
			wantErr: errExcessPadding,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)

			unpacked, err := Unpack(tt.input)

			if tt.wantErr != nil {
				require.ErrorIs(err, tt.wantErr)
				require.Nil(unpacked)
			} else {
				require.NoError(err)
				require.Equal(tt.want, unpacked)
			}
		})
	}
}

func FuzzPackUnpack(f *testing.F) {
	f.Fuzz(func(t *testing.T, input []byte) {
		packed := new(input)
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
		validPredicate := new(input)
		f.Add(validPredicate)
	}

	f.Fuzz(func(t *testing.T, original []byte) {
		unpacked, err := Unpack(original)
		if err != nil {
			t.Skip("invalid predicate")
		}

		packed := new(unpacked)
		require.Equal(t, original, packed)
	})
}

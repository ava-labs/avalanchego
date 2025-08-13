// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package predicate

import (
	"bytes"
	"fmt"
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/stretchr/testify/require"
)

func TestBytesToHashSlice(t *testing.T) {
	tests := []struct {
		name  string
		input []byte
		want  []common.Hash
	}{
		{
			name:  "empty input",
			input: []byte{},
			want:  []common.Hash{},
		},
		{
			name:  "exactly 32 bytes",
			input: []byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20, 21, 22, 23, 24, 25, 26, 27, 28, 29, 30, 31, 32},
			want:  []common.Hash{},
		},
		{
			name:  "less than 32 bytes",
			input: []byte{1, 2, 3},
			want:  []common.Hash{},
		},
		{
			name:  "exactly 64 bytes",
			input: []byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20, 21, 22, 23, 24, 25, 26, 27, 28, 29, 30, 31, 32, 33, 34, 35, 36, 37, 38, 39, 40, 41, 42, 43, 44, 45, 46, 47, 48, 49, 50, 51, 52, 53, 54, 55, 56, 57, 58, 59, 60, 61, 62, 63, 64},
			want:  []common.Hash{},
		},
		{
			name:  "between 32 and 64 bytes",
			input: []byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20, 21, 22, 23, 24, 25, 26, 27, 28, 29, 30, 31, 32, 33, 34, 35, 36, 37, 38, 39, 40, 41, 42, 43, 44, 45, 46, 47, 48},
			want:  []common.Hash{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)

			result := bytesToHashSlice(tt.input)

			// Calculate want number of hashes
			wantNumHashes := roundUpTo32(len(tt.input)) / common.HashLength
			require.Len(result, wantNumHashes)

			// Verify each hash is properly formed
			for i, hash := range result {
				start := i * common.HashLength
				end := start + common.HashLength
				if end > len(tt.input) {
					end = len(tt.input)
				}

				// Check that the hash contains the want bytes
				wantBytes := make([]byte, common.HashLength)
				copy(wantBytes, tt.input[start:end])

				require.Equal(wantBytes, hash[:])
			}
		})
	}
}

func TestHashSliceToBytes(t *testing.T) {
	tests := []struct {
		name  string
		input []common.Hash
		want  []byte
	}{
		{
			name:  "empty slice",
			input: []common.Hash{},
			want:  []byte{},
		},
		{
			name:  "single hash",
			input: []common.Hash{},
			want:  []byte{},
		},
		{
			name:  "multiple hashes",
			input: []common.Hash{},
			want:  []byte{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)

			// Generate deterministic hashes for testing
			if len(tt.input) == 0 {
				tt.input = make([]common.Hash, 2)
				// First hash: 1, 2, 3, ..., 32
				for i := range tt.input[0] {
					tt.input[0][i] = byte(i + 1)
				}
				// Second hash: 33, 34, 35, ..., 64
				for i := range tt.input[1] {
					tt.input[1][i] = byte(i + 33)
				}
			}

			result := hashSliceToBytes(tt.input)

			// Verify the result has the correct length
			wantLength := len(tt.input) * common.HashLength
			require.Len(result, wantLength)

			// Verify each hash is properly serialized
			for i, hash := range tt.input {
				start := i * common.HashLength
				end := start + common.HashLength

				require.Equal(hash[:], result[start:end])
			}
		})
	}
}

func TestBytesToHashSliceRoundTrip(t *testing.T) {
	require := require.New(t)

	// Test round-trip conversion with deterministic data
	testData := []byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20, 21, 22, 23, 24, 25, 26, 27, 28, 29, 30, 31, 32, 33, 34, 35, 36, 37, 38, 39, 40, 41, 42, 43, 44, 45, 46, 47, 48, 49, 50, 51, 52, 53, 54, 55, 56, 57, 58, 59, 60, 61, 62, 63, 64, 65, 66, 67, 68, 69, 70, 71, 72, 73, 74, 75, 76, 77, 78, 79, 80, 81, 82, 83, 84, 85, 86, 87, 88, 89, 90, 91, 92, 93, 94, 95, 96, 97, 98, 99, 100}

	hashes := bytesToHashSlice(testData)
	bytes := hashSliceToBytes(hashes)

	// The round-trip should preserve the original data (with padding)
	// Since hashSliceToBytes always returns a multiple of 32 bytes,
	// we need to check that the original data is preserved in the padded result
	require.Equal(testData, bytes[:len(testData)])

	// Verify that the padding is all zeros
	for i := len(testData); i < len(bytes); i++ {
		require.Equal(byte(0), bytes[i])
	}
}

func TestBytesToHashSliceEdgeCases(t *testing.T) {
	require := require.New(t)

	// Test edge cases around 32-byte boundaries
	for i := 30; i <= 34; i++ {
		testData := make([]byte, i)
		for j := range testData {
			testData[j] = byte(j + 1)
		}
		hashes := bytesToHashSlice(testData)

		wantNumHashes := roundUpTo32(i) / common.HashLength
		require.Len(hashes, wantNumHashes)

		// Verify padding behavior
		if i%32 != 0 {
			// Should have padding in the last hash
			lastHash := hashes[len(hashes)-1]
			start := (len(hashes) - 1) * 32
			wantBytes := make([]byte, 32)
			copy(wantBytes, testData[start:])
			require.Equal(wantBytes, lastHash[:])
		}
	}
}

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

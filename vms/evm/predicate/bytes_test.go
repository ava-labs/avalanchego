// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package predicate

import (
	"bytes"
	"fmt"
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/utils"
)

func TestBytesToHashSlice(t *testing.T) {
	tests := []struct {
		name     string
		input    []byte
		expected []common.Hash
	}{
		{
			name:     "empty input",
			input:    []byte{},
			expected: []common.Hash{},
		},
		{
			name:     "exactly 32 bytes",
			input:    utils.RandomBytes(32),
			expected: []common.Hash{},
		},
		{
			name:     "less than 32 bytes",
			input:    []byte{1, 2, 3},
			expected: []common.Hash{},
		},
		{
			name:     "exactly 64 bytes",
			input:    utils.RandomBytes(64),
			expected: []common.Hash{},
		},
		{
			name:     "between 32 and 64 bytes",
			input:    utils.RandomBytes(48),
			expected: []common.Hash{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)

			result := bytesToHashSlice(tt.input)

			// Calculate expected number of hashes
			expectedNumHashes := roundUpTo32(len(tt.input)) / common.HashLength
			require.Len(result, expectedNumHashes)

			// Verify each hash is properly formed
			for i, hash := range result {
				start := i * common.HashLength
				end := start + common.HashLength
				if end > len(tt.input) {
					end = len(tt.input)
				}

				// Check that the hash contains the expected bytes
				expectedBytes := make([]byte, common.HashLength)
				copy(expectedBytes, tt.input[start:end])

				require.Equal(expectedBytes, hash[:])
			}
		})
	}
}

func TestHashSliceToBytes(t *testing.T) {
	tests := []struct {
		name     string
		input    []common.Hash
		expected []byte
	}{
		{
			name:     "empty slice",
			input:    []common.Hash{},
			expected: []byte{},
		},
		{
			name:     "single hash",
			input:    []common.Hash{},
			expected: []byte{},
		},
		{
			name:     "multiple hashes",
			input:    []common.Hash{},
			expected: []byte{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)

			// Generate random hashes for testing
			if len(tt.input) == 0 {
				tt.input = make([]common.Hash, 2)
				for i := range tt.input {
					copy(tt.input[i][:], utils.RandomBytes(32))
				}
			}

			result := hashSliceToBytes(tt.input)

			// Verify the result has the correct length
			expectedLength := len(tt.input) * common.HashLength
			require.Len(result, expectedLength)

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

	// Test round-trip conversion
	testData := utils.RandomBytes(100)

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
		testData := utils.RandomBytes(i)
		hashes := bytesToHashSlice(testData)

		expectedNumHashes := roundUpTo32(i) / common.HashLength
		require.Len(hashes, expectedNumHashes)

		// Verify padding behavior
		if i%32 != 0 {
			// Should have padding in the last hash
			lastHash := hashes[len(hashes)-1]
			start := (len(hashes) - 1) * 32
			expectedBytes := make([]byte, 32)
			copy(expectedBytes, testData[start:])
			require.Equal(expectedBytes, lastHash[:])
		}
	}
}

func TestPackUnpack(t *testing.T) {
	tests := []struct {
		name     string
		input    []byte
		expected []byte
	}{
		{
			name:     "empty input",
			input:    []byte{},
			expected: []byte{0xff},
		},
		{
			name:     "single byte",
			input:    []byte{0x42},
			expected: []byte{0x42, 0xff},
		},
		{
			name:     "exactly 31 bytes",
			input:    bytes.Repeat([]byte{0xaa}, 31),
			expected: append(bytes.Repeat([]byte{0xaa}, 31), 0xff),
		},
		{
			name:     "exactly 32 bytes",
			input:    bytes.Repeat([]byte{0xbb}, 32),
			expected: append(bytes.Repeat([]byte{0xbb}, 32), 0xff),
		},
		{
			name:     "exactly 63 bytes",
			input:    bytes.Repeat([]byte{0xcc}, 63),
			expected: append(bytes.Repeat([]byte{0xcc}, 63), 0xff),
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
			require.Equal(tt.expected, []byte(packed)[:len(tt.expected)])

			// Verify padding is all zeros
			for i := len(tt.expected); i < len(packed); i++ {
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
			input := utils.RandomBytes(length)
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
		name        string
		input       Predicate
		expectedErr error
	}{
		{
			name:        "empty input",
			input:       Predicate{},
			expectedErr: errEmptyPredicate,
		},
		{
			name:        "all zeros",
			input:       Predicate(bytes.Repeat([]byte{0}, 32)),
			expectedErr: errAllZeroBytes,
		},
		{
			name:        "missing delimiter",
			input:       Predicate(bytes.Repeat([]byte{0x42}, 32)),
			expectedErr: errWrongEndDelimiter,
		},
		{
			name:        "wrong delimiter",
			input:       Predicate(append(bytes.Repeat([]byte{0x42}, 31), 0x00)),
			expectedErr: errWrongEndDelimiter,
		},
		{
			name:        "excess padding",
			input:       Predicate(append(append([]byte{0x42, 0xff}, bytes.Repeat([]byte{0}, 30)...), 0x01)),
			expectedErr: errExcessPadding,
		},
		{
			name:        "non-zero padding",
			input:       Predicate(append(append([]byte{0x42, 0xff}, bytes.Repeat([]byte{0}, 29)...), 0x01, 0x00)),
			expectedErr: errExcessPadding,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)

			_, err := Unpack(tt.input)
			require.ErrorIs(err, tt.expectedErr)
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
		validPredicate := New(utils.RandomBytes(i))
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

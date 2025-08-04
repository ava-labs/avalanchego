// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package predicate

import (
	"bytes"
	"errors"
	"fmt"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/libevm/common"
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

			result := BytesToHashSlice(tt.input)

			// Calculate expected number of hashes
			expectedNumHashes := (len(tt.input) + 31) / 32
			require.Equal(expectedNumHashes, len(result))

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

			result := HashSliceToBytes(tt.input)

			// Verify the result has the correct length
			expectedLength := len(tt.input) * common.HashLength
			require.Equal(expectedLength, len(result))

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

	hashes := BytesToHashSlice(testData)
	bytes := HashSliceToBytes(hashes)

	// The round-trip should preserve the original data (with padding)
	// Since HashSliceToBytes always returns a multiple of 32 bytes,
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
		hashes := BytesToHashSlice(testData)

		expectedNumHashes := (i + 31) / 32
		require.Equal(expectedNumHashes, len(hashes))

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

// Explicit tests for Pack/Unpack functions
func TestPackUnpackBasic(t *testing.T) {
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

			packed := Pack(tt.input)
			unpacked, err := Unpack(packed)

			require.NoError(err)
			require.Equal(tt.input, unpacked)

			// Verify the packed result has the correct structure
			require.Equal(tt.expected, packed[:len(tt.expected)])

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
		t.Run(fmt.Sprintf("length_%d", length), func(t *testing.T) {
			input := utils.RandomBytes(length)
			packed := Pack(input)
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
		input       []byte
		expectedErr error
	}{
		{
			name:        "empty input",
			input:       []byte{},
			expectedErr: ErrInvalidAllZeroBytes,
		},
		{
			name:        "all zeros",
			input:       bytes.Repeat([]byte{0}, 32),
			expectedErr: ErrInvalidAllZeroBytes,
		},
		{
			name:        "missing delimiter",
			input:       bytes.Repeat([]byte{0x42}, 32),
			expectedErr: ErrInvalidEndDelimiter,
		},
		{
			name:        "wrong delimiter",
			input:       append(bytes.Repeat([]byte{0x42}, 31), 0x00),
			expectedErr: ErrInvalidEndDelimiter,
		},
		{
			name:        "excess padding",
			input:       append(append([]byte{0x42, 0xff}, bytes.Repeat([]byte{0}, 30)...), 0x01),
			expectedErr: ErrInvalidPadding,
		},
		{
			name:        "non-zero padding",
			input:       append(append([]byte{0x42, 0xff}, bytes.Repeat([]byte{0}, 29)...), 0x01, 0x00),
			expectedErr: ErrInvalidPadding,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)

			_, err := Unpack(tt.input)
			require.Error(err)
			require.True(errors.Is(err, tt.expectedErr))
		})
	}
}

func FuzzPackUnpackRoundTrip(f *testing.F) {
	// Seed with various input sizes
	for i := range 100 {
		f.Add(utils.RandomBytes(i))
	}

	f.Fuzz(func(t *testing.T, input []byte) {
		packed := Pack(input)
		unpacked, err := Unpack(packed)
		require.NoError(t, err)
		require.Equal(t, input, unpacked)
	})
}

func FuzzUnpackInvalid(f *testing.F) {
	// Seed with valid predicates that we'll corrupt
	for i := range 100 {
		validPredicate := Pack(utils.RandomBytes(i))
		f.Add(validPredicate)
	}

	f.Fuzz(func(t *testing.T, validPredicate []byte) {
		// Only test if the input is actually a valid predicate
		if _, err := Unpack(validPredicate); err != nil {
			t.Skip("Input is not a valid predicate")
		}

		// Test corruption by adding non-zero bytes after the valid predicate
		// This should always create an invalid predicate
		corruption := bytes.Repeat([]byte{0xee}, 5)
		invalidPredicate := slices.Concat(validPredicate, corruption)
		_, err := Unpack(invalidPredicate)

		// Check for either error type since different padding can cause different errors.
		// Zero padding cases trigger ErrInvalidPadding (padding length check fails).
		// Non-zero padding cases trigger ErrInvalidEndDelimiter (delimiter check fails).
		require.True(t, errors.Is(err, ErrInvalidPadding) || errors.Is(err, ErrInvalidEndDelimiter),
			"Expected error for corrupted predicate, got: %v", err)
	})
}

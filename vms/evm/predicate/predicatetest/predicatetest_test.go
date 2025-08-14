// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package predicatetest

import (
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/stretchr/testify/require"
)

func TestNewAccessListEmptyPredicate(t *testing.T) {
	require := require.New(t)

	addr := common.HexToAddress("0x1234567890123456789012345678901234567890")
	accessList := NewAccessList(addr, []byte{})

	require.Len(accessList, 1)
	require.Equal(addr, accessList[0].Address)
	// 0 + delimiter, padded to 32 bytes is 1 storage key
	require.Len(accessList[0].StorageKeys, 1)
}

func TestNewAccessListWithPredicate(t *testing.T) {
	tests := []struct {
		name             string
		predicateBytes   []byte
		predicateAddress common.Address
		wantNumHashes    int
		wantStorageKeys  []common.Hash
	}{
		{
			name:             "empty predicate",
			predicateBytes:   []byte{},
			predicateAddress: common.HexToAddress("0x1234567890123456789012345678901234567890"),
			wantNumHashes:    1, // empty + delimiter = 1 byte, rounded up to 32 bytes = 1 hash
		},
		{
			name:             "single byte predicate",
			predicateBytes:   []byte{0x42},
			predicateAddress: common.HexToAddress("0x1234567890123456789012345678901234567890"),
			wantNumHashes:    1, // 1 byte + delimiter = 2 bytes, rounded up to 32 bytes = 1 hash
		},
		{
			name:             "exactly 31 bytes predicate",
			predicateBytes:   make([]byte, 31),
			predicateAddress: common.HexToAddress("0x1234567890123456789012345678901234567890"),
			wantNumHashes:    1, // 31 bytes + delimiter = 32 bytes = 1 hash
		},
		{
			name:             "exactly 32 bytes predicate",
			predicateBytes:   make([]byte, 32),
			predicateAddress: common.HexToAddress("0x1234567890123456789012345678901234567890"),
			wantNumHashes:    2, // 32 bytes + delimiter = 33 bytes, rounded up to 64 bytes = 2 hashes
		},
		{
			name:             "exactly 63 bytes predicate",
			predicateBytes:   make([]byte, 63),
			predicateAddress: common.HexToAddress("0x1234567890123456789012345678901234567890"),
			wantNumHashes:    2, // 63 bytes + delimiter = 64 bytes = 2 hashes
		},
		{
			name:             "exactly 64 bytes predicate",
			predicateBytes:   make([]byte, 64),
			predicateAddress: common.HexToAddress("0x1234567890123456789012345678901234567890"),
			wantNumHashes:    3, // 64 bytes + delimiter = 65 bytes, rounded up to 96 bytes = 3 hashes
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)

			// Initialize predicate bytes with deterministic values
			for i := range tt.predicateBytes {
				tt.predicateBytes[i] = byte(i + 1)
			}

			accessList := NewAccessList(tt.predicateAddress, tt.predicateBytes)
			require.Len(accessList, 1)

			// Verify predicate address is correct
			require.Equal(tt.predicateAddress, accessList[0].Address)

			// Verify storage keys length matches expected number of hashes
			require.Len(accessList[0].StorageKeys, tt.wantNumHashes)

			// Verify the predicate data is correctly encoded in storage keys
			predicateData := pack(tt.predicateBytes)
			for i, storageKey := range accessList[0].StorageKeys {
				start := i * common.HashLength
				end := start + common.HashLength
				if end > len(predicateData) {
					end = len(predicateData)
				}

				// Check that the storage key contains the expected bytes
				wantBytes := make([]byte, common.HashLength)
				copy(wantBytes, predicateData[start:end])

				require.Equal(wantBytes, storageKey[:])
			}
		})
	}
}

// Keeping a simple encoding test via total size

func TestNewAccessListPredicateEncoding(t *testing.T) {
	require := require.New(t)

	// Test that predicate encoding follows the expected format:
	// 1. Original bytes
	// 2. Delimiter (0xff)
	// 3. Zero padding to multiple of 32

	testCases := []struct {
		name           string
		predicateBytes []byte
		wantLength     int
	}{
		{
			name:           "empty predicate",
			predicateBytes: []byte{},
			wantLength:     32, // 0 + 1 (delimiter) + 31 (padding) = 32
		},
		{
			name:           "single byte",
			predicateBytes: []byte{0x42},
			wantLength:     32, // 1 + 1 (delimiter) + 30 (padding) = 32
		},
		{
			name:           "31 bytes",
			predicateBytes: make([]byte, 31),
			wantLength:     32, // 31 + 1 (delimiter) + 0 (padding) = 32
		},
		{
			name:           "32 bytes",
			predicateBytes: make([]byte, 32),
			wantLength:     64, // 32 + 1 (delimiter) + 31 (padding) = 64
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(_ *testing.T) {
			// Initialize predicate bytes with deterministic values
			for i := range tc.predicateBytes {
				tc.predicateBytes[i] = byte(i + 1)
			}

			accessList := NewAccessList(common.Address{}, tc.predicateBytes)
			require.Len(accessList, 1)

			// Calculate expected number of storage keys
			wantNumHashes := tc.wantLength / common.HashLength
			require.Len(accessList[0].StorageKeys, wantNumHashes)

			// Verify the total size of all storage keys equals the expected length
			totalSize := len(accessList[0].StorageKeys) * common.HashLength
			require.Equal(tc.wantLength, totalSize)
		})
	}
}

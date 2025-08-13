// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package tx

import (
	"math/big"
	"testing"

	"github.com/ava-labs/avalanchego/vms/evm/predicate"
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/types"
	"github.com/stretchr/testify/require"
)

func TestNewTxWithPredicate(t *testing.T) {
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

			// Create transaction with predicate
			tx := NewTx(
				big.NewInt(1),           // chainID
				0,                       // nonce
				nil,                     // to
				21000,                   // gas
				big.NewInt(20000000000), // gasFeeCap
				big.NewInt(1000000000),  // gasTipCap
				big.NewInt(0),           // value
				[]byte{},                // data
				types.AccessList{},      // accessList
				tt.predicateAddress,     // predicateAddress
				tt.predicateBytes,       // predicateBytes
			)

			// Verify transaction was created
			require.NotNil(tx)

			// Extract access list from transaction
			accessList := tx.AccessList()
			require.Len(accessList, 1)

			// Verify predicate address is correct
			require.Equal(tt.predicateAddress, accessList[0].Address)

			// Verify storage keys length matches expected number of hashes
			require.Len(accessList[0].StorageKeys, tt.wantNumHashes)

			// Verify the predicate data is correctly encoded in storage keys
			predicateData := predicate.New(tt.predicateBytes)
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

func TestNewTxWithExistingAccessList(t *testing.T) {
	require := require.New(t)

	// Create existing access list
	existingAccessList := types.AccessList{
		{
			Address:     common.HexToAddress("0x1111111111111111111111111111111111111111"),
			StorageKeys: []common.Hash{common.HexToHash("0x1111111111111111111111111111111111111111111111111111111111111111")},
		},
	}

	predicateAddress := common.HexToAddress("0x2222222222222222222222222222222222222222")
	predicateBytes := []byte{0x42, 0x43, 0x44}

	tx := NewTx(
		big.NewInt(1),           // chainID
		0,                       // nonce
		nil,                     // to
		21000,                   // gas
		big.NewInt(20000000000), // gasFeeCap
		big.NewInt(1000000000),  // gasTipCap
		big.NewInt(0),           // value
		[]byte{},                // data
		existingAccessList,      // accessList
		predicateAddress,        // predicateAddress
		predicateBytes,          // predicateBytes
	)

	// Verify transaction was created
	require.NotNil(tx)

	// Extract access list from transaction
	accessList := tx.AccessList()
	require.Len(accessList, 2)

	// Verify existing access list entry is preserved
	require.Equal(existingAccessList[0].Address, accessList[0].Address)
	require.Equal(existingAccessList[0].StorageKeys, accessList[0].StorageKeys)

	// Verify predicate access list entry is added
	require.Equal(predicateAddress, accessList[1].Address)
	require.Len(accessList[1].StorageKeys, 1) // 3 bytes + delimiter = 4 bytes, rounded up to 32 bytes = 1 hash
}

func TestNewTxPredicateEncoding(t *testing.T) {
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
		t.Run(tc.name, func(t *testing.T) {
			// Initialize predicate bytes with deterministic values
			for i := range tc.predicateBytes {
				tc.predicateBytes[i] = byte(i + 1)
			}

			tx := NewTx(
				big.NewInt(1),           // chainID
				0,                       // nonce
				nil,                     // to
				21000,                   // gas
				big.NewInt(20000000000), // gasFeeCap
				big.NewInt(1000000000),  // gasTipCap
				big.NewInt(0),           // value
				[]byte{},                // data
				types.AccessList{},      // accessList
				common.Address{},        // predicateAddress
				tc.predicateBytes,       // predicateBytes
			)

			accessList := tx.AccessList()
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

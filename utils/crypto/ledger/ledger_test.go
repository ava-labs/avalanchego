// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package ledger

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/utils/formatting/address"
	"github.com/ava-labs/avalanchego/utils/hashing"
)

const (
	chainAlias = "P"
	hrp        = "fuji"
)

// TestLedger will be skipped if a ledger is not connected.
func TestLedger(t *testing.T) {
	require := require.New(t)

	// Initialize Ledger
	device, err := New()
	if err != nil {
		t.Skip("ledger not detected")
	}

	// Get version
	version, err := device.Version()
	require.NoError(err)
	t.Logf("version: %s\n", version)

	// Get Fuji Address
	addr, err := device.Address(hrp, 0)
	require.NoError(err)
	paddr, err := address.Format(chainAlias, hrp, addr[:])
	require.NoError(err)
	t.Logf("address: %s shortID: %s\n", paddr, addr)

	// Get Extended Addresses
	addresses, err := device.Addresses([]uint32{0, 1, 2, 3, 4, 5, 6, 7, 8, 9})
	require.NoError(err)
	for i, taddr := range addresses {
		paddr, err := address.Format(chainAlias, hrp, taddr[:])
		require.NoError(err)
		t.Logf("address(%d): %s shortID: %s\n", i, paddr, taddr)

		// Ensure first derived address matches directly requested address
		if i == 0 {
			require.Equal(addr, taddr, "address mismatch at index 0")
		}
	}

	// Sign Hash
	rawHash := hashing.ComputeHash256([]byte{0x1, 0x2, 0x3, 0x4})
	indices := []uint32{1, 3}
	sigs, err := device.SignHash(rawHash, indices)
	require.NoError(err)
	require.Len(sigs, 2)

	for i, addrIndex := range indices {
		sig := sigs[i]

		pk, err := secp256k1.RecoverPublicKeyFromHash(rawHash, sig)
		require.NoError(err)
		require.Equal(addresses[addrIndex], pk.Address())
	}

	// Disconnect
	require.NoError(device.Disconnect())
}

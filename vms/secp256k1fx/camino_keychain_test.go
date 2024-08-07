// Copyright (C) 2022-2024, Chain4Travel AG. All rights reserved.
// See the file LICENSE for licensing terms.

package secp256k1fx

import (
	"testing"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/utils/formatting"
	"github.com/ava-labs/avalanchego/vms/components/multisig"
	"github.com/stretchr/testify/require"
)

type TestGetter struct {
	msig      ids.ShortID
	addresses []ids.ShortID
	threshold uint32
}

func (t *TestGetter) GetMultisigAlias(addr ids.ShortID) (*multisig.AliasWithNonce, error) {
	if addr == t.msig {
		return &multisig.AliasWithNonce{
			Alias: multisig.Alias{
				Owners: &OutputOwners{
					Threshold: t.threshold,
					Addrs:     t.addresses,
				},
			},
		}, nil
	}
	return nil, database.ErrNotFound
}

func TestSpendMultiSigNoMSig(t *testing.T) {
	require := require.New(t)
	kc := NewKeychain()

	addresses := make([]ids.ShortID, 0, len(keys))

	for _, keyStr := range keys {
		skBytes, err := formatting.Decode(formatting.HexNC, keyStr)
		require.NoError(err)
		sk, err := kc.factory.ToPrivateKey(skBytes)
		require.NoError(err)
		kc.Add(sk)
		addresses = append(addresses, sk.Address())
	}

	transfer := TransferOutput{
		Amt: 12345,
		OutputOwners: OutputOwners{
			Locktime:  54321,
			Threshold: 2,
			Addrs: []ids.ShortID{
				addresses[1],
				addresses[2],
			},
		},
	}
	require.NoError(transfer.Verify())

	_, sigs, err := kc.SpendMultiSig(&transfer, 54321, nil)
	require.NoError(err)

	require.Equal(len(sigs), 2)
}

func TestSpendMultiSigMSig(t *testing.T) {
	require := require.New(t)
	kc := NewKeychain()

	addresses := make([]ids.ShortID, 0, len(keys))

	for _, keyStr := range keys {
		skBytes, err := formatting.Decode(formatting.HexNC, keyStr)
		require.NoError(err)
		sk, err := kc.factory.ToPrivateKey(skBytes)
		require.NoError(err)
		kc.Add(sk)
		addresses = append(addresses, sk.Address())
	}

	transfer := TransferOutput{
		Amt: 12345,
		OutputOwners: OutputOwners{
			Locktime:  54321,
			Threshold: 3,
			Addrs: []ids.ShortID{
				addresses[1],
				addresses[2],
				msigAddress,
			},
		},
	}
	require.NoError(transfer.Verify())

	_, sigs, err := kc.SpendMultiSig(&transfer, 54321, &TestGetter{msig: msigAddress, addresses: []ids.ShortID{addresses[0]}, threshold: 1})
	require.NoError(err)

	require.Equal(len(sigs), 3)
}

func TestSpendMultiSigFakeKeys(t *testing.T) {
	require := require.New(t)
	kc := NewKeychain()

	addresses := make([]ids.ShortID, 0, len(keys))

	for _, addr := range addrs {
		addrBytes, err := ids.ShortFromString(addr)
		require.NoError(err)

		sk := secp256k1.FakePrivateKey(addrBytes)
		kc.Add(sk)

		addresses = append(addresses, sk.Address())
	}

	transfer := TransferOutput{
		Amt: 12345,
		OutputOwners: OutputOwners{
			Locktime:  54321,
			Threshold: 2,
			Addrs: []ids.ShortID{
				addresses[1],
				addresses[2],
			},
		},
	}
	require.NoError(transfer.Verify())

	_, _, err := kc.SpendMultiSig(&transfer, 54321, nil)
	require.NoError(err)
}

func TestSpendMultiSigCycle(t *testing.T) {
	require := require.New(t)
	kc := NewKeychain()

	addresses := make([]ids.ShortID, 0, len(keys))

	for _, keyStr := range keys {
		skBytes, err := formatting.Decode(formatting.HexNC, keyStr)
		require.NoError(err)
		sk, err := kc.factory.ToPrivateKey(skBytes)
		require.NoError(err)
		kc.Add(sk)
		addresses = append(addresses, sk.Address())
	}

	transfer := TransferOutput{
		Amt: 12345,
		OutputOwners: OutputOwners{
			Locktime:  54321,
			Threshold: 2,
			Addrs: []ids.ShortID{
				addresses[1],
				msigAddress,
			},
		},
	}
	require.NoError(transfer.Verify())

	_, _, err := kc.SpendMultiSig(&transfer, 54321, &TestGetter{msig: msigAddress, addresses: []ids.ShortID{msigAddress}, threshold: 1})
	require.ErrorIs(err, errCyclicAliases)
}

// Verify that visited / verified items in nested ownergroups which don't
// reach threshold are recovered correctly to it's initial state.
// This test has an {M{A0, A1, T2}, A1, T1} setup, and only A1 signing.
// We first visit A1 inside M which adds a signature. Because M's threshold of 2
// M is not verified, and we continue with A1 at root level. This must become
// SigIndex 0 because it is the first signature added
func TestUnverifiedNestedOwner(t *testing.T) {
	require := require.New(t)
	kc := NewKeychain()

	addresses := make([]ids.ShortID, 0, len(keys))

	for i, keyStr := range keys {
		skBytes, err := formatting.Decode(formatting.HexNC, keyStr)
		require.NoError(err)

		sk, err := kc.factory.ToPrivateKey(skBytes)
		require.NoError(err)
		// Only one signer for this test
		if i == 1 {
			kc.Add(sk)
		}
		addresses = append(addresses, sk.Address())
	}

	transfer := TransferOutput{
		Amt: 12345,
		OutputOwners: OutputOwners{
			Locktime:  54321,
			Threshold: 1,
			Addrs: []ids.ShortID{
				msigAddressLow,
				addresses[1],
			},
		},
	}
	require.NoError(transfer.Verify())

	ti, sigs, err := kc.SpendMultiSig(&transfer, 54321, &TestGetter{msig: msigAddressLow, addresses: []ids.ShortID{addresses[0], addresses[1]}, threshold: 2})
	require.NoError(err)
	require.Equal(1, len(sigs), 1)
	require.Equal(uint32(2), ti.(*TransferInput).SigIndices[0])
}

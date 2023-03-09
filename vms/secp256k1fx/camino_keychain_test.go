/// Copyright (C) 2023, Chain4Travel AG. All rights reserved.
// See the file LICENSE for licensing terms.

package secp256k1fx

import (
	"testing"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/crypto"
	"github.com/ava-labs/avalanchego/utils/formatting"
	"github.com/ava-labs/avalanchego/vms/components/multisig"
	"github.com/stretchr/testify/require"
)

type TestGetter struct {
	address ids.ShortID
}

func (t *TestGetter) GetMultisigAlias(addr ids.ShortID) (*multisig.Alias, error) {
	if addr == msigAddress {
		return &multisig.Alias{
			Owners: &OutputOwners{
				Threshold: 1,
				Addrs: []ids.ShortID{
					t.address,
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

		skIntf, err := kc.factory.ToPrivateKey(skBytes)
		require.NoError(err)
		sk, ok := skIntf.(*crypto.PrivateKeySECP256K1R)
		require.True(ok, "Factory should have returned secp256k1r private key")
		kc.Add(sk)
		addresses = append(addresses, sk.PublicKey().Address())
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

		skIntf, err := kc.factory.ToPrivateKey(skBytes)
		require.NoError(err)
		sk, ok := skIntf.(*crypto.PrivateKeySECP256K1R)
		require.True(ok, "Factory should have returned secp256k1r private key")
		kc.Add(sk)
		addresses = append(addresses, sk.PublicKey().Address())
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

	_, sigs, err := kc.SpendMultiSig(&transfer, 54321, &TestGetter{addresses[0]})
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

		sk := crypto.FakePrivateKey(addrBytes)
		kc.Add(sk)

		addresses = append(addresses, sk.PublicKey().Address())
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

		skIntf, err := kc.factory.ToPrivateKey(skBytes)
		require.NoError(err)
		sk, ok := skIntf.(*crypto.PrivateKeySECP256K1R)
		require.True(ok, "Factory should have returned secp256k1r private key")
		kc.Add(sk)
		addresses = append(addresses, sk.PublicKey().Address())
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

	_, _, err := kc.SpendMultiSig(&transfer, 54321, &TestGetter{address: msigAddress})
	require.ErrorIs(err, errCyclicAliases)
}

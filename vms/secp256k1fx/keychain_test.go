// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package secp256k1fx

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/utils/formatting"
)

var (
	keys = []string{
		"0xb1ed77ad48555d49f03a7465f0685a7d86bfd5f3a3ccf1be01971ea8dec5471c",
		"0x51a5e21237263396a5dfce60496d0ca3829d23fd33c38e6d13ae53b4810df9ca",
		"0x8c2bae69b0e1f6f3a5e784504ee93279226f997c5a6771b9bd6b881a8fee1e9d",
	}
	addrs = []string{
		"B6D4v1VtPYLbiUvYXtW4Px8oE9imC2vGW",
		"P5wdRuZeaDt28eHMP5S3w9ZdoBfo7wuzF",
		"Q4MzFZZDPHRPAHFeDs3NiyyaZDvxHKivf",
	}
)

func TestNewKeychain(t *testing.T) {
	require.NotNil(t, NewKeychain())
}

func TestKeychainGetUnknownAddr(t *testing.T) {
	require := require.New(t)
	kc := NewKeychain()

	addr, _ := ids.ShortFromString(addrs[0])
	_, exists := kc.Get(addr)
	require.False(exists)
}

func TestKeychainAdd(t *testing.T) {
	require := require.New(t)
	kc := NewKeychain()

	skBytes, err := formatting.Decode(formatting.HexNC, keys[0])
	require.NoError(err)

	sk, err := secp256k1.ToPrivateKey(skBytes)
	require.NoError(err)
	kc.Add(sk)

	addr, _ := ids.ShortFromString(addrs[0])
	rsk, exists := kc.Get(addr)
	require.True(exists)
	require.IsType(&secp256k1.PrivateKey{}, rsk)
	rsksecp := rsk.(*secp256k1.PrivateKey)
	require.Equal(sk.Bytes(), rsksecp.Bytes())

	addrs := kc.Addresses()
	require.Equal(1, addrs.Len())
	require.True(addrs.Contains(addr))
}

func TestKeychainNew(t *testing.T) {
	require := require.New(t)
	kc := NewKeychain()

	require.Zero(kc.Addresses().Len())

	sk, err := kc.New()
	require.NoError(err)

	addr := sk.PublicKey().Address()

	addrs := kc.Addresses()
	require.Equal(1, addrs.Len())
	require.True(addrs.Contains(addr))
}

func TestKeychainMatch(t *testing.T) {
	require := require.New(t)
	kc := NewKeychain()

	sks := []*secp256k1.PrivateKey{}
	for _, keyStr := range keys {
		skBytes, err := formatting.Decode(formatting.HexNC, keyStr)
		require.NoError(err)

		sk, err := secp256k1.ToPrivateKey(skBytes)
		require.NoError(err)
		sks = append(sks, sk)
	}

	kc.Add(sks[0])

	owners := OutputOwners{
		Threshold: 1,
		Addrs: []ids.ShortID{
			sks[1].PublicKey().Address(),
			sks[2].PublicKey().Address(),
		},
	}
	require.NoError(owners.Verify())

	_, _, ok := kc.Match(&owners, 0)
	require.False(ok)

	kc.Add(sks[1])

	indices, keys, ok := kc.Match(&owners, 1)
	require.True(ok)
	require.Equal([]uint32{0}, indices)
	require.Len(keys, 1)
	require.Equal(sks[1].PublicKey().Address(), keys[0].PublicKey().Address())

	kc.Add(sks[2])

	indices, keys, ok = kc.Match(&owners, 1)
	require.True(ok)
	require.Equal([]uint32{0}, indices)
	require.Len(keys, 1)
	require.Equal(sks[1].PublicKey().Address(), keys[0].PublicKey().Address())
}

func TestKeychainSpendMint(t *testing.T) {
	require := require.New(t)
	kc := NewKeychain()

	sks := []*secp256k1.PrivateKey{}
	for _, keyStr := range keys {
		skBytes, err := formatting.Decode(formatting.HexNC, keyStr)
		require.NoError(err)

		sk, err := secp256k1.ToPrivateKey(skBytes)
		require.NoError(err)
		sks = append(sks, sk)
	}

	mint := MintOutput{OutputOwners: OutputOwners{
		Threshold: 2,
		Addrs: []ids.ShortID{
			sks[1].PublicKey().Address(),
			sks[2].PublicKey().Address(),
		},
	}}
	require.NoError(mint.Verify())

	_, _, err := kc.Spend(&mint, 0)
	require.ErrorIs(err, errCantSpend)

	kc.Add(sks[0])
	kc.Add(sks[1])
	kc.Add(sks[2])

	vinput, keys, err := kc.Spend(&mint, 0)
	require.NoError(err)

	require.IsType(&Input{}, vinput)
	input := vinput.(*Input)
	require.NoError(input.Verify())
	require.Equal([]uint32{0, 1}, input.SigIndices)
	require.Len(keys, 2)
	require.Equal(sks[1].PublicKey().Address(), keys[0].PublicKey().Address())
	require.Equal(sks[2].PublicKey().Address(), keys[1].PublicKey().Address())
}

func TestKeychainSpendTransfer(t *testing.T) {
	require := require.New(t)
	kc := NewKeychain()

	sks := []*secp256k1.PrivateKey{}
	for _, keyStr := range keys {
		skBytes, err := formatting.Decode(formatting.HexNC, keyStr)
		require.NoError(err)

		sk, err := secp256k1.ToPrivateKey(skBytes)
		require.NoError(err)
		sks = append(sks, sk)
	}

	transfer := TransferOutput{
		Amt: 12345,
		OutputOwners: OutputOwners{
			Locktime:  54321,
			Threshold: 2,
			Addrs: []ids.ShortID{
				sks[1].PublicKey().Address(),
				sks[2].PublicKey().Address(),
			},
		},
	}
	require.NoError(transfer.Verify())

	_, _, err := kc.Spend(&transfer, 54321)
	require.ErrorIs(err, errCantSpend)

	kc.Add(sks[0])
	kc.Add(sks[1])
	kc.Add(sks[2])

	_, _, err = kc.Spend(&transfer, 4321)
	require.ErrorIs(err, errCantSpend)

	vinput, keys, err := kc.Spend(&transfer, 54321)
	require.NoError(err)

	require.IsType(&TransferInput{}, vinput)
	input := vinput.(*TransferInput)
	require.NoError(input.Verify())
	require.Equal(uint64(12345), input.Amount())
	require.Equal([]uint32{0, 1}, input.SigIndices)
	require.Len(keys, 2)
	require.Equal(sks[1].PublicKey().Address(), keys[0].PublicKey().Address())
	require.Equal(sks[2].PublicKey().Address(), keys[1].PublicKey().Address())
}

func TestKeychainString(t *testing.T) {
	require := require.New(t)
	kc := NewKeychain()

	skBytes, err := formatting.Decode(formatting.HexNC, keys[0])
	require.NoError(err)

	sk, err := secp256k1.ToPrivateKey(skBytes)
	require.NoError(err)
	kc.Add(sk)

	expected := "Key[0]: Key: 0xb1ed77ad48555d49f03a7465f0685a7d86bfd5f3a3ccf1be01971ea8dec5471c Address: B6D4v1VtPYLbiUvYXtW4Px8oE9imC2vGW"
	require.Equal(expected, kc.String())
}

func TestKeychainPrefixedString(t *testing.T) {
	require := require.New(t)
	kc := NewKeychain()

	skBytes, err := formatting.Decode(formatting.HexNC, keys[0])
	require.NoError(err)

	sk, err := secp256k1.ToPrivateKey(skBytes)
	require.NoError(err)
	kc.Add(sk)

	expected := "xDKey[0]: Key: 0xb1ed77ad48555d49f03a7465f0685a7d86bfd5f3a3ccf1be01971ea8dec5471c Address: B6D4v1VtPYLbiUvYXtW4Px8oE9imC2vGW"
	require.Equal(expected, kc.PrefixedString("xD"))
}

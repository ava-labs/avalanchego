// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package secp256k1fx

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/crypto"
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
	assert := assert.New(t)
	assert.NotNil(NewKeychain())
}

func TestKeychainGetUnknownAddr(t *testing.T) {
	assert := assert.New(t)
	kc := NewKeychain()

	addr, _ := ids.ShortFromString(addrs[0])
	_, exists := kc.Get(addr)
	assert.False(exists)
}

func TestKeychainAdd(t *testing.T) {
	assert := assert.New(t)
	kc := NewKeychain()

	skBytes, err := formatting.Decode(formatting.HexNC, keys[0])
	assert.NoError(err)

	skIntff, err := kc.factory.ToPrivateKey(skBytes)
	assert.NoError(err)
	sk, ok := skIntff.(*crypto.PrivateKeySECP256K1R)
	assert.True(ok, "Factory should have returned secp256k1r private key")
	kc.Add(sk)

	addr, _ := ids.ShortFromString(addrs[0])
	rsk, exists := kc.Get(addr)
	assert.True(exists)
	assert.Equal(sk.Bytes(), rsk.Bytes())

	addrs := kc.Addresses()
	assert.Equal(1, addrs.Len())
	assert.True(addrs.Contains(addr))
}

func TestKeychainNew(t *testing.T) {
	assert := assert.New(t)
	kc := NewKeychain()

	assert.Equal(0, kc.Addresses().Len())

	sk, err := kc.New()
	assert.NoError(err)

	addr := sk.PublicKey().Address()

	addrs := kc.Addresses()
	assert.Equal(1, addrs.Len())
	assert.True(addrs.Contains(addr))
}

func TestKeychainMatch(t *testing.T) {
	assert := assert.New(t)
	kc := NewKeychain()

	sks := []*crypto.PrivateKeySECP256K1R{}
	for _, keyStr := range keys {
		skBytes, err := formatting.Decode(formatting.HexNC, keyStr)
		assert.NoError(err)

		skIntf, err := kc.factory.ToPrivateKey(skBytes)
		assert.NoError(err)
		sk, ok := skIntf.(*crypto.PrivateKeySECP256K1R)
		assert.True(ok, "Factory should have returned secp256k1r private key")
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
	assert.NoError(owners.Verify())

	_, _, ok := kc.Match(&owners, 0)
	assert.False(ok)

	kc.Add(sks[1])

	indices, keys, ok := kc.Match(&owners, 1)
	assert.True(ok)
	assert.Equal([]uint32{0}, indices)
	assert.Len(keys, 1)
	assert.Equal(sks[1].PublicKey().Address(), keys[0].PublicKey().Address())

	kc.Add(sks[2])

	indices, keys, ok = kc.Match(&owners, 1)
	assert.True(ok)
	assert.Equal([]uint32{0}, indices)
	assert.Len(keys, 1)
	assert.Equal(sks[1].PublicKey().Address(), keys[0].PublicKey().Address())
}

func TestKeychainSpendMint(t *testing.T) {
	assert := assert.New(t)
	kc := NewKeychain()

	sks := []*crypto.PrivateKeySECP256K1R{}
	for _, keyStr := range keys {
		skBytes, err := formatting.Decode(formatting.HexNC, keyStr)
		assert.NoError(err)

		skIntf, err := kc.factory.ToPrivateKey(skBytes)
		assert.NoError(err)
		sk, ok := skIntf.(*crypto.PrivateKeySECP256K1R)
		assert.True(ok, "Factory should have returned secp256k1r private key")
		sks = append(sks, sk)
	}

	mint := MintOutput{OutputOwners: OutputOwners{
		Threshold: 2,
		Addrs: []ids.ShortID{
			sks[1].PublicKey().Address(),
			sks[2].PublicKey().Address(),
		},
	}}
	assert.NoError(mint.Verify())

	_, _, err := kc.Spend(&mint, 0)
	assert.ErrorIs(err, errCantSpend)

	kc.Add(sks[0])
	kc.Add(sks[1])
	kc.Add(sks[2])

	vinput, keys, err := kc.Spend(&mint, 0)
	assert.NoError(err)

	input, ok := vinput.(*Input)
	assert.True(ok)
	assert.NoError(input.Verify())
	assert.Equal([]uint32{0, 1}, input.SigIndices)
	assert.Len(keys, 2)
	assert.Equal(sks[1].PublicKey().Address(), keys[0].PublicKey().Address())
	assert.Equal(sks[2].PublicKey().Address(), keys[1].PublicKey().Address())
}

func TestKeychainSpendTransfer(t *testing.T) {
	assert := assert.New(t)
	kc := NewKeychain()

	sks := []*crypto.PrivateKeySECP256K1R{}
	for _, keyStr := range keys {
		skBytes, err := formatting.Decode(formatting.HexNC, keyStr)
		assert.NoError(err)

		skIntf, err := kc.factory.ToPrivateKey(skBytes)
		assert.NoError(err)
		sk, ok := skIntf.(*crypto.PrivateKeySECP256K1R)
		assert.True(ok, "Factory should have returned secp256k1r private key")
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
	assert.NoError(transfer.Verify())

	_, _, err := kc.Spend(&transfer, 54321)
	assert.ErrorIs(err, errCantSpend)

	kc.Add(sks[0])
	kc.Add(sks[1])
	kc.Add(sks[2])

	_, _, err = kc.Spend(&transfer, 4321)
	assert.ErrorIs(err, errCantSpend)

	vinput, keys, err := kc.Spend(&transfer, 54321)
	assert.NoError(err)

	input, ok := vinput.(*TransferInput)
	assert.True(ok)
	assert.NoError(input.Verify())
	assert.Equal(uint64(12345), input.Amount())
	assert.Equal([]uint32{0, 1}, input.SigIndices)
	assert.Len(keys, 2)
	assert.Equal(sks[1].PublicKey().Address(), keys[0].PublicKey().Address())
	assert.Equal(sks[2].PublicKey().Address(), keys[1].PublicKey().Address())
}

func TestKeychainString(t *testing.T) {
	assert := assert.New(t)
	kc := NewKeychain()

	skBytes, err := formatting.Decode(formatting.HexNC, keys[0])
	assert.NoError(err)

	skIntf, err := kc.factory.ToPrivateKey(skBytes)
	assert.NoError(err)
	sk, ok := skIntf.(*crypto.PrivateKeySECP256K1R)
	assert.True(ok, "Factory should have returned secp256k1r private key")
	kc.Add(sk)

	expected := "Key[0]: Key: 0xb1ed77ad48555d49f03a7465f0685a7d86bfd5f3a3ccf1be01971ea8dec5471c Address: B6D4v1VtPYLbiUvYXtW4Px8oE9imC2vGW"
	assert.Equal(expected, kc.String())
}

func TestKeychainPrefixedString(t *testing.T) {
	assert := assert.New(t)
	kc := NewKeychain()

	skBytes, err := formatting.Decode(formatting.HexNC, keys[0])
	assert.NoError(err)

	skIntf, err := kc.factory.ToPrivateKey(skBytes)
	assert.NoError(err)
	sk, ok := skIntf.(*crypto.PrivateKeySECP256K1R)
	assert.True(ok, "Factory should have returned secp256k1r private key")
	kc.Add(sk)

	expected := "xDKey[0]: Key: 0xb1ed77ad48555d49f03a7465f0685a7d86bfd5f3a3ccf1be01971ea8dec5471c Address: B6D4v1VtPYLbiUvYXtW4Px8oE9imC2vGW"
	assert.Equal(expected, kc.PrefixedString("xD"))
}

// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package crypto

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"

	secp256k1 "github.com/decred/dcrd/dcrec/secp256k1/v3"

	"github.com/ava-labs/avalanchego/cache"
	"github.com/ava-labs/avalanchego/utils/formatting"
	"github.com/ava-labs/avalanchego/utils/hashing"
)

func TestRecover(t *testing.T) {
	f := FactorySECP256K1R{}
	key, _ := f.NewPrivateKey()

	msg := []byte{1, 2, 3}
	sig, _ := key.Sign(msg)

	pub := key.PublicKey()
	pubRec, _ := f.RecoverPublicKey(msg, sig)

	if !bytes.Equal(pub.Bytes(), pubRec.Bytes()) {
		t.Fatalf("Should have been equal")
	}
}

func TestCachedRecover(t *testing.T) {
	f := FactorySECP256K1R{Cache: cache.LRU{Size: 1}}
	key, _ := f.NewPrivateKey()

	msg := []byte{1, 2, 3}
	sig, _ := key.Sign(msg)

	pub1, _ := f.RecoverPublicKey(msg, sig)
	pub2, _ := f.RecoverPublicKey(msg, sig)

	if pub1 != pub2 {
		t.Fatalf("Should have returned the same public key")
	}
}

func TestExtensive(t *testing.T) {
	f := FactorySECP256K1R{}

	hash := hashing.ComputeHash256([]byte{1, 2, 3})
	for i := 0; i < 1000; i++ {
		if key, err := f.NewPrivateKey(); err != nil {
			t.Fatalf("Generated bad private key")
		} else if _, err := key.SignHash(hash); err != nil {
			t.Fatalf("Failed signing with:\n%s", formatting.DumpBytes(key.Bytes()))
		}
	}
}

func TestGenRecreate(t *testing.T) {
	f := FactorySECP256K1R{}

	for i := 0; i < 1000; i++ {
		sk, err := f.NewPrivateKey()
		if err != nil {
			t.Fatal(err)
		}
		skBytes := sk.Bytes()
		recoveredSk, err := f.ToPrivateKey(skBytes)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(sk.PublicKey().Address().Bytes(), recoveredSk.PublicKey().Address().Bytes()) {
			t.Fatalf("Wrong public key")
		}
	}
}

func TestVerifyMutatedSignature(t *testing.T) {
	factory := FactorySECP256K1R{}

	sk, err := factory.NewPrivateKey()
	assert.NoError(t, err)

	msg := []byte{'h', 'e', 'l', 'l', 'o'}

	sig, err := sk.Sign(msg)
	assert.NoError(t, err)

	var s secp256k1.ModNScalar
	s.SetByteSlice(sig[32:64])
	s.Negate()
	newSBytes := s.Bytes()
	copy(sig[32:], newSBytes[:])

	_, err = factory.RecoverPublicKey(msg, sig)
	assert.Error(t, err)
}

func TestPrivateKeySECP256K1RFromString(t *testing.T) {
	assert := assert.New(t)
	// PrivateKeySECP256K1RFromString(String)
	f := FactorySECP256K1R{}
	kIntf, _ := f.NewPrivateKey()
	k := kIntf.(*PrivateKeySECP256K1R)
	k2, err := PrivateKeySECP256K1RFromString(k.String())
	assert.NoError(err)
	assert.Equal(k, &k2)
	// String(PrivateKeySECP256K1RFromString)
	kStr := "PrivateKey-2qg4x8qM2s2qGNSXG"
	k2, err = PrivateKeySECP256K1RFromString(kStr)
	assert.NoError(err)
	assert.Equal(kStr, k2.String())
}

func TestPrivateKeySECP256K1RFromStringError(t *testing.T) {
	assert := assert.New(t)
	tests := []struct {
		in string
	}{
		{""},
		{"foo"},
		{"foobar"},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			_, err := PrivateKeySECP256K1RFromString(tt.in)
			assert.Error(err)
		})
	}
}

func TestPrivateKeySECP256K1RUnmarshalJSON(t *testing.T) {
	assert := assert.New(t)
	// UnmarshalJSON(MarshalJSON)
	f := FactorySECP256K1R{}
	kIntf, _ := f.NewPrivateKey()
	k := kIntf.(*PrivateKeySECP256K1R)
	kJSON, err := k.MarshalJSON()
	assert.NoError(err)
	k2 := PrivateKeySECP256K1R{}
	err = k2.UnmarshalJSON(kJSON)
	assert.NoError(err)
	assert.Equal(k, &k2)
	// MarshalJSON(UnmarshalJSON)
	kJSON = []byte("\"PrivateKey-2qg4x8qM2s2qGNSXG\"")
	err = k2.UnmarshalJSON(kJSON)
	assert.NoError(err)
	kJSON2, err := k2.MarshalJSON()
	assert.NoError(err)
	assert.Equal(kJSON, kJSON2)
}

func TestPrivateKeySECP256K1RUnmarshalJSONError(t *testing.T) {
	assert := assert.New(t)
	tests := []struct {
		label string
		in    []byte
	}{
		{
			"missing start quote",
			[]byte("PrivateKey-9tLMkeWFhWXd8QZc4rSiS5meuVXF5kRsz\""),
		},
		{
			"missing end quote",
			[]byte("\"PrivateKey-9tLMkeWFhWXd8QZc4rSiS5meuVXF5kRsz"),
		},
		{
			"PrivateKey-",
			[]byte("\"PrivateKey-\""),
		},
		{
			"PrivateKey-1",
			[]byte("\"PrivateKey-1\""),
		},
	}
	for _, tt := range tests {
		t.Run(tt.label, func(t *testing.T) {
			foo := PrivateKeySECP256K1R{}
			err := foo.UnmarshalJSON(tt.in)
			assert.Error(err)
		})
	}
}

// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package crypto

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/require"

	secp256k1 "github.com/decred/dcrd/dcrec/secp256k1/v3"

	"github.com/ava-labs/avalanchego/cache"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/hashing"
)

func TestRecover(t *testing.T) {
	require := require.New(t)

	f := FactorySECP256K1R{}
	key, err := f.NewPrivateKey()
	require.NoError(err)

	msg := []byte{1, 2, 3}
	sig, err := key.Sign(msg)
	require.NoError(err)

	pub := key.PublicKey()
	pubRec, err := f.RecoverPublicKey(msg, sig)
	require.NoError(err)

	if !bytes.Equal(pub.Bytes(), pubRec.Bytes()) {
		t.Fatalf("Should have been equal")
	}
}

func TestCachedRecover(t *testing.T) {
	require := require.New(t)

	f := FactorySECP256K1R{Cache: cache.LRU[ids.ID, *PublicKeySECP256K1R]{Size: 1}}
	key, err := f.NewPrivateKey()
	require.NoError(err)

	msg := []byte{1, 2, 3}
	sig, err := key.Sign(msg)
	require.NoError(err)

	pub1, err := f.RecoverPublicKey(msg, sig)
	require.NoError(err)
	pub2, err := f.RecoverPublicKey(msg, sig)
	require.NoError(err)

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
			t.Fatalf("Failed signing with:\n0x%x", key.Bytes())
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
	require.NoError(t, err)

	msg := []byte{'h', 'e', 'l', 'l', 'o'}

	sig, err := sk.Sign(msg)
	require.NoError(t, err)

	var s secp256k1.ModNScalar
	s.SetByteSlice(sig[32:64])
	s.Negate()
	newSBytes := s.Bytes()
	copy(sig[32:], newSBytes[:])

	_, err = factory.RecoverPublicKey(msg, sig)
	require.Error(t, err)
}

func TestPrivateKeySECP256K1RUnmarshalJSON(t *testing.T) {
	require := require.New(t)

	f := FactorySECP256K1R{}
	keyIntf, err := f.NewPrivateKey()
	require.NoError(err)
	key := keyIntf.(*PrivateKeySECP256K1R)

	keyJSON, err := key.MarshalJSON()
	require.NoError(err)

	key2 := PrivateKeySECP256K1R{}
	err = key2.UnmarshalJSON(keyJSON)
	require.NoError(err)
	require.Equal(key.PublicKey().Address(), key2.PublicKey().Address())
}

func TestPrivateKeySECP256K1RUnmarshalJSONError(t *testing.T) {
	tests := []struct {
		label string
		in    []byte
	}{
		{
			"too short",
			[]byte(`"`),
		},
		{
			"missing start quote",
			[]byte(`PrivateKey-ewoqjP7PxY4yr3iLTpLisriqt94hdyDFNgchSxGGztUrTXtNN"`),
		},
		{
			"missing end quote",
			[]byte(`"PrivateKey-ewoqjP7PxY4yr3iLTpLisriqt94hdyDFNgchSxGGztUrTXtNN`),
		},
		{
			"incorrect prefix",
			[]byte(`"PrivateKfy-ewoqjP7PxY4yr3iLTpLisriqt94hdyDFNgchSxGGztUrTXtNN"`),
		},
		{
			`"PrivateKey-"`,
			[]byte(`"PrivateKey-"`),
		},
		{
			`"PrivateKey-1"`,
			[]byte(`"PrivateKey-1"`),
		},
	}
	for _, tt := range tests {
		t.Run(tt.label, func(t *testing.T) {
			require := require.New(t)

			foo := PrivateKeySECP256K1R{}
			err := foo.UnmarshalJSON(tt.in)
			require.Error(err)
		})
	}
}

func TestSigning(t *testing.T) {
	tests := []struct {
		msg []byte
		sig []byte
	}{
		{
			[]byte("hello world"),
			[]byte{
				0x17, 0x8c, 0xb6, 0x09, 0x6b, 0x3c, 0xa5, 0x82,
				0x0a, 0x4c, 0x6e, 0xce, 0xdf, 0x15, 0xb6, 0x8b,
				0x6f, 0x50, 0xe2, 0x52, 0xc2, 0xb6, 0x4f, 0x37,
				0x74, 0x88, 0x86, 0x02, 0xcc, 0x9f, 0xa0, 0x8c,
				0x5d, 0x01, 0x9d, 0x82, 0xfd, 0xde, 0x95, 0xfd,
				0xf2, 0x34, 0xaa, 0x2d, 0x12, 0xad, 0x79, 0xb5,
				0xab, 0xb3, 0x45, 0xfe, 0x95, 0x3a, 0x9f, 0x72,
				0xf7, 0x09, 0x14, 0xfd, 0x31, 0x39, 0x06, 0x3b,
				0x00,
			},
		},
		{
			[]byte("scooby doo"),
			[]byte{
				0xc2, 0x57, 0x3f, 0x29, 0xb0, 0xd1, 0x7a, 0xe7,
				0x00, 0x9a, 0x9f, 0x17, 0xa4, 0x55, 0x8d, 0x32,
				0x46, 0x2e, 0x5b, 0x8d, 0x05, 0x9e, 0x38, 0x32,
				0xec, 0xb0, 0x32, 0x54, 0x1a, 0xbc, 0x7d, 0xaf,
				0x57, 0x51, 0xf9, 0x6b, 0x85, 0x71, 0xbc, 0xb7,
				0x18, 0xd2, 0x6b, 0xe8, 0xed, 0x8d, 0x59, 0xb0,
				0xd6, 0x03, 0x69, 0xab, 0x57, 0xac, 0xc0, 0xf7,
				0x13, 0x3b, 0x21, 0x94, 0x56, 0x03, 0x8e, 0xc7,
				0x01,
			},
		},
		{
			[]byte("a really long string"),
			[]byte{
				0x1b, 0xf5, 0x61, 0xc3, 0x60, 0x07, 0xd2, 0xa6,
				0x12, 0x68, 0xe9, 0xe1, 0x3a, 0x90, 0x2a, 0x9c,
				0x2b, 0xa4, 0x3e, 0x28, 0xf8, 0xd4, 0x75, 0x54,
				0x21, 0x57, 0x11, 0xdc, 0xdc, 0xc6, 0xd3, 0x5e,
				0x78, 0x43, 0x18, 0xf6, 0x22, 0x91, 0x37, 0x3c,
				0x95, 0x77, 0x9f, 0x67, 0x94, 0x91, 0x0a, 0x44,
				0x16, 0xbf, 0xa3, 0xae, 0x9f, 0x25, 0xfa, 0x34,
				0xa0, 0x14, 0xea, 0x9c, 0x6f, 0xe0, 0x20, 0x37,
				0x00,
			},
		},
	}

	key := BuildTestKeys()[0]

	for _, tt := range tests {
		t.Run(string(tt.msg), func(t *testing.T) {
			require := require.New(t)

			bytes, err := key.Sign(tt.msg)
			require.NoError(err)
			require.Equal(tt.sig, bytes)
		})
	}
}

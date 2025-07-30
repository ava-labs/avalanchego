// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package secp256k1

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/utils/cb58"
	"github.com/ava-labs/avalanchego/utils/hashing"

	secp256k1 "github.com/decred/dcrd/dcrec/secp256k1/v4"
)

func TestRecover(t *testing.T) {
	require := require.New(t)

	key, err := NewPrivateKey()
	require.NoError(err)

	msg := []byte{1, 2, 3}
	sig, err := key.Sign(msg)
	require.NoError(err)

	pub := key.PublicKey()
	pubRec, err := RecoverPublicKey(msg, sig)
	require.NoError(err)

	require.Equal(pub, pubRec)

	require.True(pub.Verify(msg, sig))
}

func TestCachedRecover(t *testing.T) {
	require := require.New(t)

	key, err := NewPrivateKey()
	require.NoError(err)

	msg := []byte{1, 2, 3}
	sig, err := key.Sign(msg)
	require.NoError(err)

	r := NewRecoverCache(1)
	pub1, err := r.RecoverPublicKey(msg, sig)
	require.NoError(err)
	pub2, err := r.RecoverPublicKey(msg, sig)
	require.NoError(err)

	require.Equal(key.PublicKey(), pub1)
	require.Equal(key.PublicKey(), pub2)
}

func TestExtensive(t *testing.T) {
	require := require.New(t)

	hash := hashing.ComputeHash256([]byte{1, 2, 3})
	for i := 0; i < 1000; i++ {
		key, err := NewPrivateKey()
		require.NoError(err)

		_, err = key.SignHash(hash)
		require.NoError(err)
	}
}

func TestGenRecreate(t *testing.T) {
	require := require.New(t)

	for i := 0; i < 1000; i++ {
		sk, err := NewPrivateKey()
		require.NoError(err)

		skBytes := sk.Bytes()
		recoveredSk, err := ToPrivateKey(skBytes)
		require.NoError(err)

		require.Equal(sk.PublicKey(), recoveredSk.PublicKey())
	}
}

func TestVerifyMutatedSignature(t *testing.T) {
	require := require.New(t)

	sk, err := NewPrivateKey()
	require.NoError(err)

	msg := []byte{'h', 'e', 'l', 'l', 'o'}
	sig, err := sk.Sign(msg)
	require.NoError(err)

	var s secp256k1.ModNScalar
	s.SetByteSlice(sig[32:64])
	s.Negate()
	newSBytes := s.Bytes()
	copy(sig[32:], newSBytes[:])

	_, err = RecoverPublicKey(msg, sig)
	require.ErrorIs(err, errMutatedSig)
}

func TestPrivateKeySECP256K1RUnmarshalJSON(t *testing.T) {
	require := require.New(t)

	key, err := NewPrivateKey()
	require.NoError(err)

	keyJSON, err := key.MarshalJSON()
	require.NoError(err)

	key2 := PrivateKey{}
	require.NoError(key2.UnmarshalJSON(keyJSON))
	require.Equal(key.PublicKey(), key2.PublicKey())
}

func TestPrivateKeySECP256K1RUnmarshalJSONError(t *testing.T) {
	tests := []struct {
		label string
		in    []byte
		err   error
	}{
		{
			label: "too short",
			in:    []byte(`"`),
			err:   errMissingQuotes,
		},
		{
			label: "missing start quote",
			in:    []byte(`PrivateKey-ewoqjP7PxY4yr3iLTpLisriqt94hdyDFNgchSxGGztUrTXtNN"`),
			err:   errMissingQuotes,
		},
		{
			label: "missing end quote",
			in:    []byte(`"PrivateKey-ewoqjP7PxY4yr3iLTpLisriqt94hdyDFNgchSxGGztUrTXtNN`),
			err:   errMissingQuotes,
		},
		{
			label: "incorrect prefix",
			in:    []byte(`"PrivateKfy-ewoqjP7PxY4yr3iLTpLisriqt94hdyDFNgchSxGGztUrTXtNN"`),
			err:   errMissingKeyPrefix,
		},
		{
			label: `"PrivateKey-"`,
			in:    []byte(`"PrivateKey-"`),
			err:   cb58.ErrBase58Decoding,
		},
		{
			label: `"PrivateKey-1"`,
			in:    []byte(`"PrivateKey-1"`),
			err:   cb58.ErrMissingChecksum,
		},
		{
			label: `"PrivateKey-1"`,
			in:    []byte(`"PrivateKey-1"`),
			err:   cb58.ErrMissingChecksum,
		},
		{
			label: `"PrivateKey-1"`,
			in:    []byte(`"PrivateKey-45PJLL"`),
			err:   errInvalidPrivateKeyLength,
		},
	}
	for _, tt := range tests {
		t.Run(tt.label, func(t *testing.T) {
			require := require.New(t)

			foo := PrivateKey{}
			err := foo.UnmarshalJSON(tt.in)
			require.ErrorIs(err, tt.err)
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

	key := TestKeys()[0]

	for _, tt := range tests {
		t.Run(string(tt.msg), func(t *testing.T) {
			require := require.New(t)

			bytes, err := key.Sign(tt.msg)
			require.NoError(err)
			require.Equal(tt.sig, bytes)
		})
	}
}

func TestExportedMethods(t *testing.T) {
	require := require.New(t)

	key := TestKeys()[0]

	pubKey := key.PublicKey()
	require.Equal("111111111111111111116DBWJs", pubKey.addr.String())
	require.Equal("Q4MzFZZDPHRPAHFeDs3NiyyaZDvxHKivf", pubKey.Address().String())
	require.Equal("Q4MzFZZDPHRPAHFeDs3NiyyaZDvxHKivf", pubKey.addr.String())
	require.Equal("Q4MzFZZDPHRPAHFeDs3NiyyaZDvxHKivf", key.Address().String())

	expectedPubKeyBytes := []byte{
		0x03, 0x73, 0x93, 0x53, 0x47, 0x88, 0x44, 0x78,
		0xe4, 0x94, 0x5c, 0xd0, 0xfd, 0x94, 0x8e, 0xcf,
		0x08, 0x8b, 0x94, 0xdf, 0xc9, 0x20, 0x74, 0xf0,
		0xfb, 0x03, 0xda, 0x6f, 0x4d, 0xbc, 0x94, 0x35,
		0x7d,
	}
	require.Equal(expectedPubKeyBytes, pubKey.bytes)

	expectedPubKey, err := ToPublicKey(expectedPubKeyBytes)
	require.NoError(err)
	require.Equal(expectedPubKey.Address(), pubKey.Address())
	require.Equal(expectedPubKeyBytes, expectedPubKey.Bytes())

	expectedECDSAParams := struct {
		X []byte
		Y []byte
	}{
		[]byte{
			0x73, 0x93, 0x53, 0x47, 0x88, 0x44, 0x78, 0xe4,
			0x94, 0x5c, 0xd0, 0xfd, 0x94, 0x8e, 0xcf, 0x08,
			0x8b, 0x94, 0xdf, 0xc9, 0x20, 0x74, 0xf0, 0xfb,
			0x03, 0xda, 0x6f, 0x4d, 0xbc, 0x94, 0x35, 0x7d,
		},
		[]byte{
			0x78, 0xe7, 0x39, 0x45, 0x6c, 0x3b, 0xdb, 0x9e,
			0xe9, 0xb2, 0xa9, 0xf2, 0x84, 0xfa, 0x64, 0x32,
			0xd8, 0x4e, 0xf0, 0xfa, 0x3f, 0x82, 0xf5, 0x56,
			0x10, 0x40, 0x71, 0x7f, 0x1f, 0x5e, 0x8e, 0x27,
		},
	}
	require.Equal(expectedECDSAParams.X, pubKey.ToECDSA().X.Bytes())
	require.Equal(expectedECDSAParams.Y, pubKey.ToECDSA().Y.Bytes())

	require.Equal(expectedECDSAParams.X, key.ToECDSA().X.Bytes())
	require.Equal(expectedECDSAParams.Y, key.ToECDSA().Y.Bytes())
}

func FuzzVerifySignature(f *testing.F) {
	f.Fuzz(func(t *testing.T, data []byte) {
		require := require.New(t)

		privateKey, err := NewPrivateKey()
		require.NoError(err)

		publicKey := privateKey.PublicKey()

		sig, err := privateKey.Sign(data)
		require.NoError(err)

		recoveredPublicKey, err := RecoverPublicKey(data, sig)
		require.NoError(err)

		require.Equal(publicKey, recoveredPublicKey)
	})
}

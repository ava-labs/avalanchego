// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package crypto

import (
	"crypto"
	"crypto/rand"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/staking"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"github.com/ava-labs/avalanchego/utils/hashing"
)

func TestTLSVerifier(t *testing.T) {
	type args struct {
		message, signature []byte
	}

	ipBytes := []byte{1, 2, 3, 4, 5}

	cert, err := staking.NewTLSCert()
	require.NoError(t, err)

	signer := cert.PrivateKey.(crypto.Signer)
	sig, err := signer.Sign(rand.Reader, hashing.ComputeHash256(ipBytes),
		crypto.SHA256)
	require.NoError(t, err)

	tests := []struct {
		name        string
		args        args
		expectedErr bool
	}{
		{
			name: "fail - nil signature",
			args: args{
				signature: nil,
			},
			expectedErr: true,
		},
		{
			name: "fail - empty signature",
			args: args{
				signature: []byte{},
			},
			expectedErr: true,
		},
		{
			name: "fail - nil msg",
			args: args{
				message: nil,
			},
			expectedErr: true,
		},
		{
			name: "fail - empty msg",
			args: args{
				message: []byte{},
			},
			expectedErr: true,
		},
		{
			name: "fail - invalid signature",
			args: args{
				message:   ipBytes,
				signature: []byte("garbage"),
			},
			expectedErr: true,
		},
		{
			name: "success - valid signature",
			args: args{
				message:   ipBytes,
				signature: sig,
			},
			expectedErr: false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			r := require.New(t)
			verifier := TLSVerifier{
				Cert: cert.Leaf,
			}

			err := verifier.Verify(test.args.message, test.args.signature)
			if test.expectedErr {
				r.Error(err)
			} else {
				r.NoError(err)
			}
		})
	}
}

func TestBLSVerifier(t *testing.T) {
	type args struct {
		message, signature []byte
	}

	ipBytes := []byte{1, 2, 3, 4, 5}

	sk, err := bls.NewSecretKey()
	require.NoError(t, err)
	pk := bls.PublicFromSecretKey(sk)

	sig := bls.SignatureToBytes(bls.Sign(sk, ipBytes))

	tests := []struct {
		name        string
		args        args
		expectedErr bool
	}{
		{
			name: "fail - nil msg",
			args: args{
				message: nil,
			},
			expectedErr: true,
		},
		{
			name: "fail - empty msg",
			args: args{
				message: []byte{},
			},
			expectedErr: true,
		},
		{
			name: "fail - missing signature",
			args: args{
				signature: nil,
			},
			expectedErr: true,
		},
		{
			name: "fail - empty signature",
			args: args{
				signature: []byte{},
			},
			expectedErr: true,
		},
		{
			name: "fail - invalid signature",
			args: args{
				message:   ipBytes,
				signature: []byte("garbage"),
			},
			expectedErr: true,
		},
		{
			name: "success - valid msg",
			args: args{
				message:   ipBytes,
				signature: sig,
			},
			expectedErr: false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			r := require.New(t)

			verifier := BLSVerifier{
				PublicKey: pk,
			}

			err := verifier.Verify(test.args.message, test.args.signature)
			if test.expectedErr {
				r.Error(err)
			} else {
				r.NoError(err)
			}
		})
	}
}

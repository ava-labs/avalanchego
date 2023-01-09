// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package peer

import (
	"crypto"
	"crypto/rand"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/staking"
	"github.com/ava-labs/avalanchego/utils/hashing"
)

func TestTLSVerifier(t *testing.T) {
	type args struct {
		message   []byte
		signature Signature
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
			name: "nil ipBytes",
			args: args{
				message:   nil,
				signature: Signature{TLSSignature: sig},
			},
			expectedErr: true,
		},
		{
			name: "invalid ipBytes",
			args: args{
				message:   ipBytes,
				signature: Signature{TLSSignature: []byte("garbage")},
			},
			expectedErr: true,
		},
		{
			name: "valid ipBytes",
			args: args{
				message:   ipBytes,
				signature: Signature{TLSSignature: sig},
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

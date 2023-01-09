// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package peer

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/utils/crypto/bls"
)

func TestBLSVerifier(t *testing.T) {
	type args struct {
		message   []byte
		signature Signature
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
			name: "nil ipBytes",
			args: args{
				message:   nil,
				signature: Signature{BLSSignature: sig},
			},
			expectedErr: true,
		},
		{
			name: "invalid ipBytes",
			args: args{
				message:   ipBytes,
				signature: Signature{BLSSignature: []byte("garbage")},
			},
			expectedErr: true,
		},
		{
			name: "valid ipBytes",
			args: args{
				message:   ipBytes,
				signature: Signature{BLSSignature: sig},
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

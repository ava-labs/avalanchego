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
		name    string
		args    args
		wantErr func(r *require.Assertions, err error)
	}{
		{
			name: "fail - nil signature",
			args: args{
				signature: Signature{TLSSignature: nil},
			},
			wantErr: func(r *require.Assertions, err error) {
				r.ErrorIs(err, errMissingSignature)
			},
		},
		{
			name: "fail - empty signature",
			args: args{
				signature: Signature{TLSSignature: []byte{}},
			},
			wantErr: func(r *require.Assertions, err error) {
				r.ErrorIs(err, errMissingSignature)
			},
		},
		{
			name: "fail - nil ipBytes",
			args: args{
				message: nil,
			},
			wantErr: func(r *require.Assertions, err error) {
				r.Error(err)
			},
		},
		{
			name: "fail - empty ipBytes",
			args: args{
				message: []byte{},
			},
			wantErr: func(r *require.Assertions, err error) {
				r.Error(err)
			},
		},
		{
			name: "fail - invalid signature",
			args: args{
				message:   ipBytes,
				signature: Signature{TLSSignature: []byte("garbage")},
			},
			wantErr: func(r *require.Assertions, err error) {
				r.Error(err)
			},
		},
		{
			name: "success - valid signature",
			args: args{
				message:   ipBytes,
				signature: Signature{TLSSignature: sig},
			},
			wantErr: func(r *require.Assertions, err error) {
				r.NoError(err)
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			r := require.New(t)
			verifier := TLSVerifier{
				Cert: cert.Leaf,
			}

			test.wantErr(r, verifier.Verify(test.args.message, test.args.signature))
		})
	}
}

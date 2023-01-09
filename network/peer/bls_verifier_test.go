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
		name    string
		args    args
		wantErr func(r *require.Assertions, err error)
	}{
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
			name: "fail - missing signature",
			args: args{
				signature: Signature{BLSSignature: nil},
			},
			wantErr: func(r *require.Assertions, err error) {
				r.ErrorIs(err, errMissingSignature)
			},
		},
		{
			name: "fail - empty signature",
			args: args{
				signature: Signature{BLSSignature: []byte{}},
			},
			wantErr: func(r *require.Assertions, err error) {
				r.ErrorIs(err, errMissingSignature)
			},
		},
		{
			name: "fail - invalid signature",
			args: args{
				message:   ipBytes,
				signature: Signature{BLSSignature: []byte("garbage")},
			},
			wantErr: func(r *require.Assertions, err error) {
				r.Error(err)
			},
		},
		{
			name: "success - valid ipBytes",
			args: args{
				message:   ipBytes,
				signature: Signature{BLSSignature: sig},
			},
			wantErr: func(r *require.Assertions, err error) {
				r.NoError(err)
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			r := require.New(t)

			verifier := BLSVerifier{
				PublicKey: pk,
			}

			test.wantErr(r, verifier.Verify(test.args.message, test.args.signature))
		})
	}
}

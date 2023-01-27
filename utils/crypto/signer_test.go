// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package crypto

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/staking"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
)

func TestTLSSigner(t *testing.T) {
	type args struct {
		msg []byte
	}

	tests := []struct {
		name string
		args args
	}{
		{
			name: "nil msg",
			args: args{
				msg: nil,
			},
		},
		{
			name: "empty msg",
			args: args{
				msg: []byte{},
			},
		},
		{
			name: "non-empty msg",
			args: args{
				msg: []byte{1, 2, 3, 3, 5},
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			r := require.New(t)

			// generate a tls cert
			cert, err := staking.NewTLSCert()
			r.NoError(err)
			signer, err := NewTLSSigner(cert)
			r.NoError(err)

			// sign the ip with the cert
			sig, err := signer.Sign(test.args.msg)
			r.NoError(err)

			// verify the signature of the ip against the cert
			r.NoError(cert.Leaf.CheckSignature(cert.Leaf.SignatureAlgorithm,
				test.args.msg, sig))
		})
	}
}

func TestBLSSigner(t *testing.T) {
	type args struct {
		msg       []byte
		signature []byte
	}

	tests := []struct {
		name string
		args args
	}{
		{
			name: "nil ip",
			args: args{
				msg: nil,
			},
		},
		{
			name: "empty ip",
			args: args{
				msg: []byte{},
			},
		},
		{
			name: "non-empty ip",
			args: args{
				msg: []byte{1, 2, 3, 3, 5},
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			r := require.New(t)

			// generate a bls key
			sk, err := bls.NewSecretKey()
			r.NoError(err)

			// sign the ip
			signer := NewBLSSigner(sk)
			sig := signer.Sign(test.args.msg)

			// verify the signature of the ip against the public key
			blsSig, err := bls.SignatureFromBytes(sig)
			r.NoError(err)

			pk := bls.PublicFromSecretKey(sk)
			r.True(bls.Verify(pk, blsSig, test.args.msg))
		})
	}
}

func TestBLSSigner_MissingKey(t *testing.T) {
	r := require.New(t)

	signer := NewBLSSigner(nil)
	msg := []byte("message")
	sig := signer.Sign(msg)

	r.Nil(sig)
}

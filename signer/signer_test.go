// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package signer

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/utils/crypto/bls"
)

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
		{
			name: "overwrite previous signature",
			args: args{
				msg:       []byte{1, 2, 3, 3, 5},
				signature: []byte{6, 7, 8, 9, 0},
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

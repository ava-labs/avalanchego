// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package bls

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSign(t *testing.T) {
	type args struct {
		msg []byte
	}

	tests := []struct {
		name string
		args args
	}{
		{
			name: "nil message",
			args: args{
				msg: nil,
			},
		},
		{
			name: "empty message",
			args: args{
				msg: []byte{},
			},
		},
		{
			name: "non-empty message",
			args: args{
				msg: []byte{1, 2, 3, 3, 5},
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			r := require.New(t)

			sk, err := NewSecretKey()
			r.NoError(err)

			signer := NewSigner(sk)
			sig, err := signer.Sign(test.args.msg)
			r.NoError(err)

			sigBytes, err := SignatureFromBytes(sig)
			r.NoError(err)

			expected := Sign(sk, test.args.msg)
			r.Equal(expected, sigBytes)
		})
	}
}

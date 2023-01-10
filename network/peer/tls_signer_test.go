// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package peer

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/staking"
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

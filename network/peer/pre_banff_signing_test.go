// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package peer

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/staking"
)

func TestPreBanffSigner_TLS(t *testing.T) {
	tests := []struct {
		name string
		msg  []byte
	}{
		{
			name: "nil msg",
			msg:  nil,
		},
		{
			name: "empty msg",
			msg:  []byte{},
		},
		{
			name: "populated msg",
			msg:  []byte{0, 1, 2, 3, 4},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			r := require.New(t)

			cert, err := staking.NewTLSCert()
			r.NoError(err)
			signer, err := NewPreBanffSigner(cert)
			r.NoError(err)

			sig, err := signer.SignTLS(test.msg)
			r.NoError(err)

			verifier := NewPreBanffVerifier(cert.Leaf)

			r.NoError(verifier.VerifyTLS(test.msg, sig))
		})
	}
}

func TestPreBanffSigner_BLS(t *testing.T) {
	tests := []struct {
		name string
		msg  []byte
	}{
		{
			name: "nil msg",
			msg:  nil,
		},
		{
			name: "empty msg",
			msg:  []byte{},
		},
		{
			name: "populated msg",
			msg:  []byte{0, 1, 2, 3, 4},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			r := require.New(t)

			cert, err := staking.NewTLSCert()
			r.NoError(err)
			signer, err := NewPreBanffSigner(cert)
			r.NoError(err)

			sig := signer.SignBLS(test.msg)
			verifier := NewPreBanffVerifier(cert.Leaf)

			r.NoError(verifier.VerifyBLS(test.msg, sig))
		})
	}
}

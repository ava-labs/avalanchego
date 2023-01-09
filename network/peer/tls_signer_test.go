package peer

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/staking"
)

func TestTLSSigner(t *testing.T) {
	type args struct {
		ipBytes   []byte
		signature Signature
	}

	tests := []struct {
		name string
		args args
	}{
		{
			name: "nil ip",
			args: args{
				ipBytes:   nil,
				signature: Signature{},
			},
		},
		{
			name: "empty ip",
			args: args{
				ipBytes:   []byte{},
				signature: Signature{},
			},
		},
		{
			name: "non-empty ip",
			args: args{
				ipBytes:   []byte{1, 2, 3, 3, 5},
				signature: Signature{},
			},
		},
		{
			name: "overwrite previous signature",
			args: args{
				ipBytes: []byte{1, 2, 3, 3, 5},
				signature: Signature{
					TLSSignature: []byte{6, 7, 8, 9, 0},
				},
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
			sig, err := signer.Sign(test.args.ipBytes, test.args.signature)
			r.NoError(err)

			// verify the signature of the ip against the cert
			r.NoError(cert.Leaf.CheckSignature(cert.Leaf.SignatureAlgorithm,
				test.args.ipBytes, sig.TLSSignature))
		})
	}
}

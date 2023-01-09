// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package peer

import (
	"fmt"
	"testing"

	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/require"
)

func TestIPSigners(t *testing.T) {
	errFoobar := fmt.Errorf("foobar")

	type args struct {
		ipBytes []byte
		sig     Signature
	}

	type result struct {
		sig Signature
		err error
	}

	type expected struct {
		sig Signature
		err error
	}

	tests := []struct {
		name     string
		args     args
		result0  result
		result1  result
		expected expected
	}{
		{
			name: "fail - all fail",
			result0: result{
				err: errFoobar,
			},
			result1: result{
				err: errFoobar,
			},
			expected: expected{
				err: errFoobar,
			},
		},
		{
			name: "fail - first signer",
			result0: result{
				err: errFoobar,
			},
			expected: expected{
				err: errFoobar,
			},
		},
		{
			name: "fail - second signer",
			result1: result{
				err: errFoobar,
			},
			expected: expected{
				err: errFoobar,
			},
		},
		{
			name: "success - all signers pass",
			result0: result{
				sig: Signature{
					TLSSignature: []byte("tls"),
					BLSSignature: nil,
				},
			},
			result1: result{
				sig: Signature{
					TLSSignature: []byte("tls"),
					BLSSignature: []byte("bls"),
				},
			},
			expected: expected{
				sig: Signature{
					TLSSignature: []byte("tls"),
					BLSSignature: []byte("bls"),
				},
				err: nil,
			},
		},
		{
			name: "success - signature overridden",
			args: args{
				ipBytes: []byte("foobar"),
				sig: Signature{
					TLSSignature: []byte("old tls"),
					BLSSignature: []byte("old bls"),
				},
			},
			result0: result{
				sig: Signature{
					TLSSignature: []byte("tls"),
					BLSSignature: nil,
				},
			},
			result1: result{
				sig: Signature{
					TLSSignature: []byte("tls"),
					BLSSignature: []byte("bls"),
				},
			},
			expected: expected{
				sig: Signature{
					TLSSignature: []byte("tls"),
					BLSSignature: []byte("bls"),
				},
				err: nil,
			},
		},
	}

	for _, test := range tests {
		r := require.New(t)
		ctrl := gomock.NewController(t)

		signer0 := NewMockIPSigner(ctrl)
		signer0.EXPECT().Sign(gomock.Any(), gomock.Any()).
			Return(test.result0.sig, test.result0.err).AnyTimes()

		signer1 := NewMockIPSigner(ctrl)
		signer1.EXPECT().Sign(gomock.Any(), gomock.Any()).
			Return(test.result1.sig, test.result1.err).AnyTimes()

		signer := NewIPSigners(signer0, signer1)

		sig, err := signer.Sign(test.args.ipBytes, test.args.sig)
		r.Equal(test.expected.sig, sig)
		r.Equal(test.expected.err, err)
	}
}

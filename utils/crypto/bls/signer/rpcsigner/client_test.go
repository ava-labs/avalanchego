// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package rpcsigner

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"

	"github.com/ava-labs/avalanchego/buf/proto/pb/signer"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"github.com/ava-labs/avalanchego/utils/crypto/bls/signer/localsigner"
)

var (
	validSignatureMsg        = []byte("valid")
	uncompressedSignatureMsg = []byte("uncompressed")
	emptySignatureMsg        = []byte("empty")
	noSignatureMsg           = []byte("none")
)

func newSigner(t *testing.T) *Client {
	localSigner, err := localsigner.New()
	require.NoError(t, err)

	return &Client{
		client: &stubClient{
			signer: localSigner,
		},
		pk: localSigner.PublicKey(),
	}
}

func TestValidSignature(t *testing.T) {
	client := newSigner(t)
	sig, err := client.Sign(validSignatureMsg)
	require.NoError(t, err)
	ok := bls.Verify(client.PublicKey(), sig, validSignatureMsg)
	require.True(t, ok)
}

type test struct {
	name string
	msg  []byte
	err  error
}

var tests = []test{
	{
		name: "uncompressed",
		msg:  uncompressedSignatureMsg,
		err:  bls.ErrFailedSignatureDecompress,
	},
	{
		name: "empty",
		msg:  emptySignatureMsg,
		err:  bls.ErrFailedSignatureDecompress,
	},
	{
		name: "none",
		msg:  noSignatureMsg,
		err:  bls.ErrFailedSignatureDecompress,
	},
}

func TestInvalidSignature(t *testing.T) {
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			client := newSigner(t)
			sig, err := client.Sign(test.msg)
			require.Nil(t, sig)
			require.ErrorIs(t, err, test.err)
		})
	}
}

func TestValidPOPSignature(t *testing.T) {
	client := newSigner(t)
	sig, err := client.SignProofOfPossession(validSignatureMsg)
	require.NoError(t, err)
	ok := bls.VerifyProofOfPossession(client.PublicKey(), sig, validSignatureMsg)
	require.True(t, ok)
}

func TestInvalidPOPSignature(t *testing.T) {
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			client := newSigner(t)
			sig, err := client.SignProofOfPossession(test.msg)
			require.Nil(t, sig)
			require.ErrorIs(t, err, test.err)
		})
	}
}

type stubClient struct {
	signer *localsigner.LocalSigner
}

func (*stubClient) PublicKey(_ context.Context, _ *signer.PublicKeyRequest, _ ...grpc.CallOption) (*signer.PublicKeyResponse, error) {
	// this function is not used in the tests, however it's required to implement the `signer.SignerClient` interface
	return nil, nil
}

// for the `Sign` and `SignProofOfPossession` methods, we're using the same logic where
// we match on the message to determine the type of response we want to test
func (c *stubClient) Sign(_ context.Context, in *signer.SignRequest, _ ...grpc.CallOption) (*signer.SignResponse, error) {
	switch string(in.Message) {
	case string(validSignatureMsg):
		sig, err := c.signer.Sign(in.Message)
		if err != nil {
			return nil, err
		}

		return &signer.SignResponse{
			Signature: bls.SignatureToBytes(sig),
		}, nil
	// the client expects a compressed signature so this signature is invalid
	case string(uncompressedSignatureMsg):
		sig, err := c.signer.Sign(in.Message)
		if err != nil {
			return nil, err
		}

		bytes := sig.Serialize()

		return &signer.SignResponse{
			// here, we're using the compressed signature length
			// we could also use the full signature
			Signature: bytes[:bls.SignatureLen],
		}, nil
	case string(emptySignatureMsg):
		return &signer.SignResponse{
			Signature: []byte{},
		}, nil
	case string(noSignatureMsg):
		return &signer.SignResponse{}, nil
	default:
		return nil, errors.New("invalid case")
	}
}

// see comments from `Sign` function above
func (c *stubClient) SignProofOfPossession(_ context.Context, in *signer.SignProofOfPossessionRequest, _ ...grpc.CallOption) (*signer.SignProofOfPossessionResponse, error) {
	switch string(in.Message) {
	case string(validSignatureMsg):
		sig, err := c.signer.SignProofOfPossession(in.Message)
		if err != nil {
			return nil, err
		}
		return &signer.SignProofOfPossessionResponse{
			Signature: bls.SignatureToBytes(sig),
		}, nil
	case string(uncompressedSignatureMsg):
		sig, err := c.signer.SignProofOfPossession(in.Message)
		if err != nil {
			return nil, err
		}

		bytes := sig.Serialize()

		return &signer.SignProofOfPossessionResponse{
			Signature: bytes[:bls.SignatureLen],
		}, nil
	case string(emptySignatureMsg):
		return &signer.SignProofOfPossessionResponse{
			Signature: []byte{},
		}, nil
	case string(noSignatureMsg):
		return &signer.SignProofOfPossessionResponse{}, nil
	default:
		return nil, errors.New("invalid case")
	}
}

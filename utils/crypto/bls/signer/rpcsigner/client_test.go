// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package rpcsigner

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"

	"github.com/ava-labs/avalanchego/proto/pb/signer"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"github.com/ava-labs/avalanchego/utils/crypto/bls/signer/localsigner"
)

type testClient struct{}

var (
	localSigner, _ = localsigner.New()

	client = &Client{
		client: &testClient{},
		pk:     localSigner.PublicKey(),
	}

	validSignatureMsg        = []byte("valid")
	uncompressedSignatureMsg = []byte("uncompressed")
	emptySignatureMsg        = []byte("empty")
	noSignatureMsg           = []byte("none")
)

func TestValidSignature(t *testing.T) {
	sig, err := client.Sign(validSignatureMsg)
	require.NoError(t, err)
	bls.Verify(client.PublicKey(), sig, validSignatureMsg)
}

type test struct {
	name string
	msg  []byte
	err  error
}

var tests = []test{
	{
		name: "Uncompressed",
		msg:  uncompressedSignatureMsg,
		err:  bls.ErrFailedSignatureDecompress,
	},
	{
		name: "Empty",
		msg:  emptySignatureMsg,
		err:  ErrorEmptySignature,
	},
	{
		name: "None",
		msg:  noSignatureMsg,
		err:  ErrorEmptySignature,
	},
}

func TestInvalidSignature(t *testing.T) {
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			sig, err := client.Sign(test.msg)
			require.Nil(t, sig)
			require.EqualError(t, err, test.err.Error())
		})
	}
}

func TestValidPOPSignature(t *testing.T) {
	sig, err := client.SignProofOfPossession(validSignatureMsg)
	require.NoError(t, err)
	bls.VerifyProofOfPossession(client.PublicKey(), sig, validSignatureMsg)
}

func TestInvalidPOPSignature(t *testing.T) {
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			sig, err := client.SignProofOfPossession(test.msg)
			require.Nil(t, sig)
			require.EqualError(t, err, test.err.Error())
		})
	}
}

func (*testClient) PublicKey(_ context.Context, _ *signer.PublicKeyRequest, _ ...grpc.CallOption) (*signer.PublicKeyResponse, error) {
	return nil, nil
}

func (*testClient) Sign(_ context.Context, in *signer.SignRequest, _ ...grpc.CallOption) (*signer.SignResponse, error) {
	switch string(in.Message) {
	case string(validSignatureMsg):
		sig, err := localSigner.Sign(in.Message)
		if err != nil {
			return nil, err
		}

		return &signer.SignResponse{
			Signature: bls.SignatureToBytes(sig),
		}, nil
	case string(uncompressedSignatureMsg):
		sig, err := localSigner.Sign(in.Message)
		if err != nil {
			return nil, err
		}

		bytes := sig.Serialize()

		return &signer.SignResponse{
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

func (*testClient) SignProofOfPossession(_ context.Context, in *signer.SignProofOfPossessionRequest, _ ...grpc.CallOption) (*signer.SignProofOfPossessionResponse, error) {
	switch string(in.Message) {
	case string(validSignatureMsg):
		sig, err := localSigner.SignProofOfPossession(in.Message)
		if err != nil {
			return nil, err
		}
		return &signer.SignProofOfPossessionResponse{
			Signature: bls.SignatureToBytes(sig),
		}, nil
	case string(uncompressedSignatureMsg):
		sig, err := localSigner.SignProofOfPossession(in.Message)
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

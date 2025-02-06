// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package rpcsigner

import (
	"context"

	"github.com/ava-labs/avalanchego/proto/pb/signer"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"github.com/ava-labs/avalanchego/utils/crypto/bls/signer/localsigner"
)

type server struct {
	signer.UnimplementedSignerServer
	localSigner *localsigner.LocalSigner
}

func newServer() (*server, error) {
	localSigner, err := localsigner.New()
	if err != nil {
		return nil, err
	}

	return &server{
		localSigner: localSigner,
	}, nil
}

func (s *server) PublicKey(_ context.Context, _ *signer.PublicKeyRequest) (*signer.PublicKeyResponse, error) {
	publicKey := s.localSigner.PublicKey()

	publicKeyRes := &signer.PublicKeyResponse{
		PublicKey: bls.PublicKeyToCompressedBytes(publicKey),
	}

	return publicKeyRes, nil
}

func (s *server) Sign(_ context.Context, in *signer.SignatureRequest) (*signer.SignatureResponse, error) {
	signature, err := s.localSigner.Sign(in.Message)
	if err != nil {
		return nil, err
	}

	return &signer.SignatureResponse{
		Signature: bls.SignatureToBytes(signature),
	}, nil
}

func (s *server) SignProofOfPossession(_ context.Context, in *signer.ProofOfPossessionSignatureRequest) (*signer.ProofOfPossessionSignatureResponse, error) {
	signature, err := s.localSigner.SignProofOfPossession(in.Message)
	if err != nil {
		return nil, err
	}

	return &signer.ProofOfPossessionSignatureResponse{
		Signature: bls.SignatureToBytes(signature),
	}, nil
}

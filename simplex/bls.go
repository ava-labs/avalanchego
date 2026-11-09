// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package simplex

//go:generate go tool canoto $GOFILE

import (
	"errors"
	"fmt"

	"github.com/ava-labs/avalanchego/graft/simplex/common"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
)

var (
	errSignatureVerificationFailed = errors.New("signature verification failed")
	errSignerNotFound              = errors.New("signer not found in the membership set")
	errInvalidNodeID               = errors.New("unable to parse node ID")
	errFailedToParseSignature      = errors.New("failed to parse signature")
)

var (
	_ common.Signer            = (*BLSSigner)(nil)
	_ common.SignatureVerifier = (*BLSVerifier)(nil)
)

type SignFunc func(msg []byte) (*bls.Signature, error)

// BLSSigner signs messages encoded with the provided ChainID and NetworkID.
// using the SignBLS function.
type BLSSigner struct {
	chainID   ids.ID
	networkID uint32
	// signBLS is passed in because we support both software and hardware BLS signing.
	signBLS SignFunc
}

type BLSVerifier struct {
	networkID uint32
	chainID   ids.ID
}

func NewBLSAuth(config *Config) (BLSSigner, BLSVerifier, error) {
	verifier, err := createVerifier(config)
	if err != nil {
		return BLSSigner{}, BLSVerifier{}, err
	}

	return BLSSigner{
		chainID:   config.Ctx.ChainID,
		networkID: config.Ctx.NetworkID,
		signBLS:   config.SignBLS,
	}, verifier, nil
}

// Sign returns a signature on the given message using BLS signature scheme.
// It encodes the message to sign with the chain ID, and network ID,
func (s *BLSSigner) Sign(message []byte) (common.SignatureBytes, error) {
	message2Sign, err := encodeMessageToSign(message, s.chainID, s.networkID)
	if err != nil {
		return nil, fmt.Errorf("failed to encode message to sign: %w", err)
	}

	sig, err := s.signBLS(message2Sign)
	if err != nil {
		return nil, err
	}

	sigBytes := bls.SignatureToBytes(sig)
	return sigBytes, nil
}

type encodedSimplexSignedPayload struct {
	NetworkID uint32 `canoto:"uint,1"`
	ChainID   ids.ID `canoto:"fixed bytes,2"`
	Message   []byte `canoto:"bytes,3"`

	canotoData canotoData_encodedSimplexSignedPayload
}

func encodeMessageToSign(message []byte, chainID ids.ID, networkID uint32) ([]byte, error) {
	encodedSimplexMessage := encodedSimplexSignedPayload{
		Message:   message,
		ChainID:   chainID,
		NetworkID: networkID,
	}
	return encodedSimplexMessage.MarshalCanoto(), nil
}

func (v BLSVerifier) VerifySignature(message []byte, signature common.SignatureBytes, signer common.PublicKeyBytes) error {
	pk, err := bls.PublicKeyFromCompressedBytes(signer)
	if err != nil {
		return fmt.Errorf("%w: %w", errInvalidPK, err)
	}

	sig, err := bls.SignatureFromBytes(signature)
	if err != nil {
		return fmt.Errorf("%w: %w", errFailedToParseSignature, err)
	}

	message2Verify, err := encodeMessageToSign(message, v.chainID, v.networkID)
	if err != nil {
		return fmt.Errorf("failed to encode message to verify: %w", err)
	}

	if !bls.Verify(pk, sig, message2Verify) {
		return errSignatureVerificationFailed
	}

	return nil
}

func createVerifier(config *Config) (BLSVerifier, error) {
	verifier := BLSVerifier{
		networkID: config.Ctx.NetworkID,
		chainID:   config.Ctx.ChainID,
	}
	return verifier, nil
}

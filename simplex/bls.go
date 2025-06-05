// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package simplex

import (
	"encoding/asn1"
	"errors"
	"fmt"

	"github.com/ava-labs/simplex"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
)

var (
	errSignatureVerificationFailed = errors.New("signature verification failed")
	errSignerNotFound              = errors.New("signer not found in the membership set")
	simplexLabel                   = []byte("simplex")
)

var _ simplex.Signer = (*BLSSigner)(nil)

type SignFunc func(msg []byte) (*bls.Signature, error)

// BLSSigner signs messages encoded with the provided ChainID and SubnetID.
// using the SignBLS function.
type BLSSigner struct {
	chainID  ids.ID
	subnetID ids.ID
	// signBLS is passed in because we support both software and hardware BLS signing.
	signBLS SignFunc
}

type BLSVerifier struct {
	nodeID2PK map[ids.NodeID]bls.PublicKey
	subnetID  ids.ID
	chainID   ids.ID
}

func NewBLSAuth(config *Config) (BLSSigner, BLSVerifier) {
	return BLSSigner{
		chainID:  config.Ctx.ChainID,
		subnetID: config.Ctx.SubnetID,
		signBLS:  config.SignBLS,
	}, createVerifier(config)
}

// Sign returns a signature on the given message using BLS signature scheme.
// It encodes the message to sign with the chain ID, and subnet ID,
func (s *BLSSigner) Sign(message []byte) ([]byte, error) {
	message2Sign, err := encodeMessageToSign(message, s.chainID, s.subnetID)
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
	Message  []byte
	ChainID  []byte
	SubnetID []byte
	Label    []byte
}

// encodesMessageToSign returns a byte slice [simplexLabel][chainID][networkID][message length][message].
func encodeMessageToSign(message []byte, chainID ids.ID, subnetID ids.ID) ([]byte, error) {
	encodedSimplexMessage := encodedSimplexSignedPayload{
		Message:  message,
		ChainID:  chainID[:],
		SubnetID: subnetID[:],
		Label:    simplexLabel,
	}
	return asn1.Marshal(encodedSimplexMessage)
}

func (v BLSVerifier) Verify(message []byte, signature []byte, signer simplex.NodeID) error {
	if len(signer) != ids.NodeIDLen {
		return fmt.Errorf("expected signer to be %d bytes but got %d bytes", ids.NodeIDLen, len(signer))
	}

	key := ids.NodeID(signer)
	pk, exists := v.nodeID2PK[key]
	if !exists {
		return fmt.Errorf("%w: signer %x", errSignerNotFound, key)
	}

	sig, err := bls.SignatureFromBytes(signature)
	if err != nil {
		return fmt.Errorf("failed to parse signature: %w", err)
	}

	message2Verify, err := encodeMessageToSign(message, v.chainID, v.subnetID)
	if err != nil {
		return fmt.Errorf("failed to encode message to verify: %w", err)
	}

	if !bls.Verify(&pk, sig, message2Verify) {
		return errSignatureVerificationFailed
	}

	return nil
}

func createVerifier(config *Config) BLSVerifier {
	verifier := BLSVerifier{
		nodeID2PK: make(map[ids.NodeID]bls.PublicKey),
		subnetID:  config.Ctx.SubnetID,
		chainID:   config.Ctx.ChainID,
	}

	nodes := config.Validators.GetValidatorIDs(config.Ctx.SubnetID)
	for _, node := range nodes {
		validator, ok := config.Validators.GetValidator(config.Ctx.SubnetID, node)
		if !ok {
			continue
		}
		verifier.nodeID2PK[node] = *validator.PublicKey
	}
	return verifier
}

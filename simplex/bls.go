// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package simplex

import (
	"errors"
	"fmt"

	"github.com/ava-labs/simplex"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
)

var (
	errSignatureVerificationFailed = errors.New("signature verification failed")
	errSignerNotFound              = errors.New("signer not found in the membership set")
	errInvalidNodeID               = errors.New("unable to parse node ID")
	errFailedToParseSignature      = errors.New("failed to parse signature")
)

var _ simplex.Signer = (*BLSSigner)(nil)

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
	nodeID2PK map[ids.NodeID]*bls.PublicKey
	networkID uint32
	chainID   ids.ID

	canonicalNodeIDs       []ids.NodeID
	canonicalNodeIDIndices map[ids.NodeID]int
}

func NewBLSAuth(config *Config) (BLSSigner, BLSVerifier) {
	verifier := createVerifier(config)

	return BLSSigner{
		chainID:   config.Ctx.ChainID,
		networkID: config.Ctx.NetworkID,
		signBLS:   config.SignBLS,
	}, verifier
}

// Sign returns a signature on the given message using BLS signature scheme.
// It encodes the message to sign with the chain ID, and network ID,
func (s *BLSSigner) Sign(message []byte) ([]byte, error) {
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
	NetworkID uint32 `serialize:"true"`
	ChainID   ids.ID `serialize:"true"`
	Message   []byte `serialize:"true"`
}

func encodeMessageToSign(message []byte, chainID ids.ID, networkID uint32) ([]byte, error) {
	encodedSimplexMessage := encodedSimplexSignedPayload{
		Message:   message,
		ChainID:   chainID,
		NetworkID: networkID,
	}
	return Codec.Marshal(CodecVersion, &encodedSimplexMessage)
}

func (v BLSVerifier) Verify(message []byte, signature []byte, signer simplex.NodeID) error {
	key, err := ids.ToNodeID(signer)
	if err != nil {
		return fmt.Errorf("%w: %w", errInvalidNodeID, err)
	}

	pk, exists := v.nodeID2PK[key]
	if !exists {
		return fmt.Errorf("%w: signer %x", errSignerNotFound, key)
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

func createVerifier(config *Config) BLSVerifier {
	verifier := BLSVerifier{
		nodeID2PK: make(map[ids.NodeID]*bls.PublicKey),
		networkID: config.Ctx.NetworkID,
		chainID:   config.Ctx.ChainID,
	}

	nodeIDs := make([]ids.NodeID, 0, len(config.Validators))
	for _, node := range config.Validators {
		verifier.nodeID2PK[node.NodeID] = node.PublicKey
		nodeIDs = append(nodeIDs, node.NodeID)
	}

	utils.Sort(nodeIDs)
	verifier.canonicalNodeIDs = nodeIDs
	verifier.canonicalNodeIDIndices = make(map[ids.NodeID]int, len(nodeIDs))
	for i, nodeID := range nodeIDs {
		verifier.canonicalNodeIDIndices[nodeID] = i
	}

	return verifier
}

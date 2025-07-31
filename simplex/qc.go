// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package simplex

//go:generate go run github.com/StephenButtolph/canoto/canoto $GOFILE

import (
	"bytes"
	"errors"
	"fmt"

	"github.com/ava-labs/simplex"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"github.com/ava-labs/avalanchego/utils/set"
)

var (
	_ simplex.QuorumCertificate   = (*QC)(nil)
	_ simplex.QCDeserializer      = (*QCDeserializer)(nil)
	_ simplex.SignatureAggregator = (*SignatureAggregator)(nil)

	errFailedToParseQC       = errors.New("failed to parse quorum certificate")
	errUnexpectedSigners     = errors.New("unexpected number of signers in quorum certificate")
	errSignatureAggregation  = errors.New("signature aggregation failed")
	errEncodingMessageToSign = errors.New("failed to encode message to sign")
	errDuplicateSigner       = errors.New("duplicate signer in quorum certificate")
	errInvalidBitSet         = errors.New("bitset is invalid")
	errFailedToFilterSigners = errors.New("failed to filter signers")
)

// QC represents a quorum certificate in the Simplex consensus protocol.
type QC struct {
	verifier *BLSVerifier
	sig      *bls.Signature
	signers  []ids.NodeID
}

// CanotoQC is the Canoto representation of a quorum certificate
type canotoQC struct {
	Sig     [bls.SignatureLen]byte `canoto:"fixed bytes,1"`
	Signers []byte                 `canoto:"bytes,2"`

	canotoData canotoData_canotoQC
}

// Signers returns the list of signers for the quorum certificate.
func (qc *QC) Signers() []simplex.NodeID {
	signers := make([]simplex.NodeID, len(qc.signers))
	for i, signer := range qc.signers {
		signers[i] = signer[:]
	}

	return signers
}

// Verify checks if the quorum certificate is valid by verifying the aggregated signature against the signers' public keys.
func (qc *QC) Verify(msg []byte) error {
	quorum := simplex.Quorum(len(qc.verifier.nodeID2PK))
	if len(qc.signers) != quorum {
		return fmt.Errorf("%w: expected %d signers but got %d", errUnexpectedSigners, quorum, len(qc.signers))
	}

	uniqueSigners := set.NewSet[ids.NodeID](len(qc.signers))
	pks := make([]*bls.PublicKey, 0, len(qc.signers))

	// ensure signers are not double counted and are in the membership set
	for _, signer := range qc.signers {
		if uniqueSigners.Contains(signer) {
			return fmt.Errorf("%w: %x", errDuplicateSigner, signer)
		}

		pk, exists := qc.verifier.nodeID2PK[signer]
		if !exists {
			return fmt.Errorf("%w: %x", errSignerNotFound, signer)
		}

		uniqueSigners.Add(signer)
		pks = append(pks, pk)
	}

	// aggregate the public keys
	aggPK, err := bls.AggregatePublicKeys(pks)
	if err != nil {
		return fmt.Errorf("%w: %w", errSignatureAggregation, err)
	}

	message2Verify, err := encodeMessageToSign(msg, qc.verifier.chainID, qc.verifier.networkID)
	if err != nil {
		return fmt.Errorf("%w: %w", errEncodingMessageToSign, err)
	}

	if !bls.Verify(aggPK, qc.sig, message2Verify) {
		return errSignatureVerificationFailed
	}

	return nil
}

// Bytes serializes the quorum certificate into bytes.
func (qc *QC) Bytes() []byte {
	sigBytes := bls.SignatureToBytes(qc.sig)
	signers := qc.createSignersBitSet()

	canotoQC := &canotoQC{
		Sig:     [bls.SignatureLen]byte(sigBytes),
		Signers: signers,
	}

	return canotoQC.MarshalCanoto()
}

func (qc *QC) createSignersBitSet() []byte {
	bitset := set.NewBits()
	for _, signer := range qc.signers {
		// index should always exist, since we deserialized the signers from the same verifier
		index := qc.verifier.canonicalNodeIDIndices[signer]
		bitset.Add(index)
	}

	return bitset.Bytes()
}

type QCDeserializer struct {
	verifier *BLSVerifier
}

// DeserializeQuorumCertificate deserializes a quorum certificate from bytes.
func (d *QCDeserializer) DeserializeQuorumCertificate(bytes []byte) (simplex.QuorumCertificate, error) {
	var canotoQC canotoQC
	if err := canotoQC.UnmarshalCanoto(bytes); err != nil {
		return nil, fmt.Errorf("%w: %w", errFailedToParseQC, err)
	}

	sig, err := bls.SignatureFromBytes(canotoQC.Sig[:])
	if err != nil {
		return nil, fmt.Errorf("%w: %w", errFailedToParseSignature, err)
	}

	signers, err := d.signersFromBytes(canotoQC.Signers)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", errInvalidBitSet, err)
	}

	return &QC{
		sig:      sig,
		signers:  signers,
		verifier: d.verifier,
	}, nil
}

// SignatureAggregator aggregates signatures into a quorum certificate.
type SignatureAggregator struct {
	verifier *BLSVerifier
}

// Aggregate aggregates the provided signatures into a quorum certificate.
// It requires at least a quorum of signatures to succeed.
// If any signature is from a signer not in the membership set, it returns an error.
func (a *SignatureAggregator) Aggregate(signatures []simplex.Signature) (simplex.QuorumCertificate, error) {
	quorumSize := simplex.Quorum(len(a.verifier.nodeID2PK))
	if len(signatures) < quorumSize {
		return nil, fmt.Errorf("%w: expected %d signatures but got %d", errUnexpectedSigners, quorumSize, len(signatures))
	}
	signatures = signatures[:quorumSize]

	signers := make([]ids.NodeID, 0, quorumSize)
	sigs := make([]*bls.Signature, 0, quorumSize)
	for _, signature := range signatures {
		signer := signature.Signer
		id, err := ids.ToNodeID(signer)
		if err != nil {
			return nil, fmt.Errorf("%w: %w", errInvalidNodeID, err)
		}

		if _, exists := a.verifier.nodeID2PK[id]; !exists {
			return nil, fmt.Errorf("%w: %x", errSignerNotFound, signer)
		}
		sig, err := bls.SignatureFromBytes(signature.Value)
		if err != nil {
			return nil, fmt.Errorf("%w: %w", errFailedToParseSignature, err)
		}

		signers = append(signers, id)
		sigs = append(sigs, sig)
	}

	aggregatedSig, err := bls.AggregateSignatures(sigs)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", errSignatureAggregation, err)
	}

	return &QC{
		verifier: a.verifier,
		signers:  signers,
		sig:      aggregatedSig,
	}, nil
}

func (d *QCDeserializer) signersFromBytes(signerBytes []byte) ([]ids.NodeID, error) {
	signerIndices := set.BitsFromBytes(signerBytes)
	if !bytes.Equal(signerIndices.Bytes(), signerBytes) {
		return nil, errInvalidBitSet
	}

	signers, err := filterNodes(signerIndices, d.verifier.canonicalNodeIDs)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", errFailedToFilterSigners, err)
	}
	return signers, nil
}

// filterNodeIDS returns the nodeIDs in nodeIDs whose
// bit is set to 1 in indices.
//
// Returns an error if indices references an unknown node.
func filterNodes(
	indices set.Bits,
	nodeIDs []ids.NodeID,
) ([]ids.NodeID, error) {
	// Verify that all alleged signers exist
	if indices.BitLen() > len(nodeIDs) {
		return nil, fmt.Errorf(
			"%w: NumIndices (%d) >= NumFilteredValidators (%d)",
			errNodeNotFound,
			indices.BitLen()-1, // -1 to convert from length to index
			len(nodeIDs),
		)
	}

	filteredNodes := make([]ids.NodeID, 0, len(nodeIDs))
	for i, nodeID := range nodeIDs {
		if !indices.Contains(i) {
			continue
		}

		filteredNodes = append(filteredNodes, nodeID)
	}
	return filteredNodes, nil
}

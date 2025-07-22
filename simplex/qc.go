// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package simplex

//go:generate go run github.com/StephenButtolph/canoto/canoto $GOFILE

import (
	"errors"
	"fmt"

	"github.com/ava-labs/simplex"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
)

var (
	_ simplex.QuorumCertificate   = (*QC)(nil)
	_ simplex.QCDeserializer      = (*QCDeserializer)(nil)
	_ simplex.SignatureAggregator = (*SignatureAggregator)(nil)

	// QC errors
	errFailedToParseQC       = errors.New("failed to parse quorum certificate")
	errUnexpectedSigners     = errors.New("unexpected number of signers in quorum certificate")
	errSignatureAggregation  = errors.New("signature aggregation failed")
	errEncodingMessageToSign = errors.New("failed to encode message to sign")
	errDuplicateSigner       = errors.New("duplicate signer in quorum certificate")
)

// QC represents a quorum certificate in the Simplex consensus protocol.
type QC struct {
	verifier *BLSVerifier
	sig      *bls.Signature
	signers  []simplex.NodeID
}

// CanotoQC is the Canoto representation of a quorum certificate
type canotoQC struct {
	Sig     [bls.SignatureLen]byte `canoto:"fixed bytes,1"`
	Signers []simplex.NodeID       `canoto:"repeated bytes,2"`

	canotoData canotoData_canotoQC
}

// Signers returns the list of signers for the quorum certificate.
func (qc *QC) Signers() []simplex.NodeID {
	return qc.signers
}

// Verify checks if the quorum certificate is valid by verifying the aggregated signature against the signers' public keys.
func (qc *QC) Verify(msg []byte) error {
	pks := make([]*bls.PublicKey, 0, len(qc.signers))
	quorum := simplex.Quorum(len(qc.verifier.nodeID2PK))
	if len(qc.signers) != quorum {
		return fmt.Errorf("%w: expected %d signers but got %d", errUnexpectedSigners, quorum, len(qc.signers))
	}

	uniqueSigners := make(map[ids.NodeID]struct{}, len(qc.signers))

	// ensure all signers are in the membership set
	for _, signer := range qc.signers {
		id, err := ids.ToNodeID(signer)
		if err != nil {
			return fmt.Errorf("error converting signer to NodeID: %w", err)
		}

		if _, exists := uniqueSigners[id]; exists {
			return fmt.Errorf("%w: %x", errDuplicateSigner, signer)
		}
		uniqueSigners[id] = struct{}{}

		pk, exists := qc.verifier.nodeID2PK[id]
		if !exists {
			return fmt.Errorf("%w: %x", errSignerNotFound, signer)
		}

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
	canotoQC := &canotoQC{
		Sig:     [bls.SignatureLen]byte(sigBytes),
		Signers: qc.signers,
	}

	return canotoQC.MarshalCanoto()
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

	qc := QC{
		sig:     sig,
		signers: canotoQC.Signers,
	}

	qc.verifier = d.verifier

	return &qc, nil
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

	signers := make([]simplex.NodeID, 0, quorumSize)
	sigs := make([]*bls.Signature, 0, quorumSize)
	for _, signature := range signatures {
		signer := signature.Signer
		id, err := ids.ToNodeID(signer)
		if err != nil {
			return nil, fmt.Errorf("error converting signer to NodeID: %w", err)
		}

		if _, exists := a.verifier.nodeID2PK[id]; !exists {
			return nil, fmt.Errorf("%w: %x", errSignerNotFound, signer)
		}
		signers = append(signers, signer)
		sig, err := bls.SignatureFromBytes(signature.Value)
		if err != nil {
			return nil, fmt.Errorf("%w: %w", errFailedToParseSignature, err)
		}
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

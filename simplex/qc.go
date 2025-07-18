// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package simplex

import (
	"errors"
	"fmt"

	"github.com/ava-labs/simplex"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
)

var (
	_ simplex.QuorumCertificate   = (*QC)(nil)
	_ simplex.QCDeserializer      = QCDeserializer{}
	_ simplex.SignatureAggregator = (*SignatureAggregator)(nil)

	// QC errors
	errFailedToParseQC       = errors.New("failed to parse quorum certificate")
	errUnexpectedSigners     = errors.New("unexpected number of signers in quorum certificate")
	errSignatureAggregation  = errors.New("signature aggregation failed")
	errEncodingMessageToSign = errors.New("failed to encode message to sign")
)

// QC represents a quorum certificate in the Simplex consensus protocol.
type QC struct {
	verifier *BLSVerifier
	sig      *bls.Signature
	signers  []simplex.NodeID
}

type SerializedQC struct {
	Sig     []byte           `serialize:"true"`
	Signers []simplex.NodeID `serialize:"true"`
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

	// ensure all signers are in the membership set
	for _, signer := range qc.signers {
		pk, exists := qc.verifier.nodeID2PK[ids.NodeID(signer)]
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
	serializedQC := &SerializedQC{
		Sig:     bls.SignatureToBytes(qc.sig),
		Signers: qc.signers,
	}

	bytes, err := Codec.Marshal(CodecVersion, serializedQC)
	if err != nil {
		panic(fmt.Errorf("failed to marshal QC: %w", err))
	}
	return bytes
}

type QCDeserializer BLSVerifier

// DeserializeQuorumCertificate deserializes a quorum certificate from bytes.
func (d QCDeserializer) DeserializeQuorumCertificate(bytes []byte) (simplex.QuorumCertificate, error) {
	var serializedQC SerializedQC
	if _, err := Codec.Unmarshal(bytes, &serializedQC); err != nil {
		return nil, fmt.Errorf("%w: %w", errFailedToParseQC, err)
	}

	sig, err := bls.SignatureFromBytes(serializedQC.Sig)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", errFailedToParseSignature, err)
	}

	qc := QC{
		sig:     sig,
		signers: serializedQC.Signers,
	}

	verifier := BLSVerifier(d)
	qc.verifier = &verifier

	return &qc, nil
}

// SignatureAggregator aggregates signatures into a quorum certificate.
type SignatureAggregator BLSVerifier

// Aggregate aggregates the provided signatures into a quorum certificate.
// It requires at least a quorum of signatures to succeed.
// If any signature is from a signer not in the membership set, it returns an error.
func (a SignatureAggregator) Aggregate(signatures []simplex.Signature) (simplex.QuorumCertificate, error) {
	quorumSize := simplex.Quorum(len(a.nodeID2PK))
	if len(signatures) < quorumSize {
		return nil, fmt.Errorf("%w: expected %d signatures but got %d", errUnexpectedSigners, quorumSize, len(signatures))
	}
	signatures = signatures[:quorumSize]

	signers := make([]simplex.NodeID, 0, quorumSize)
	sigs := make([]*bls.Signature, 0, quorumSize)
	for _, signature := range signatures {
		signer := signature.Signer
		if _, exists := a.nodeID2PK[ids.NodeID(signer)]; !exists {
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

	verifier := BLSVerifier(a)
	return &QC{
		verifier: &verifier,
		signers:  signers,
		sig:      aggregatedSig,
	}, nil
}

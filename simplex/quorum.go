// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package simplex

import (
	"fmt"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"github.com/ava-labs/simplex"
)

var _ simplex.QuorumCertificate = (*QC)(nil)
var _ simplex.QCDeserializer = QCDeserializer{}

type QCDeserializer BLSVerifier

// QC represents a quorum certificate in the Simplex consensus protocol.
type QC struct {
	verifier BLSVerifier
	sig      bls.Signature
	signers  []simplex.NodeID
}

// Signers returns the list of signers for the quorum certificate.
func (qc *QC) Signers() []simplex.NodeID {
	return qc.signers
}

// Verify checks if the quorum certificate is valid by verifying the aggregated signature against the signers' public keys.
func (qc *QC) Verify(msg []byte) error {
	pks := make([]*bls.PublicKey, 0, len(qc.signers))

	// ensure all signers are in the membership set
	for _, signer := range qc.signers {
		pk, exists := qc.verifier.nodeID2PK[ids.NodeID(signer)]
		if !exists {
			return fmt.Errorf("signer %x is not found in the membership set", signer)
		}
		pks = append(pks, &pk)
	}

	// aggregate the public keys
	aggPK, err := bls.AggregatePublicKeys(pks)
	if err != nil {
		return fmt.Errorf("failed to aggregate public keys: %w", err)
	}

	message2Verify, err := encodeMessageToSign(msg, qc.verifier.chainID, qc.verifier.subnetID)
	if err != nil {
		return fmt.Errorf("failed to encode message to verify: %w", err)
	}

	if !bls.Verify(aggPK, &qc.sig, message2Verify) {
		return fmt.Errorf("signature verification failed")
	}

	return nil
}

// Bytes serializes the quorum certificate into bytes.
// The serialization format is:
// [signer1][signer2]...[signerN][signature]
// where each signer is represented by its NodeID and the signature is the BLS signature.
func (qc *QC) Bytes() []byte {
	sigBytes := bls.SignatureToBytes(&qc.sig)
	buff := make([]byte, len(sigBytes)+len(qc.signers)*ids.NodeIDLen)
	var pos int
	for _, signer := range qc.signers {
		copy(buff[pos:], signer[:ids.NodeIDLen])
		pos += ids.NodeIDLen
	}

	copy(buff[pos:], sigBytes)
	return buff
}

// DeserializeQuorumCertificate deserializes a quorum certificate from bytes.
func (d QCDeserializer) DeserializeQuorumCertificate(bytes []byte) (simplex.QuorumCertificate, error) {
	quorumSize := simplex.Quorum(len(d.nodeID2PK))
	expectedSize := quorumSize*ids.NodeIDLen + bls.SignatureLen
	if len(bytes) != expectedSize {
		return nil, fmt.Errorf("expected at least %d bytes but got %d bytes", expectedSize, len(bytes))
	}

	signers := make([]simplex.NodeID, 0, quorumSize)

	var pos int
	for range quorumSize {
		signers = append(signers, bytes[pos:pos+ids.NodeIDLen])
		pos += ids.NodeIDLen
	}

	sig, err := bls.SignatureFromBytes(bytes[pos:])
	if err != nil {
		return nil, fmt.Errorf("failed to parse signature: %w", err)
	}

	return &QC{
		verifier: BLSVerifier(d),
		signers:  signers,
		sig:      *sig,
	}, nil
}

// SignatureAggregator aggregates signatures into a quorum certificate.
type SignatureAggregator BLSVerifier

// Aggregate aggregates the provided signatures into a quorum certificate.
// It requires at least a quorum of signatures to succeed.
// If any signature is from a signer not in the membership set, it returns an error.
func (a SignatureAggregator) Aggregate(signatures []simplex.Signature) (simplex.QuorumCertificate, error) {
	quorumSize := simplex.Quorum(len(a.nodeID2PK))
	if len(signatures) < quorumSize {
		return nil, fmt.Errorf("expected at least %d signatures but got %d", quorumSize, len(signatures))
	}

	signatures = signatures[:quorumSize]

	signers := make([]simplex.NodeID, 0, quorumSize)
	sigs := make([]*bls.Signature, 0, quorumSize)
	for _, signature := range signatures {
		signer := signature.Signer
		_, exists := a.nodeID2PK[ids.NodeID(signer)]
		if !exists {
			return nil, fmt.Errorf("signer %x is not found in the membership set", signer)
		}
		signers = append(signers, signer)
		sig, err := bls.SignatureFromBytes(signature.Value)
		if err != nil {
			return nil, fmt.Errorf("failed to parse signature: %w", err)
		}
		sigs = append(sigs, sig)
	}

	aggregatedSig, err := bls.AggregateSignatures(sigs)
	if err != nil {
		return nil, fmt.Errorf("failed to aggregate signatures: %w", err)
	}

	return &QC{
		verifier: BLSVerifier(a),
		signers:  signers,
		sig:      *aggregatedSig,
	}, nil
}

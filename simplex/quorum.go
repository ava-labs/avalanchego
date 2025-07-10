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
	if len(qc.signers) != simplex.Quorum(len(qc.verifier.nodeID2PK)) {
		return fmt.Errorf("%w: expected %d signers but got %d", errUnexpectedSigners, simplex.Quorum(len(qc.verifier.nodeID2PK)), len(qc.signers))
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

	if !bls.Verify(aggPK, &qc.sig, message2Verify) {
		return errSignatureVerificationFailed
	}

	return nil
}

// asn1QC is the ASN.1 structure for the quorum certificate.
// It contains the signers' public keys and the aggregated signature.
// The signers are represented as byte slices of their IDs.
type asn1QC struct {
	Signers   [][]byte
	Signature []byte
}

func (qc *QC) MarshalASN1() ([]byte, error) {
	sigBytes := bls.SignatureToBytes(&qc.sig)

	signersBytes := make([][]byte, len(qc.signers))
	for i, signer := range qc.signers {
		s := signer // avoid aliasing
		signersBytes[i] = s[:]
	}
	asn1Data := asn1QC{
		Signers:   signersBytes,
		Signature: sigBytes,
	}
	return asn1.Marshal(asn1Data)
}

func (qc *QC) UnmarshalASN1(data []byte) error {
	var decoded asn1QC
	_, err := asn1.Unmarshal(data, &decoded)
	if err != nil {
		return err
	}
	qc.signers = make([]simplex.NodeID, len(decoded.Signers))
	for i, signerBytes := range decoded.Signers {
		if len(signerBytes) != ids.ShortIDLen { // TODO: so long as simplex is in a separate repo, we should decouple these ids as much as possible
			return errors.New("invalid signer length")
		}
		qc.signers[i] = simplex.NodeID(signerBytes)
	}
	sig, err := bls.SignatureFromBytes(decoded.Signature)
	if err != nil {
		return err
	}
	qc.sig = *sig

	return nil
}

// Bytes serializes the quorum certificate into bytes.
func (qc *QC) Bytes() []byte {
	bytes, err := qc.MarshalASN1()
	if err != nil {
		panic(fmt.Errorf("failed to marshal QC: %w", err))
	}
	return bytes
}

type QCDeserializer BLSVerifier

// DeserializeQuorumCertificate deserializes a quorum certificate from bytes.
func (d QCDeserializer) DeserializeQuorumCertificate(bytes []byte) (simplex.QuorumCertificate, error) {
	var qc QC
	if err := qc.UnmarshalASN1(bytes); err != nil {
		return nil, fmt.Errorf("%w: %w", errFailedToParseQC, err)
	}
	qc.verifier = BLSVerifier(d)

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
		return nil, fmt.Errorf("%w: wanted %d signatures but got %d", errUnexpectedSigners, quorumSize, len(signatures))
	}

	signatures = signatures[:quorumSize]

	signers := make([]simplex.NodeID, 0, quorumSize)
	sigs := make([]*bls.Signature, 0, quorumSize)
	for _, signature := range signatures {
		signer := signature.Signer
		_, exists := a.nodeID2PK[ids.NodeID(signer)]
		if !exists {
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
		verifier: BLSVerifier(a),
		signers:  signers,
		sig:      *aggregatedSig,
	}, nil
}

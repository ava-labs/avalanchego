package simplex

import (
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"simplex"
)

var (
	simplexLabel = []byte("<simplex>")
)

type BLSSigner struct {
	ChainID   ids.ID
	NetworkID ids.ID
	SignBLS   func(msg []byte) (*bls.Signature, error)
	NodeID    ids.NodeID
}

func (s *BLSSigner) Sign(message []byte) ([]byte, error) {
	message2Sign := encodeMessageToSign(message, s.ChainID, s.NetworkID)
	sig, err := s.SignBLS(message2Sign)
	if err != nil {
		return nil, err
	}

	sigBytes := bls.SignatureToBytes(sig)

	buff := make([]byte, ids.NodeIDLen+len(sigBytes))
	copy(buff, s.NodeID[:])
	copy(buff[len(s.NodeID):], sigBytes)

	return sigBytes, nil
}

func encodeMessageToSign(message []byte, chainID ids.ID, networkID ids.ID) []byte {
	message2Sign := make([]byte, len(message)+len(simplexLabel)+ids.IDLen+ids.IDLen+4)
	msgLenBuff := make([]byte, 4)
	binary.BigEndian.PutUint32(msgLenBuff, uint32(len(message)))
	copy(message2Sign, simplexLabel)
	copy(message2Sign[len(simplexLabel):], chainID[:])
	copy(message2Sign[len(simplexLabel)+ids.IDLen:], networkID[:])
	copy(message2Sign[len(simplexLabel)+ids.IDLen+ids.IDLen:], msgLenBuff)
	copy(message2Sign[len(simplexLabel)+ids.IDLen+ids.IDLen+4:], message)
	return message2Sign
}

type NodePK struct {
	NodeID    ids.NodeID
	PublicKey *bls.PublicKey
}

type BLSVerifier struct {
	nodeID2PK map[ids.NodeID]bls.PublicKey
	networkID ids.ID
	chainID   ids.ID
}

func (v BLSVerifier) Verify(message []byte, signature []byte, signer simplex.NodeID) error {
	if len(signer) != ids.NodeIDLen {
		return fmt.Errorf("expected signer to be %d bytes but got %d bytes", ids.NodeIDLen, len(signer))
	}

	key := ids.NodeID(signer)
	pk, exists := v.nodeID2PK[key]
	if !exists {
		return fmt.Errorf("signer %x is not found in the membership set", signer)
	}

	sig, err := bls.SignatureFromBytes(signature)
	if err != nil {
		return fmt.Errorf("failed to parse signature: %w", err)
	}

	message2Verify := encodeMessageToSign(message, v.chainID, v.networkID)

	if !bls.Verify(&pk, sig, message2Verify) {
		return fmt.Errorf("signature verification failed")
	}

	return nil
}

type QC struct {
	v       BLSVerifier
	sig     bls.Signature
	signers []simplex.NodeID
}

func (qc *QC) Signers() []simplex.NodeID {
	return qc.signers
}

func (qc *QC) Verify(msg []byte) error {
	pks := make([]*bls.PublicKey, 0, len(qc.signers))
	for _, signer := range qc.signers {
		pk, exists := qc.v.nodeID2PK[ids.NodeID(signer)]
		if !exists {
			return fmt.Errorf("signer %x is not found in the membership set", signer)
		}
		pks = append(pks, &pk)
	}

	aggPK, err := bls.AggregatePublicKeys(pks)
	if err != nil {
		return fmt.Errorf("failed to aggregate public keys: %w", err)
	}

	message2Verify := encodeMessageToSign(msg, qc.v.chainID, qc.v.networkID)

	if !bls.Verify(aggPK, &qc.sig, message2Verify) {
		return fmt.Errorf("signature verification failed")
	}

	return nil
}

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

type QCDeserializer BLSVerifier

func (d QCDeserializer) DeserializeQuorumCertificate(bytes []byte) (simplex.QuorumCertificate, error) {
	quorumSize := simplex.Quorum(len(d.nodeID2PK))
	expectedMinimalSize := quorumSize * ids.NodeIDLen
	if len(bytes) < expectedMinimalSize {
		return nil, fmt.Errorf("expected at least %d bytes but got %d bytes", expectedMinimalSize, len(bytes))
	}

	signers := make([]simplex.NodeID, 0, quorumSize)

	var pos int
	for range quorumSize {
		signers = append(signers, bytes[pos:pos+ids.NodeIDLen])
		pos += ids.NodeIDLen
	}

	sig, err := bls.SignatureFromBytes(bytes[pos:])
	if err != nil {
		fmt.Println(">>>", base64.StdEncoding.EncodeToString(bytes))
		return nil, fmt.Errorf("failed to parse signature: %w", err)
	}

	return &QC{
		v:       BLSVerifier(d),
		signers: signers,
		sig:     *sig,
	}, nil
}

type SignatureAggregator BLSVerifier

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
		v:       BLSVerifier(a),
		signers: signers,
		sig:     *aggregatedSig,
	}, nil
}

// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package simplex

//go:generate go tool canoto $GOFILE

import (
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"

	"github.com/ava-labs/avalanchego/graft/simplex/common"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"github.com/ava-labs/avalanchego/utils/set"
)

var (
	_ common.QuorumCertificate   = (*QC)(nil)
	_ common.QCDeserializer      = (*QCDeserializer)(nil)
	_ common.SignatureAggregator = (*SignatureAggregator)(nil)

	errFailedToParseQC       = errors.New("failed to parse quorum certificate")
	errUnexpectedSigners     = errors.New("unexpected number/weight of signers in quorum certificate")
	errSignatureAggregation  = errors.New("signature aggregation failed")
	errInvalidPK             = errors.New("failed parsing public key")
	errEncodingMessageToSign = errors.New("failed to encode message to sign")
	errDuplicateSigner       = errors.New("duplicate signer in quorum certificate")
	errInvalidBitSet         = errors.New("bitset is invalid")
	errFailedToFilterSigners = errors.New("failed to filter signers")
)

// QC represents a quorum certificate in the Simplex consensus protocol.
type QC struct {
	chainID   ids.ID
	networkID uint32

	sig        *bls.Signature
	signerMask set.Bits
	signers    common.NodeIDs

	serializedQC []byte
}

// CanotoQC is the Canoto representation of a quorum certificate
type canotoQC struct {
	Sig     [bls.SignatureLen]byte `canoto:"fixed bytes,1"`
	Signers []byte                 `canoto:"bytes,2"`

	canotoData canotoData_canotoQC
}

func newQC(
	networkID uint32,
	chainID ids.ID,
	signers common.NodeIDs,
	signerMask set.Bits,
	sig *bls.Signature,
) *QC {
	qc := &QC{
		networkID:  networkID,
		chainID:    chainID,
		signers:    signers,
		signerMask: signerMask,
		sig:        sig,
	}

	serializedQC := canotoQC{
		Sig:     [bls.SignatureLen]byte(sig.Compress()),
		Signers: signerMask.Bytes(),
	}

	qc.serializedQC = serializedQC.MarshalCanoto()

	return qc
}

// Signers returns the list of signers for the quorum certificate.
func (qc *QC) Signers() []common.NodeID {
	return qc.signers
}

// Bytes serializes the quorum certificate into bytes.
func (qc *QC) Bytes() []byte {
	return qc.serializedQC
}

// Size returns the size of the serialized quorum certificate in bytes.
func (qc *QC) Size() int {
	return len(qc.serializedQC)
}

// Verify checks if the quorum certificate is valid by verifying the aggregated signature against the signers' public keys.
func (qc *QC) Verify(msg []byte, nodes common.Nodes) error {
	signers, err := nodesFromBitmask(qc.signerMask, nodes.NodeIDs())
	if err != nil {
		return fmt.Errorf("%w: %w", errInvalidBitSet, err)
	}

	totalWeight := sumWeights(nodes)
	quorumWeight := common.Quorum(totalWeight)

	nodesByNodeID := nodesByNodeID(nodes)

	// Ensure that the signers are in the membership set and calculate their total weight.
	weightOfSigners, err := computeWeightOfSigners(signers, nodesByNodeID)
	if err != nil {
		return err
	}

	stakeOrNodes := stakeOrNodes(nodes, totalWeight)

	if weightOfSigners < quorumWeight {
		return fmt.Errorf("%w: expected signatures from %d %s but got %d out of %d", errUnexpectedSigners, quorumWeight, stakeOrNodes, weightOfSigners, totalWeight)
	}

	pks := make([]*bls.PublicKey, 0, len(signers))

	for _, signer := range signers {
		node, exists := nodesByNodeID[signer]
		if !exists {
			// This should never happen because we already checked that the signers are in the membership set above,
			// but we anyway need to lookup the node to get its public key,
			// so there is no harm in making sure the node is not the zero value.
			return fmt.Errorf("%w: %x", errSignerNotFound, signer)
		}

		pkBytes := node.PK
		pk, err := bls.PublicKeyFromCompressedBytes(pkBytes)
		if err != nil {
			return fmt.Errorf("%w: %w (%s)", errInvalidPK, err, base64.StdEncoding.EncodeToString(pkBytes))
		}

		pks = append(pks, pk)
	}

	aggPK, err := bls.AggregatePublicKeys(pks)
	if err != nil {
		return fmt.Errorf("%w: %w", errSignatureAggregation, err)
	}

	message2Verify, err := encodeMessageToSign(msg, qc.chainID, qc.networkID)
	if err != nil {
		return fmt.Errorf("%w: %w", errEncodingMessageToSign, err)
	}

	if !bls.Verify(aggPK, qc.sig, message2Verify) {
		return errSignatureVerificationFailed
	}

	return nil
}

func computeWeightOfSigners(signers common.NodeIDs, nodesByNodeID map[common.NodeID]common.Node) (uint64, error) {
	var weightOfSigners uint64
	for _, signer := range signers {
		node, exists := nodesByNodeID[signer]
		if !exists {
			return 0, fmt.Errorf("%w: %x", errSignerNotFound, signer)
		}
		weightOfSigners += node.Weight
	}
	return weightOfSigners, nil
}

func stakeOrNodes(nodes common.Nodes, totalWeight uint64) string {
	stakeOrNodes := "nodes"

	if uint64(len(nodes)) < totalWeight {
		stakeOrNodes = "stake"
	}
	return stakeOrNodes
}

func nodesByNodeID(nodes common.Nodes) map[common.NodeID]common.Node {
	nodesByNodeID := make(map[common.NodeID]common.Node, len(nodes))
	for _, node := range nodes {
		nodesByNodeID[node.Id] = node
	}
	return nodesByNodeID
}

type QCDeserializer struct {
	networkID uint32
	chainID   ids.ID
	// nodes is the sorted validator set that deserialize decodes signer bitmasks against.
	// It is used by the paths that have no epoch of their own (p2p inbound and storage).
	nodes common.NodeIDs
}

// deserialize deserializes a quorum certificate against the deserializer's own validator set.
func (d *QCDeserializer) deserialize(qcBytes []byte) (common.QuorumCertificate, error) {
	return d.DeserializeQuorumCertificate(qcBytes, d.nodes)
}

// DeserializeQuorumCertificate deserializes a quorum certificate from bytes.
func (d *QCDeserializer) DeserializeQuorumCertificate(qcBytes []byte, nodes common.NodeIDs) (common.QuorumCertificate, error) {
	var canotoQC canotoQC
	if err := canotoQC.UnmarshalCanoto(qcBytes); err != nil {
		return nil, fmt.Errorf("%w: %w", errFailedToParseQC, err)
	}

	sig, err := bls.SignatureFromBytes(canotoQC.Sig[:])
	if err != nil {
		return nil, fmt.Errorf("%w: %w", errFailedToParseSignature, err)
	}

	signerIndices := set.BitsFromBytes(canotoQC.Signers)

	// Check the encoding of the bitmask is canonical.
	if !bytes.Equal(signerIndices.Bytes(), canotoQC.Signers) {
		return nil, errInvalidBitSet
	}

	signers, err := nodesFromBitmask(signerIndices, nodes)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", errFailedToFilterSigners, err)
	}

	return newQC(d.networkID, d.chainID, signers, signerIndices, sig), nil
}

// SignatureAggregator aggregates signatures into a quorum certificate.
type SignatureAggregator struct {
	networkID        uint32
	chainID          ids.ID
	nodes            []common.Node
	nodeIDsToWeights map[common.NodeID]uint64
	nodeIDToIndex    map[common.NodeID]int
	quorumWeight     uint64
	totalWeight      uint64
}

func newSignatureAggregator(networkID uint32, chainID ids.ID, nodes []common.Node) *SignatureAggregator {
	totalWeight := sumWeights(nodes)
	quorumWeight := common.Quorum(totalWeight)
	return &SignatureAggregator{
		networkID:        networkID,
		chainID:          chainID,
		nodes:            nodes,
		quorumWeight:     quorumWeight,
		totalWeight:      totalWeight,
		nodeIDsToWeights: nodeIDsToWeights(nodes),
		nodeIDToIndex:    nodeIDsToIndex(nodes),
	}
}

// Aggregate aggregates the provided signatures into a quorum certificate.
// It requires signatures from at least a quorum of validator nodes to succeed.
// If any signature is from a signer not in the membership set, it returns an error.
func (a *SignatureAggregator) Aggregate(signatures []common.Signature) (common.QuorumCertificate, error) {
	// Drop duplicate signatures from the same signer, so no signer is counted twice.
	signatures = deduplicateSignatures(signatures)
	signers := signersFromSignatures(signatures)

	stakeOrNodes := stakeOrNodes(a.nodes, a.quorumWeight)

	// Ensure that the signatures are from a quorum of signers.
	weightOfNodesThatSigned := a.weightOfNodes(signers)
	if weightOfNodesThatSigned < a.quorumWeight {
		return nil, fmt.Errorf("%w: expected signatures from %d %s but got %d out of %d", errUnexpectedSigners, a.quorumWeight, stakeOrNodes, weightOfNodesThatSigned, a.totalWeight)
	}

	sigs := make([]*bls.Signature, 0, len(signatures))
	signerMask := set.NewBits()

	for _, signature := range signatures {
		signer := signature.Signer

		// Ensure that every node that has signed is in the membership set.
		index, exists := a.nodeIDToIndex[signer]
		if !exists {
			return nil, fmt.Errorf("%w: %x", errSignerNotFound, signer)
		}

		signerMask.Add(index)

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

	return newQC(a.networkID, a.chainID, signers, signerMask, aggregatedSig), nil
}

func (a *SignatureAggregator) AboveByzantineThreshold(nodes []common.NodeID) bool {
	return 2*a.weightOfNodes(deduplicateNodeIDs(nodes)) > a.totalWeight
}

// AppendSignatures appends signatures to an existing signature.
// If the existing signature is empty, it just aggregates the given signatures.
func (*SignatureAggregator) AppendSignatures(existingSignature common.SignatureBytes, additionalSignatures ...common.SignatureBytes) (common.SignatureBytes, error) {
	totalSignatureCount := len(additionalSignatures)
	if len(existingSignature) > 0 {
		totalSignatureCount++
	}
	sigs := make([]*bls.Signature, 0, totalSignatureCount)

	if len(existingSignature) > 0 {
		sig, err := bls.SignatureFromBytes(existingSignature)
		if err != nil {
			return nil, fmt.Errorf("%w: failed parsing existing signature %w (%s)",
				errFailedToParseSignature, err, base64.StdEncoding.EncodeToString(existingSignature))
		}
		sigs = append(sigs, sig)
	}

	for i, sigBytes := range additionalSignatures {
		sig, err := bls.SignatureFromBytes(sigBytes)
		if err != nil {
			return nil, fmt.Errorf("%w: failed parsing additional signature %d %w (%s)",
				errFailedToParseSignature, i, err, base64.StdEncoding.EncodeToString(sigBytes))
		}
		sigs = append(sigs, sig)
	}

	aggregatedSig, err := bls.AggregateSignatures(sigs)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", errSignatureAggregation, err)
	}

	return bls.SignatureToBytes(aggregatedSig), nil
}

// IsQuorum checks if the provided nodes are a quorum of the membership set.
func (a *SignatureAggregator) IsQuorum(nodes []common.NodeID) bool {
	// First thing, de-duplicate the nodes, since we don't want to double count any signers.
	uniqueNodes := deduplicateNodeIDs(nodes)
	weightOfSignerParties := a.weightOfNodes(uniqueNodes)
	return weightOfSignerParties >= a.quorumWeight
}

func deduplicateNodeIDs(nodes []common.NodeID) []common.NodeID {
	res := make([]common.NodeID, 0, len(nodes))
	uniqueNodes := set.NewSet[common.NodeID](len(nodes))
	for _, node := range nodes {
		if uniqueNodes.Contains(node) {
			continue
		}
		uniqueNodes.Add(node)
		res = append(res, node)
	}
	return res
}

func (a *SignatureAggregator) weightOfNodes(nodes []common.NodeID) uint64 {
	var weight uint64
	for _, signer := range nodes {
		weight += a.nodeIDsToWeights[signer]
	}
	return weight
}

// nodesFromBitmask returns the nodeIDs in nodeIDs whose
// bit is set to 1 in indices.
//
// Returns an error if indices references an unknown node.
func nodesFromBitmask(
	bitmask set.Bits,
	nodeIDs common.NodeIDs,
) (common.NodeIDs, error) {
	// Verify that all alleged signers exist
	if bitmask.BitLen() > len(nodeIDs) {
		return nil, fmt.Errorf(
			"%w: highest signer index (%d) >= number of validators (%d)",
			errNodeNotFound,
			bitmask.BitLen()-1, // -1 to convert from length to index
			len(nodeIDs),
		)
	}

	result := make(common.NodeIDs, 0, len(nodeIDs))
	for i, nodeID := range nodeIDs {
		if !bitmask.Contains(i) {
			continue
		}

		result = append(result, nodeID)
	}
	return result, nil
}

func sumWeights(nodes []common.Node) uint64 {
	var sum uint64
	for _, node := range nodes {
		sum += node.Weight
	}
	return sum
}

func nodeIDsToWeights(nodes []common.Node) map[common.NodeID]uint64 {
	var res = make(map[common.NodeID]uint64, len(nodes))
	for _, node := range nodes {
		res[node.Id] = node.Weight
	}
	return res
}

func nodeIDsToIndex(nodes []common.Node) map[common.NodeID]int {
	var res = make(map[common.NodeID]int, len(nodes))
	for i, node := range nodes {
		res[node.Id] = i
	}
	return res
}

// deduplicateSignatures returns the signatures with at most one signature per signer,
// keeping the first signature seen for each signer and preserving order.
func deduplicateSignatures(signatures []common.Signature) []common.Signature {
	var res []common.Signature
	uniqueSigners := set.NewSet[common.NodeID](len(signatures))
	for _, signature := range signatures {
		signer := signature.Signer
		if uniqueSigners.Contains(signer) {
			continue
		}
		uniqueSigners.Add(signer)
		res = append(res, signature)
	}
	return res
}

func signersFromSignatures(signatures []common.Signature) []common.NodeID {
	res := make([]common.NodeID, 0, len(signatures))
	for _, signature := range signatures {
		res = append(res, signature.Signer)
	}
	return res
}

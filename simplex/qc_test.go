// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package simplex

import (
	"testing"

	"github.com/ava-labs/simplex"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
)

// TestQCAggregateAndSign tests the aggregation of multiple signatures
// and then verifies the generated quorum certificate on that message.
func TestQCAggregateAndSign(t *testing.T) {
	configs := newNetworkConfigs(t, 2)
	quorum := simplex.Quorum(len(configs))

	msg := []byte("Begin at the beginning, and go on till you come to the end: then stop")

	signatures := make([]simplex.Signature, 0, quorum)
	nodes := make([]simplex.NodeID, 0, quorum)
	var signer BLSSigner
	var verifier BLSVerifier

	for _, config := range configs {
		signer, verifier = NewBLSAuth(config)

		sig, err := signer.Sign(msg)
		require.NoError(t, err)
		require.NoError(t, verifier.Verify(msg, sig, config.Ctx.NodeID[:]))

		signatures = append(signatures, simplex.Signature{
			Signer: config.Ctx.NodeID[:],
			Value:  sig,
		})
		nodes = append(nodes, config.Ctx.NodeID[:])
	}

	// aggregate the signatures into a quorum certificate
	signatureAggregator := SignatureAggregator(verifier)
	qc, err := signatureAggregator.Aggregate(signatures)

	require.NoError(t, err)
	require.Equal(t, nodes, qc.Signers())
	// verify the quorum certificate
	require.NoError(t, qc.Verify(msg))

	d := QCDeserializer(verifier)
	// try to deserialize the quorum certificate
	deserializedQC, err := d.DeserializeQuorumCertificate(qc.Bytes())
	require.NoError(t, err)

	require.Equal(t, qc.Signers(), deserializedQC.Signers())
	require.Equal(t, qc.Bytes(), deserializedQC.Bytes())
	require.NoError(t, deserializedQC.Verify(msg))
}

func TestQCSignerNotInMembershipSet(t *testing.T) {
	node1 := newEngineConfig(t, 2)
	signer, verifier := NewBLSAuth(node1)

	// nodes 1 and 2 will sign the same message
	msg := []byte("Begin at the beginning, and go on till you come to the end: then stop")
	sig, err := signer.Sign(msg)
	require.NoError(t, err)
	require.NoError(t, verifier.Verify(msg, sig, node1.Ctx.NodeID[:]))

	// add a new validator, but it won't be in the membership set of the first node signer/verifier
	node2 := newEngineConfig(t, 2)
	node2.Ctx.ChainID = node1.Ctx.ChainID

	// sign the same message with the new node
	signer2, verifier2 := NewBLSAuth(node2)
	sig2, err := signer2.Sign(msg)
	require.NoError(t, err)
	require.NoError(t, verifier2.Verify(msg, sig2, node2.Ctx.NodeID[:]))

	// aggregate the signatures into a quorum certificate
	signatureAggregator := SignatureAggregator(verifier)
	_, err = signatureAggregator.Aggregate(
		[]simplex.Signature{
			{Signer: node1.Ctx.NodeID[:], Value: sig},
			{Signer: node2.Ctx.NodeID[:], Value: sig2},
		},
	)
	require.ErrorIs(t, err, errSignerNotFound)
}

func TestQCDeserializerInvalidInput(t *testing.T) {
	config := newEngineConfig(t, 2)

	_, verifier := NewBLSAuth(config)
	deserializer := QCDeserializer(verifier)

	tests := []struct {
		name  string
		input []byte
		err   error
	}{
		{
			name:  "too short input",
			input: make([]byte, 10),
			err:   errFailedToParseQC,
		},
		{
			name:  "invalid signature bytes",
			input: make([]byte, simplex.Quorum(len(verifier.nodeID2PK))*ids.NodeIDLen+bls.SignatureLen),
			err:   errFailedToParseQC,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := deserializer.DeserializeQuorumCertificate(tt.input)
			require.ErrorIs(t, err, tt.err)
		})
	}
}

func TestSignatureAggregatorInsufficientSignatures(t *testing.T) {
	config := newEngineConfig(t, 3)

	signer, verifier := NewBLSAuth(config)
	msg := []byte("test message")
	sig, err := signer.Sign(msg)
	require.NoError(t, err)

	// try to aggregate with only 1 signature when quorum is 2
	signatureAggregator := SignatureAggregator(verifier)
	_, err = signatureAggregator.Aggregate(
		[]simplex.Signature{
			{Signer: config.Ctx.NodeID[:], Value: sig},
		},
	)
	require.ErrorIs(t, err, errUnexpectedSigners)
}

func TestSignatureAggregatorInvalidSignatureBytes(t *testing.T) {
	config := newEngineConfig(t, 2)

	signer, verifier := NewBLSAuth(config)
	msg := []byte("test message")
	sig, err := signer.Sign(msg)
	require.NoError(t, err)

	signatureAggregator := SignatureAggregator(verifier)
	_, err = signatureAggregator.Aggregate(
		[]simplex.Signature{
			{Signer: config.Ctx.NodeID[:], Value: sig},
			{Signer: config.Ctx.NodeID[:], Value: []byte("invalid signature")},
		},
	)
	require.ErrorIs(t, err, errFailedToParseSignature)
}

func TestSignatureAggregatorExcessSignatures(t *testing.T) {
	configs := newNetworkConfigs(t, 4)

	msg := []byte("test message")

	var nodeSigner BLSSigner
	var verifier BLSVerifier

	// Create signatures from all 4 nodes
	signatures := make([]simplex.Signature, 4)
	for i, config := range configs {
		nodeSigner, verifier = NewBLSAuth(config)
		sig, err := nodeSigner.Sign(msg)
		require.NoError(t, err)

		signatures[i] = simplex.Signature{Signer: config.Ctx.NodeID[:], Value: sig}
	}

	// Aggregate should only use the first 3 signatures
	signatureAggregator := SignatureAggregator(verifier)
	qc, err := signatureAggregator.Aggregate(signatures)
	require.NoError(t, err)

	// Should only have 3 signers, not 4
	require.Len(t, qc.Signers(), simplex.Quorum(len(configs)))
	require.NoError(t, qc.Verify(msg))
}

func TestQCVerifyWithWrongMessage(t *testing.T) {
	configs := newNetworkConfigs(t, 2)

	node1 := configs[0]
	node2 := configs[1]
	signer, verifier := NewBLSAuth(node1)
	originalMsg := []byte("original message")
	wrongMsg := []byte("wrong message")

	// Create signatures for original message
	sig1, err := signer.Sign(originalMsg)
	require.NoError(t, err)

	signer2, _ := NewBLSAuth(node2)
	sig2, err := signer2.Sign(originalMsg)
	require.NoError(t, err)

	signatureAggregator := SignatureAggregator(verifier)
	qc, err := signatureAggregator.Aggregate(
		[]simplex.Signature{
			{Signer: node1.Ctx.NodeID[:], Value: sig1},
			{Signer: node2.Ctx.NodeID[:], Value: sig2},
		},
	)
	require.NoError(t, err)

	// Verify with original message should succeed
	require.NoError(t, qc.Verify(originalMsg))

	// Verify with wrong message should fail
	err = qc.Verify(wrongMsg)
	require.ErrorIs(t, err, errSignatureVerificationFailed)
}

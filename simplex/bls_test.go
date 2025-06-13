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

func TestBLSSignVerify(t *testing.T) {
	config := newEngineConfig(t, 1)

	signer, verifier := NewBLSAuth(&config.Config)

	msg := "Begin at the beginning, and go on till you come to the end: then stop"

	sig, err := signer.Sign([]byte(msg))
	require.NoError(t, err)

	err = verifier.Verify([]byte(msg), sig, config.Ctx.NodeID[:])
	require.NoError(t, err)
}

func TestSignerNotInMemberSet(t *testing.T) {
	config := newEngineConfig(t, 1)
	signer, verifier := NewBLSAuth(&config.Config)

	msg := "Begin at the beginning, and go on till you come to the end: then stop"

	sig, err := signer.Sign([]byte(msg))
	require.NoError(t, err)

	notInMembershipSet := ids.GenerateTestNodeID()
	err = verifier.Verify([]byte(msg), sig, notInMembershipSet[:])
	require.ErrorIs(t, err, errSignerNotFound)
}

func TestSignerInvalidMessageEncoding(t *testing.T) {
	config := newEngineConfig(t, 1)

	// sign a message with invalid encoding
	dummyMsg := []byte("dummy message")
	sig, err := config.SignBLS(dummyMsg)
	require.NoError(t, err)

	sigBytes := bls.SignatureToBytes(sig)

	_, verifier := NewBLSAuth(&config.Config)
	err = verifier.Verify(dummyMsg, sigBytes, config.Ctx.NodeID[:])
	require.ErrorIs(t, err, errSignatureVerificationFailed)
}

// TestQCAggregateAndSign tests the aggregation of multiple signatures
// and then verifies the generated quorum certificate on that message.
func TestQCAggregateAndSign(t *testing.T) {
	config := newEngineConfig(t, 2)

	signer, verifier := NewBLSAuth(&config.Config)
	// nodes 1 and 2 will sign the same message
	msg := []byte("Begin at the beginning, and go on till you come to the end: then stop")
	sig, err := signer.Sign(msg)
	require.NoError(t, err)
	require.NoError(t, verifier.Verify(msg, sig, config.Ctx.NodeID[:]))

	node2ID := getNewSigner(config.Nodes, config.Ctx.NodeID)
	config.SignBLS = node2ID.sign
	signer2, verifier2 := NewBLSAuth(&config.Config)
	sig2, err := signer2.Sign(msg)
	require.NoError(t, err)
	require.NoError(t, verifier2.Verify(msg, sig2, node2ID.NodeID[:]))

	// aggregate the signatures into a quorum certificate
	signatureAggregator := SignatureAggregator(verifier)
	qc, err := signatureAggregator.Aggregate(
		[]simplex.Signature{
			{Signer: config.Ctx.NodeID[:], Value: sig},
			{Signer: node2ID.NodeID[:], Value: sig2},
		},
	)
	require.NoError(t, err)
	require.Equal(t, []simplex.NodeID{config.Ctx.NodeID[:], node2ID.NodeID[:]}, qc.Signers())
	// verify the quorum certificate
	require.NoError(t, qc.Verify(msg))

	d := QCDeserializer(verifier)
	// try to deserialize the quorum certificate
	deserializedQC, err := d.DeserializeQuorumCertificate(qc.Bytes())
	require.NoError(t, err)

	require.Equal(t, qc.Signers(), deserializedQC.Signers())
	require.NoError(t, deserializedQC.Verify(msg))
	require.Equal(t, qc.Bytes(), deserializedQC.Bytes())
}

func TestQCSignerNotInMembershipSet(t *testing.T) {
	config := newEngineConfig(t, 2)
	nodeID1 := config.Ctx.NodeID

	signer, verifier := NewBLSAuth(&config.Config)
	// nodes 1 and 2 will sign the same message
	msg := []byte("Begin at the beginning, and go on till you come to the end: then stop")
	sig, err := signer.Sign(msg)
	require.NoError(t, err)
	require.NoError(t, verifier.Verify(msg, sig, config.Ctx.NodeID[:]))

	// add a new validator, but it won't be in the membership set of the first node signer/verifier
	vds := generateTestValidators(t, 1)
	config.Ctx.NodeID = vds[0].NodeID
	config.Validators[vds[0].NodeID] = &vds[0].GetValidatorOutput
	config.SignBLS = vds[0].sign

	// sign the same message with the new node
	signer2, verifier2 := NewBLSAuth(&config.Config)
	sig2, err := signer2.Sign(msg)
	require.NoError(t, err)
	require.NoError(t, verifier2.Verify(msg, sig2, vds[0].NodeID[:]))

	// aggregate the signatures into a quorum certificate
	signatureAggregator := SignatureAggregator(verifier)
	_, err = signatureAggregator.Aggregate(
		[]simplex.Signature{
			{Signer: nodeID1[:], Value: sig},
			{Signer: config.Ctx.NodeID[:], Value: sig2},
		},
	)
	require.ErrorIs(t, err, errSignerNotFound)
}

func TestQCDeserializerInvalidInput(t *testing.T) {
	config := newEngineConfig(t, 2)

	_, verifier := NewBLSAuth(&config.Config)
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

	signer, verifier := NewBLSAuth(&config.Config)
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
	require.ErrorIs(t, err, errNotEnoughSigners)
}

func TestSignatureAggregatorInvalidSignatureBytes(t *testing.T) {
	config := newEngineConfig(t, 2)

	signer, verifier := NewBLSAuth(&config.Config)
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
	config := newEngineConfig(t, 4)

	_, verifier := NewBLSAuth(&config.Config)
	msg := []byte("test message")

	// Create signatures from all 4 nodes
	signatures := make([]simplex.Signature, 4)
	for i, node := range config.Nodes {
		config.SignBLS = node.sign
		nodeSigner, _ := NewBLSAuth(&config.Config)
		sig, err := nodeSigner.Sign(msg)
		require.NoError(t, err)

		signatures[i] = simplex.Signature{Signer: node.NodeID[:], Value: sig}
	}

	// Aggregate should only use the first 3 signatures
	signatureAggregator := SignatureAggregator(verifier)
	qc, err := signatureAggregator.Aggregate(signatures)
	require.NoError(t, err)

	// Should only have 3 signers, not 4
	require.Len(t, qc.Signers(), simplex.Quorum(len(config.Nodes)))
	require.NoError(t, qc.Verify(msg))
}

func TestQCVerifyWithWrongMessage(t *testing.T) {
	config := newEngineConfig(t, 2)

	signer, verifier := NewBLSAuth(&config.Config)
	originalMsg := []byte("original message")
	wrongMsg := []byte("wrong message")

	// Create signatures for original message
	sig1, err := signer.Sign(originalMsg)
	require.NoError(t, err)

	config.SignBLS = config.Nodes[1].sign
	signer2, _ := NewBLSAuth(&config.Config)
	sig2, err := signer2.Sign(originalMsg)
	require.NoError(t, err)

	signatureAggregator := SignatureAggregator(verifier)
	qc, err := signatureAggregator.Aggregate(
		[]simplex.Signature{
			{Signer: config.Nodes[0].NodeID[:], Value: sig1},
			{Signer: config.Nodes[1].NodeID[:], Value: sig2},
		},
	)
	require.NoError(t, err)

	// Verify with original message should succeed
	require.NoError(t, qc.Verify(originalMsg))

	// Verify with wrong message should fail
	err = qc.Verify(wrongMsg)
	require.ErrorIs(t, err, errSignatureVerificationFailed)
}

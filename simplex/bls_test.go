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
	config, err := newEngineConfig(nil)
	require.NoError(t, err)

	signer, verifier := NewBLSAuth(config)

	msg := "Begin at the beginning, and go on till you come to the end: then stop"

	sig, err := signer.Sign([]byte(msg))
	require.NoError(t, err)

	err = verifier.Verify([]byte(msg), sig, config.Ctx.NodeID[:])
	require.NoError(t, err)
}

func TestSignerNotInMemberSet(t *testing.T) {
	config, err := newEngineConfig(nil)
	require.NoError(t, err)
	signer, verifier := NewBLSAuth(config)

	msg := "Begin at the beginning, and go on till you come to the end: then stop"

	sig, err := signer.Sign([]byte(msg))
	require.NoError(t, err)

	notInMembershipSet := ids.GenerateTestNodeID()
	err = verifier.Verify([]byte(msg), sig, notInMembershipSet[:])
	require.ErrorIs(t, err, errSignerNotFound)
}

func TestSignerInvalidMessageEncoding(t *testing.T) {
	config, err := newEngineConfig(nil)
	require.NoError(t, err)

	// sign a message with invalid encoding
	dummyMsg := []byte("dummy message")
	sig, err := config.SignBLS(dummyMsg)
	require.NoError(t, err)

	sigBytes := bls.SignatureToBytes(sig)

	_, verifier := NewBLSAuth(config)
	err = verifier.Verify(dummyMsg, sigBytes, config.Ctx.NodeID[:])
	require.ErrorIs(t, err, errSignatureVerificationFailed)
}

// TestQCAggregateAndSign tests the aggregation of multiple signatures
// and then verifies the generated quorum certificate on that message.
func TestQCAggregateAndSign(t *testing.T) {
	options, err := withNodes(2)
	require.NoError(t, err)
	config, err := newEngineConfig(options)
	require.NoError(t, err)

	signer, verifier := NewBLSAuth(config)
	// nodes 1 and 2 will sign the same message
	msg := []byte("Begin at the beginning, and go on till you come to the end: then stop")
	sig, err := signer.Sign(msg)
	require.NoError(t, err)
	require.NoError(t, verifier.Verify(msg, sig, config.Ctx.NodeID[:]))

	options.curNode = options.allNodes[1]
	config2, err := newEngineConfig(options)
	require.NoError(t, err)

	signer2, verifier2 := NewBLSAuth(config2)
	sig2, err := signer2.Sign(msg)
	require.NoError(t, err)
	require.NoError(t, verifier2.Verify(msg, sig2, config2.Ctx.NodeID[:]))

	// aggregate the signatures into a quorum certificate
	signatureAggregator := SignatureAggregator(verifier)
	qc, err := signatureAggregator.Aggregate(
		[]simplex.Signature{
			{Signer: config.Ctx.NodeID[:], Value: sig},
			{Signer: config2.Ctx.NodeID[:], Value: sig2},
		},
	)
	require.NoError(t, err)
	require.Equal(t, []simplex.NodeID{config.Ctx.NodeID[:], config2.Ctx.NodeID[:]}, qc.Signers())
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
	options, err := withNodes(2)
	require.NoError(t, err)
	config, err := newEngineConfig(options)
	require.NoError(t, err)

	signer, verifier := NewBLSAuth(config)
	// nodes 1 and 2 will sign the same message
	msg := []byte("Begin at the beginning, and go on till you come to the end: then stop")
	sig, err := signer.Sign(msg)
	require.NoError(t, err)
	require.NoError(t, verifier.Verify(msg, sig, config.Ctx.NodeID[:]))

	// vds is not in the membership set of the first node
	vds, err := newTestValidators(1)
	require.NoError(t, err)

	options.curNode = vds[0]
	options.allNodes[0] = vds[0]
	config2, err := newEngineConfig(options)
	require.NoError(t, err)
	signer2, verifier2 := NewBLSAuth(config2)
	sig2, err := signer2.Sign(msg)
	require.NoError(t, err)
	require.NoError(t, verifier2.Verify(msg, sig2, config2.Ctx.NodeID[:]))

	// aggregate the signatures into a quorum certificate
	signatureAggregator := SignatureAggregator(verifier)
	_, err = signatureAggregator.Aggregate(
		[]simplex.Signature{
			{Signer: config.Ctx.NodeID[:], Value: sig},
			{Signer: config2.Ctx.NodeID[:], Value: sig2},
		},
	)
	require.ErrorIs(t, err, errSignerNotFound)
}

func TestQCDeserializerInvalidInput(t *testing.T) {
	options, err := withNodes(2)
	require.NoError(t, err)
	config, err := newEngineConfig(options)
	require.NoError(t, err)

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
			err:   errInvalidByteSliceLength,
		},
		{
			name:  "invalid signature bytes",
			input: make([]byte, simplex.Quorum(len(verifier.nodeID2PK))*ids.NodeIDLen+bls.SignatureLen),
			err:   errFailedToParseSignature,
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
	options, err := withNodes(3)
	require.NoError(t, err)
	config, err := newEngineConfig(options)
	require.NoError(t, err)

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
	require.ErrorIs(t, err, errNotEnoughSigners)
}

func TestSignatureAggregatorInvalidSignatureBytes(t *testing.T) {
	options, err := withNodes(2)
	require.NoError(t, err)
	config, err := newEngineConfig(options)
	require.NoError(t, err)

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
	options, err := withNodes(4) // quorum will be 3
	require.NoError(t, err)
	config, err := newEngineConfig(options)
	require.NoError(t, err)

	_, verifier := NewBLSAuth(config)
	msg := []byte("test message")

	// Create signatures from all 4 nodes
	signatures := make([]simplex.Signature, 4)
	for i, node := range options.allNodes {
		options.curNode = node
		nodeConfig, err := newEngineConfig(options)
		require.NoError(t, err)

		nodeSigner, _ := NewBLSAuth(nodeConfig)
		sig, err := nodeSigner.Sign(msg)
		require.NoError(t, err)

		signatures[i] = simplex.Signature{Signer: nodeConfig.Ctx.NodeID[:], Value: sig}
	}

	// Aggregate should only use the first 3 signatures
	signatureAggregator := SignatureAggregator(verifier)
	qc, err := signatureAggregator.Aggregate(signatures)
	require.NoError(t, err)

	// Should only have 3 signers, not 4
	require.Len(t, qc.Signers(), simplex.Quorum(len(options.allNodes)))
	require.NoError(t, qc.Verify(msg))
}

func TestQCVerifyWithWrongMessage(t *testing.T) {
	options, err := withNodes(2)
	require.NoError(t, err)
	config, err := newEngineConfig(options)
	require.NoError(t, err)

	signer, verifier := NewBLSAuth(config)
	originalMsg := []byte("original message")
	wrongMsg := []byte("wrong message")

	// Create signatures for original message
	sig1, err := signer.Sign(originalMsg)
	require.NoError(t, err)

	options.curNode = options.allNodes[1]
	config2, err := newEngineConfig(options)
	require.NoError(t, err)

	signer2, _ := NewBLSAuth(config2)
	sig2, err := signer2.Sign(originalMsg)
	require.NoError(t, err)

	signatureAggregator := SignatureAggregator(verifier)
	qc, err := signatureAggregator.Aggregate(
		[]simplex.Signature{
			{Signer: config.Ctx.NodeID[:], Value: sig1},
			{Signer: config2.Ctx.NodeID[:], Value: sig2},
		},
	)
	require.NoError(t, err)

	// Verify with original message should succeed
	require.NoError(t, qc.Verify(originalMsg))

	// Verify with wrong message should fail
	err = qc.Verify(wrongMsg)
	require.ErrorIs(t, err, errSignatureVerificationFailed)
}

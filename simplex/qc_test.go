// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package simplex

import (
	"testing"

	"github.com/ava-labs/simplex"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/utils/crypto/bls"
)

// TestQCDuplicateSigners tests verification fails if the
// same signer signs multiple times.
func TestQCDuplicateSigners(t *testing.T) {
	configs := newNetworkConfigs(t, 2)
	quorum := simplex.Quorum(len(configs))
	msg := []byte("Begin at the beginning, and go on till you come to the end: then stop")

	signer, verifier := NewBLSAuth(configs[0])
	sig, err := signer.Sign(msg)
	require.NoError(t, err)
	require.NoError(t, verifier.Verify(msg, sig, configs[0].Ctx.NodeID[:]))

	signatures := make([]simplex.Signature, 0, quorum)
	signatures = append(signatures, simplex.Signature{
		Signer: configs[0].Ctx.NodeID[:],
		Value:  sig,
	})

	// Duplicate the first signer to test for duplicate signers
	signatures = append(signatures, simplex.Signature{
		Signer: configs[0].Ctx.NodeID[:],
		Value:  sig,
	})

	// aggregate the signatures into a quorum certificate
	signatureAggregator := SignatureAggregator{verifier: &verifier}
	qc, err := signatureAggregator.Aggregate(signatures)

	require.NoError(t, err)
	err = qc.Verify(msg)
	require.ErrorIs(t, err, errDuplicateSigner)
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
	signatureAggregator := SignatureAggregator{verifier: &verifier}
	_, err = signatureAggregator.Aggregate(
		[]simplex.Signature{
			{Signer: node1.Ctx.NodeID[:], Value: sig},
			{Signer: node2.Ctx.NodeID[:], Value: sig2},
		},
	)
	require.ErrorIs(t, err, errSignerNotFound)
}

func TestQCDeserializerInvalidBytes(t *testing.T) {
	config := newEngineConfig(t, 2)

	_, verifier := NewBLSAuth(config)
	deserializer := QCDeserializer{verifier: &verifier}

	sig, err := config.SignBLS([]byte("message2Sign"))
	require.NoError(t, err)

	tests := []struct {
		name  string
		input []byte
		err   error
	}{
		{
			name:  "qc bytes too short",
			input: make([]byte, 10),
			err:   errFailedToParseQC,
		},
		{
			name: "invalid bitset",
			input: func() []byte {
				sigBytes := bls.SignatureToBytes(sig)
				canotoQC := &canotoQC{
					Sig:     [bls.SignatureLen]byte(sigBytes),
					Signers: []byte{0, 1, 2, 3},
				}
				return canotoQC.MarshalCanoto()
			}(),
			err: errInvalidBitSet,
		},
		{
			name: "invalid qc signer bytes",
			input: func() []byte {
				canotoQC := &canotoQC{
					Sig:     [bls.SignatureLen]byte{},
					Signers: []byte{0, 1, 2, 3},
				}
				return canotoQC.MarshalCanoto()
			}(),
			err: errFailedToParseSignature,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := deserializer.DeserializeQuorumCertificate(tt.input)
			require.ErrorIs(t, err, tt.err)
		})
	}
}

func TestSignatureAggregation(t *testing.T) {
	configs := newNetworkConfigs(t, 4)
	msg := []byte("test message")

	tests := []struct {
		name        string
		signers     []simplex.Signature
		expectError error
	}{
		{
			name: "invalid signature bytes",
			signers: []simplex.Signature{
				{Signer: configs[0].Ctx.NodeID[:], Value: []byte("invalid signature")},
				{Signer: configs[1].Ctx.NodeID[:], Value: []byte("another invalid signature")},
				{Signer: configs[2].Ctx.NodeID[:], Value: []byte("another invalid signature")},
			},
			expectError: errFailedToParseSignature,
		},
		{
			name: "excess signatures",
			signers: func() []simplex.Signature {
				sigs := make([]simplex.Signature, 0, 4)
				for _, config := range configs {
					signer, _ := NewBLSAuth(config)
					sig, err := signer.Sign(msg)
					require.NoError(t, err)
					sigs = append(sigs, simplex.Signature{Signer: config.Ctx.NodeID[:], Value: sig})
				}
				return sigs
			}(),
			expectError: nil, // should succeed, but only use the first 3 signatures
		},
		{
			name: "insufficient signatures",
			signers: func() []simplex.Signature {
				sigs := make([]simplex.Signature, 0, 2)
				for i, config := range configs {
					signer, _ := NewBLSAuth(config)
					if i < 2 {
						sig, err := signer.Sign(msg)
						require.NoError(t, err)
						sigs = append(sigs, simplex.Signature{Signer: config.Ctx.NodeID[:], Value: sig})
					}
				}
				return sigs
			}(),
			expectError: errUnexpectedSigners,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, verifier := NewBLSAuth(configs[0])
			signatureAggregator := SignatureAggregator{verifier: &verifier}
			qc, err := signatureAggregator.Aggregate(tt.signers)
			require.ErrorIs(t, err, tt.expectError)

			// verify the quorum certificate
			if tt.expectError == nil {
				require.Len(t, qc.Signers(), simplex.Quorum(len(configs)))
				require.NoError(t, qc.Verify(msg))

				d := QCDeserializer{verifier: &verifier}
				// try to deserialize the quorum certificate
				deserializedQC, err := d.DeserializeQuorumCertificate(qc.Bytes())
				require.NoError(t, err)

				for _, signer := range qc.Signers() {
					require.Contains(t, deserializedQC.Signers(), signer)
				}
				require.Equal(t, qc.Bytes(), deserializedQC.Bytes())
				require.NoError(t, deserializedQC.Verify(msg))
			}
		})
	}
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

	signatureAggregator := SignatureAggregator{verifier: &verifier}
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

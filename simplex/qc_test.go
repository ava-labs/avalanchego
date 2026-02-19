// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package simplex

import (
	"testing"

	"github.com/ava-labs/simplex"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"github.com/ava-labs/avalanchego/utils/set"
)

// TestQCDuplicateSigners tests verification fails if the
// same signer signs multiple times.
func TestQCDuplicateSigners(t *testing.T) {
	configs := newNetworkConfigs(t, 4)
	quorum := simplex.Quorum(len(configs))
	msg := []byte("Begin at the beginning, and go on till you come to the end: then stop")

	signer, verifier, err := NewBLSAuth(configs[0])
	require.NoError(t, err)
	sig, err := signer.Sign(msg)
	require.NoError(t, err)
	require.NoError(t, verifier.Verify(msg, sig, configs[0].Ctx.NodeID[:]))

	signatures := make([]simplex.Signature, 0, quorum)
	for range quorum {
		signatures = append(signatures, simplex.Signature{
			Signer: configs[0].Ctx.NodeID[:],
			Value:  sig,
		})
	}

	// aggregate the signatures into a quorum certificate
	signatureAggregator := SignatureAggregator{verifier: &verifier}
	qc, err := signatureAggregator.Aggregate(signatures)
	require.NoError(t, err)

	err = qc.Verify(msg)
	require.ErrorIs(t, err, errDuplicateSigner)
}

func TestQCDeserializerInvalidBytes(t *testing.T) {
	config := newEngineConfig(t, 2)

	_, verifier, err := NewBLSAuth(config)
	require.NoError(t, err)
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
					signer, _, err := NewBLSAuth(config)
					require.NoError(t, err)
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
					signer, _, err := NewBLSAuth(config)
					require.NoError(t, err)
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
		{
			name: "not in membership set",
			signers: func() []simplex.Signature {
				sigs := make([]simplex.Signature, 0, 3)
				for i, config := range configs {
					signer, _, err := NewBLSAuth(config)
					require.NoError(t, err)
					if i < 2 {
						sig, err := signer.Sign(msg)
						require.NoError(t, err)
						sigs = append(sigs, simplex.Signature{Signer: config.Ctx.NodeID[:], Value: sig})
					}
				}

				// add a signature from a node not in the membership set
				newConfig := newNetworkConfigs(t, 4)
				signer, _, err := NewBLSAuth(newConfig[0])
				require.NoError(t, err)
				sig, err := signer.Sign(msg)
				require.NoError(t, err)
				sigs = append(sigs, simplex.Signature{Signer: newConfig[0].Ctx.NodeID[:], Value: sig})

				return sigs
			}(),
			expectError: errSignerNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, verifier, err := NewBLSAuth(configs[0])
			require.NoError(t, err)
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
	configs := newNetworkConfigs(t, 4)
	originalMsg := []byte("original message")
	wrongMsg := []byte("wrong message")
	sigs := make([]simplex.Signature, 0, len(configs))

	for i := range 3 {
		signer, _, err := NewBLSAuth(configs[i])
		require.NoError(t, err)
		sig, err := signer.Sign(originalMsg)
		require.NoError(t, err)
		sigs = append(sigs, simplex.Signature{
			Signer: configs[i].Ctx.NodeID[:],
			Value:  sig,
		})
	}

	_, verifier, err := NewBLSAuth(configs[0])
	require.NoError(t, err)
	signatureAggregator := SignatureAggregator{verifier: &verifier}
	qc, err := signatureAggregator.Aggregate(sigs)
	require.NoError(t, err)

	// Verify with original message should succeed
	require.NoError(t, qc.Verify(originalMsg))

	// Verify with wrong message should fail
	err = qc.Verify(wrongMsg)
	require.ErrorIs(t, err, errSignatureVerificationFailed)
}

func TestFilterNodes(t *testing.T) {
	nodeID1 := ids.GenerateTestNodeID()
	nodeID2 := ids.GenerateTestNodeID()
	tests := []struct {
		name          string
		indices       set.Bits
		nodes         []ids.NodeID
		expectedNodes []ids.NodeID
		expectedErr   error
	}{
		{
			name:          "empty",
			indices:       set.NewBits(),
			nodes:         []ids.NodeID{},
			expectedNodes: []ids.NodeID{},
			expectedErr:   nil,
		},
		{
			name:        "unknown node",
			indices:     set.NewBits(2),
			nodes:       []ids.NodeID{nodeID1, nodeID2},
			expectedErr: errNodeNotFound,
		},
		{
			name:          "two filtered out",
			indices:       set.NewBits(),
			nodes:         []ids.NodeID{nodeID1, nodeID2},
			expectedNodes: []ids.NodeID{},
			expectedErr:   nil,
		},
		{
			name:    "one filtered out",
			indices: set.NewBits(1),
			nodes:   []ids.NodeID{nodeID1, nodeID2},
			expectedNodes: []ids.NodeID{
				nodeID2,
			},
			expectedErr: nil,
		},
		{
			name:    "none filtered out",
			indices: set.NewBits(0, 1),
			nodes:   []ids.NodeID{nodeID1, nodeID2},
			expectedNodes: []ids.NodeID{
				nodeID1,
				nodeID2,
			},
			expectedErr: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)

			nodes, err := filterNodes(tt.indices, tt.nodes)
			require.ErrorIs(err, tt.expectedErr)
			if tt.expectedErr != nil {
				return
			}
			require.Equal(tt.expectedNodes, nodes)
		})
	}
}

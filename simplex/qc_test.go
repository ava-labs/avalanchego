// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package simplex

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/graft/simplex/common"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"github.com/ava-labs/avalanchego/utils/set"
)

// TestQCDuplicateSigners tests that signatures from the same signer are
// not double counted when aggregating.
func TestQCDuplicateSigners(t *testing.T) {
	configs := newNetworkConfigs(t, 4)
	quorum := common.Quorum(len(configs))
	msg := []byte("Begin at the beginning, and go on till you come to the end: then stop")

	signer, verifier, err := NewBLSAuth(configs[0])
	require.NoError(t, err)
	sig, err := signer.Sign(msg)
	require.NoError(t, err)
	require.NoError(t, verifier.VerifySignature(msg, sig, publicKeyOf(t, configs[0])))

	signatures := make([]common.Signature, 0, quorum)
	for range quorum {
		signatures = append(signatures, common.Signature{
			Signer: common.NodeIDFromAvalancheID(configs[0].Ctx.NodeID),
			Value:  sig,
		})
	}

	// A single signer repeated quorum times is still a single signer.
	_, err = newTestSignatureAggregator(configs[0]).Aggregate(signatures)
	require.ErrorIs(t, err, errUnexpectedSigners)
}

func TestQCDeserializerInvalidBytes(t *testing.T) {
	config := newEngineConfig(t, 2)

	deserializer := newTestQCDeserializer(config)
	nodeIDs := testValidators(config).NodeIDs()

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
			_, err := deserializer.DeserializeQuorumCertificate(tt.input, nodeIDs)
			require.ErrorIs(t, err, tt.err)
		})
	}
}

func TestSignatureAggregation(t *testing.T) {
	configs := newNetworkConfigs(t, 4)
	msg := []byte("test message")

	tests := []struct {
		name        string
		signers     []common.Signature
		expectError error
	}{
		{
			name: "invalid signature bytes",
			signers: []common.Signature{
				{Signer: common.NodeIDFromAvalancheID(configs[0].Ctx.NodeID), Value: []byte("invalid signature")},
				{Signer: common.NodeIDFromAvalancheID(configs[1].Ctx.NodeID), Value: []byte("another invalid signature")},
				{Signer: common.NodeIDFromAvalancheID(configs[2].Ctx.NodeID), Value: []byte("another invalid signature")},
			},
			expectError: errFailedToParseSignature,
		},
		{
			name: "excess signatures",
			signers: func() []common.Signature {
				sigs := make([]common.Signature, 0, 4)
				for _, config := range configs {
					signer, _, err := NewBLSAuth(config)
					require.NoError(t, err)
					sig, err := signer.Sign(msg)
					require.NoError(t, err)
					sigs = append(sigs, common.Signature{Signer: common.NodeIDFromAvalancheID(config.Ctx.NodeID), Value: sig})
				}
				return sigs
			}(),
			expectError: nil, // should succeed, but only use the first 3 signatures
		},
		{
			name: "insufficient signatures",
			signers: func() []common.Signature {
				sigs := make([]common.Signature, 0, 2)
				for i, config := range configs {
					signer, _, err := NewBLSAuth(config)
					require.NoError(t, err)
					if i < 2 {
						sig, err := signer.Sign(msg)
						require.NoError(t, err)
						sigs = append(sigs, common.Signature{Signer: common.NodeIDFromAvalancheID(config.Ctx.NodeID), Value: sig})
					}
				}
				return sigs
			}(),
			expectError: errUnexpectedSigners,
		},
		{
			name: "not in membership set",
			signers: func() []common.Signature {
				sigs := make([]common.Signature, 0, 4)
				for i, config := range configs {
					signer, _, err := NewBLSAuth(config)
					require.NoError(t, err)
					if i < 3 {
						sig, err := signer.Sign(msg)
						require.NoError(t, err)
						sigs = append(sigs, common.Signature{Signer: common.NodeIDFromAvalancheID(config.Ctx.NodeID), Value: sig})
					}
				}

				// add a signature from a node not in the membership set
				newConfig := newNetworkConfigs(t, 4)
				signer, _, err := NewBLSAuth(newConfig[0])
				require.NoError(t, err)
				sig, err := signer.Sign(msg)
				require.NoError(t, err)
				sigs = append(sigs, common.Signature{Signer: common.NodeIDFromAvalancheID(newConfig[0].Ctx.NodeID), Value: sig})

				return sigs
			}(),
			expectError: errSignerNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			signatureAggregator := newTestSignatureAggregator(configs[0])
			qc, err := signatureAggregator.Aggregate(tt.signers)
			require.ErrorIs(t, err, tt.expectError)

			// verify the quorum certificate
			if tt.expectError == nil {
				// every distinct signature is included, there is no truncation to a quorum
				require.Len(t, qc.Signers(), len(tt.signers))
				validators := testValidators(configs[0])
				require.NoError(t, qc.Verify(msg, validators))

				d := newTestQCDeserializer(configs[0])
				// try to deserialize the quorum certificate
				deserializedQC, err := d.DeserializeQuorumCertificate(qc.Bytes(), validators.NodeIDs())
				require.NoError(t, err)

				for _, signer := range qc.Signers() {
					require.Contains(t, deserializedQC.Signers(), signer)
				}
				require.Equal(t, qc.Bytes(), deserializedQC.Bytes())
				require.NoError(t, deserializedQC.Verify(msg, validators))
			}
		})
	}
}

func TestQCVerifyWithWrongMessage(t *testing.T) {
	configs := newNetworkConfigs(t, 4)
	originalMsg := []byte("original message")
	wrongMsg := []byte("wrong message")
	sigs := make([]common.Signature, 0, len(configs))

	for i := range 3 {
		signer, _, err := NewBLSAuth(configs[i])
		require.NoError(t, err)
		sig, err := signer.Sign(originalMsg)
		require.NoError(t, err)
		sigs = append(sigs, common.Signature{
			Signer: common.NodeIDFromAvalancheID(configs[i].Ctx.NodeID),
			Value:  sig,
		})
	}

	qc, err := newTestSignatureAggregator(configs[0]).Aggregate(sigs)
	require.NoError(t, err)

	validators := testValidators(configs[0])

	// Verify with original message should succeed
	require.NoError(t, qc.Verify(originalMsg, validators))

	// Verify with wrong message should fail
	err = qc.Verify(wrongMsg, validators)
	require.ErrorIs(t, err, errSignatureVerificationFailed)
}

func TestIsQuorum(t *testing.T) {
	configs := newNetworkConfigs(t, 4)
	aggregator := newTestSignatureAggregator(configs[0])

	nodeIDs := make([]common.NodeID, len(configs))
	for i, config := range configs {
		nodeIDs[i] = common.NodeIDFromAvalancheID(config.Ctx.NodeID)
	}
	unknownNodeID := ids.GenerateTestNodeID()

	tests := []struct {
		name     string
		nodes    []common.NodeID
		expected bool
	}{
		{
			name:     "below quorum",
			nodes:    []common.NodeID{nodeIDs[0], nodeIDs[1]},
			expected: false,
		},
		{
			name:     "exact quorum",
			nodes:    []common.NodeID{nodeIDs[0], nodeIDs[1], nodeIDs[2]},
			expected: true,
		},
		{
			name:     "unknown node",
			nodes:    []common.NodeID{nodeIDs[0], nodeIDs[1], common.NodeIDFromAvalancheID(unknownNodeID)},
			expected: false,
		},
		{
			name:     "duplicates still meet quorum",
			nodes:    []common.NodeID{nodeIDs[0], nodeIDs[0], nodeIDs[1], nodeIDs[2]},
			expected: true,
		},
		{
			name:     "duplicates do not meet quorum",
			nodes:    []common.NodeID{nodeIDs[0], nodeIDs[0], nodeIDs[1]},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.expected, aggregator.IsQuorum(tt.nodes))
		})
	}
}

func TestFilterNodes(t *testing.T) {
	nodeID1 := common.NodeIDFromAvalancheID(ids.GenerateTestNodeID())
	nodeID2 := common.NodeIDFromAvalancheID(ids.GenerateTestNodeID())
	tests := []struct {
		name          string
		indices       set.Bits
		nodes         common.NodeIDs
		expectedNodes common.NodeIDs
		expectedErr   error
	}{
		{
			name:          "empty",
			indices:       set.NewBits(),
			nodes:         common.NodeIDs{},
			expectedNodes: common.NodeIDs{},
			expectedErr:   nil,
		},
		{
			name:        "unknown node",
			indices:     set.NewBits(2),
			nodes:       common.NodeIDs{nodeID1, nodeID2},
			expectedErr: errNodeNotFound,
		},
		{
			name:          "two filtered out",
			indices:       set.NewBits(),
			nodes:         common.NodeIDs{nodeID1, nodeID2},
			expectedNodes: common.NodeIDs{},
			expectedErr:   nil,
		},
		{
			name:    "one filtered out",
			indices: set.NewBits(1),
			nodes:   common.NodeIDs{nodeID1, nodeID2},
			expectedNodes: common.NodeIDs{
				nodeID2,
			},
			expectedErr: nil,
		},
		{
			name:    "none filtered out",
			indices: set.NewBits(0, 1),
			nodes:   common.NodeIDs{nodeID1, nodeID2},
			expectedNodes: common.NodeIDs{
				nodeID1,
				nodeID2,
			},
			expectedErr: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)

			nodes, err := nodesFromBitmask(tt.indices, tt.nodes)
			require.ErrorIs(err, tt.expectedErr)
			if tt.expectedErr != nil {
				return
			}
			require.Equal(tt.expectedNodes, nodes)
		})
	}
}

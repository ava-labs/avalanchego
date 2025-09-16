// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package simplex

import (
	"context"
	"testing"

	"github.com/ava-labs/simplex"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/proto/pb/p2p"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/snow/networking/sender/sendermock"
)

// TestSimplexEngineHandlesSimplexMessages tests that the Simplex engine can handle
// various types of Simplex messages without errors. The contents of the messages do not have
// to be valid, as long as they can be parsed and processed by the engine.
func TestSimplexEngineHandlesSimplexMessages(t *testing.T) {
	configs := newNetworkConfigs(t, 4)
	ctx := context.Background()

	config := configs[0]
	config.Sender.(*sendermock.ExternalSender).EXPECT().Send(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
	consensusCtx := &snow.ConsensusContext{}
	engine, err := NewEngine(consensusCtx, ctx, config)
	require.NoError(t, err)

	config.VM.(*wrappedVM).ParseBlockF = func(_ context.Context, _ []byte) (snowman.Block, error) {
		return newTestBlock(t, newBlockConfig{round: 1}).vmBlock, nil
	}

	require.NoError(t, engine.Start(ctx, 1))
	md := engine.epoch.Metadata()
	require.Equal(t, uint64(1), md.Seq)
	require.Equal(t, uint64(1), md.Round)

	qcBytes := buildQCBytes(t, configs)

	allSimplexMessages := []*p2p.Simplex{
		simplexBlockProposal,
		simplexVote,
		simplexEmptyVote,
		simplexFinalizeVote,
		NewNotarizationMessage(qcBytes),
		NewEmptyNotarizationMessage(qcBytes),
		NewFinalizationMessage(qcBytes),
		simplexReplicationRequest,
		NewReplicationResponseMessage(qcBytes),
	}

	for _, msg := range allSimplexMessages {
		require.NoError(t, engine.SimplexMessage(ctx, configs[1].Ctx.NodeID, msg))
	}
}

func TestSimplexEngineRejectsMalformedSimplexMessages(t *testing.T) {
	configs := newNetworkConfigs(t, 4)
	ctx := context.Background()

	config := configs[0]
	config.Sender.(*sendermock.ExternalSender).
		EXPECT().
		Send(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
		AnyTimes()

	consensusCtx := &snow.ConsensusContext{}
	engine, err := NewEngine(consensusCtx, ctx, config)
	require.NoError(t, err)

	config.VM.(*wrappedVM).ParseBlockF = func(_ context.Context, _ []byte) (snowman.Block, error) {
		return newTestBlock(t, newBlockConfig{round: 1}).vmBlock, nil
	}

	require.NoError(t, engine.Start(ctx, 1))

	tests := []struct {
		name        string
		msg         *p2p.Simplex
		expectedErr error
	}{
		// --- BlockProposal ---
		{
			name: "BlockProposal missing block",
			msg: &p2p.Simplex{
				Message: &p2p.Simplex_BlockProposal{
					BlockProposal: &p2p.BlockProposal{},
				},
			},
			expectedErr: errFailedToParseMetadata,
		},
		{
			name: "BlockProposal missing vote",
			msg: &p2p.Simplex{
				Message: &p2p.Simplex_BlockProposal{
					BlockProposal: &p2p.BlockProposal{
						Block: blockBytes,
					},
				},
			},
			expectedErr: errNilField,
		},
		// --- Vote ---
		{
			name: "Vote with nil header",
			msg: &p2p.Simplex{
				Message: &p2p.Simplex_Vote{
					Vote: &p2p.Vote{},
				},
			},
			expectedErr: errNilField,
		},
		// --- EmptyVote ---
		{
			name: "EmptyVote with nil metadata",
			msg: &p2p.Simplex{
				Message: &p2p.Simplex_EmptyVote{
					EmptyVote: &p2p.EmptyVote{},
				},
			},
			expectedErr: errNilField,
		},
		{
			name: "EmptyVote with nil signature",
			msg: &p2p.Simplex{
				Message: &p2p.Simplex_EmptyVote{
					EmptyVote: &p2p.EmptyVote{
						Metadata: &p2p.EmptyVoteMetadata{
							Epoch: 1, Round: 1,
						},
					},
				},
			},
			expectedErr: errNilField,
		},
		// --- FinalizeVote ---
		{
			name: "FinalizeVote missing header + sig",
			msg: &p2p.Simplex{
				Message: &p2p.Simplex_FinalizeVote{
					FinalizeVote: &p2p.Vote{},
				},
			},
			expectedErr: errNilField,
		},
		// --- Notarization ---
		{
			name: "Notarization with nil BlockHeader",
			msg: &p2p.Simplex{
				Message: &p2p.Simplex_Notarization{
					Notarization: &p2p.QuorumCertificate{},
				},
			},
			expectedErr: errNilField,
		},
		{
			name: "Notarization with nil QC",
			msg: &p2p.Simplex{
				Message: &p2p.Simplex_Notarization{
					Notarization: &p2p.QuorumCertificate{
						BlockHeader: &p2p.BlockHeader{
							Metadata: p2pProtocolMetadata,
							Digest:   digest[:],
						},
					},
				},
			},
			expectedErr: errNilField,
		},
		// --- EmptyNotarization ---
		{
			name: "EmptyNotarization with nil metadata",
			msg: &p2p.Simplex{
				Message: &p2p.Simplex_EmptyNotarization{
					EmptyNotarization: &p2p.EmptyNotarization{},
				},
			},
			expectedErr: errNilField,
		},
		{
			name: "EmptyNotarization with nil QC",
			msg: &p2p.Simplex{
				Message: &p2p.Simplex_EmptyNotarization{
					EmptyNotarization: &p2p.EmptyNotarization{
						Metadata: &p2p.EmptyVoteMetadata{
							Epoch: 1, Round: 1,
						},
					},
				},
			},
			expectedErr: errNilField,
		},
		// --- Finalization ---
		{
			name: "Finalization with nil BlockHeader",
			msg: &p2p.Simplex{
				Message: &p2p.Simplex_Finalization{
					Finalization: &p2p.QuorumCertificate{},
				},
			},
			expectedErr: errNilField,
		},
		{
			name: "Finalization with nil QC",
			msg: &p2p.Simplex{
				Message: &p2p.Simplex_Finalization{
					Finalization: &p2p.QuorumCertificate{
						BlockHeader: &p2p.BlockHeader{
							Metadata: p2pProtocolMetadata,
							Digest:   digest[:],
						},
					},
				},
			},
			expectedErr: errNilField,
		},
		// --- ReplicationRequest ---
		{
			name: "ReplicationRequest missing seqs/round (allowed)",
			msg: &p2p.Simplex{
				Message: &p2p.Simplex_ReplicationRequest{
					ReplicationRequest: &p2p.ReplicationRequest{},
				},
			},
			expectedErr: nil,
		},
		// --- ReplicationResponse ---
		{
			name: "ReplicationResponse with nil latest_round",
			msg: &p2p.Simplex{
				Message: &p2p.Simplex_ReplicationResponse{
					ReplicationResponse: &p2p.ReplicationResponse{},
				},
			},
			expectedErr: errNilField,
		},
		// --- Edge case: invalid digest ---
		{
			name: "Vote with invalid digest length",
			msg: &p2p.Simplex{
				Message: &p2p.Simplex_Vote{
					Vote: &p2p.Vote{
						BlockHeader: &p2p.BlockHeader{
							Metadata: &p2p.ProtocolMetadata{},
						},
						Signature: &p2p.Signature{
							Signer: []byte{1, 2, 3}, Value: []byte{4},
						},
					},
				},
			},
			expectedErr: errInvalidDigestLength,
		},
		{
			name: "Vote with invalid signer",
			msg: &p2p.Simplex{
				Message: &p2p.Simplex_Vote{
					Vote: &p2p.Vote{
						BlockHeader: &p2p.BlockHeader{
							Metadata: p2pProtocolMetadata,
							Digest:   digest[:],
						},
						Signature: &p2p.Signature{
							Signer: []byte{1, 2, 3}, Value: []byte{4},
						},
					},
				},
			},
			expectedErr: errInvalidSigner,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := engine.SimplexMessage(ctx, configs[1].Ctx.NodeID, tt.msg)
			require.ErrorIs(t, err, tt.expectedErr)
		})
	}
}

// -----------------------------------------------------------------------------
// Shared Test Data
// -----------------------------------------------------------------------------

var (
	blockMetadata = &simplex.ProtocolMetadata{
		Version: 1,
		Epoch:   2,
		Round:   3,
		Seq:     4,
		Prev:    simplex.Digest{0x1, 0x2, 0x3, 0x4},
	}

	canotoBlock = &canotoSimplexBlock{
		Metadata:   blockMetadata.Bytes(),
		InnerBlock: []byte("inner-block"),
	}

	blockBytes = canotoBlock.MarshalCanoto()
	digest     = computeDigest(blockBytes)
	nodeID     = ids.GenerateTestNodeID()
	signer     = nodeID[:]
)

// -----------------------------------------------------------------------------
// Predefined Messages (no QC dependency)
// -----------------------------------------------------------------------------

var (
	p2pProtocolMetadata = &p2p.ProtocolMetadata{
		Version: 1,
		Epoch:   42,
		Round:   7,
		Seq:     100,
		Prev:    digest[:],
	}
	// BlockProposal
	simplexBlockProposal = &p2p.Simplex{
		ChainId: []byte("chain-1"),
		Message: &p2p.Simplex_BlockProposal{
			BlockProposal: &p2p.BlockProposal{
				Block: blockBytes,
				Vote: &p2p.Vote{
					BlockHeader: &p2p.BlockHeader{
						Metadata: p2pProtocolMetadata,
						Digest:   digest[:],
					},
					Signature: &p2p.Signature{Signer: signer, Value: []byte("signature")},
				},
			},
		},
	}

	// Vote
	simplexVote = &p2p.Simplex{
		ChainId: []byte("chain-1"),
		Message: &p2p.Simplex_Vote{
			Vote: &p2p.Vote{
				BlockHeader: &p2p.BlockHeader{
					Metadata: &p2p.ProtocolMetadata{
						Version: 1, Epoch: 1, Round: 1, Seq: 1, Prev: digest[:],
					},
					Digest: digest[:],
				},
				Signature: &p2p.Signature{Signer: signer, Value: []byte("vote-sig")},
			},
		},
	}

	// EmptyVote
	simplexEmptyVote = &p2p.Simplex{
		ChainId: []byte("chain-1"),
		Message: &p2p.Simplex_EmptyVote{
			EmptyVote: &p2p.EmptyVote{
				Metadata:  &p2p.EmptyVoteMetadata{Epoch: 1, Round: 1},
				Signature: &p2p.Signature{Signer: signer, Value: []byte("emptyvote-sig")},
			},
		},
	}

	// FinalizeVote
	simplexFinalizeVote = &p2p.Simplex{
		ChainId: []byte("chain-1"),
		Message: &p2p.Simplex_FinalizeVote{
			FinalizeVote: &p2p.Vote{
				BlockHeader: &p2p.BlockHeader{
					Metadata: &p2p.ProtocolMetadata{
						Version: 1, Epoch: 2, Round: 5, Seq: 50, Prev: digest[:],
					},
					Digest: digest[:],
				},
				Signature: &p2p.Signature{Signer: signer, Value: []byte("finalize-sig")},
			},
		},
	}

	// ReplicationRequest
	simplexReplicationRequest = &p2p.Simplex{
		ChainId: []byte("chain-1"),
		Message: &p2p.Simplex_ReplicationRequest{
			ReplicationRequest: &p2p.ReplicationRequest{
				Seqs:        []uint64{1, 2, 3},
				LatestRound: 42,
			},
		},
	}
)

// -----------------------------------------------------------------------------
// QC Builder
// -----------------------------------------------------------------------------

func buildQCBytes(t *testing.T, configs []*Config) []byte {
	t.Helper()

	msg := []byte("test message")
	sigs := make([]simplex.Signature, 0, len(configs))

	for _, config := range configs {
		signer, _ := NewBLSAuth(config)
		sig, _ := signer.Sign(msg)
		sigs = append(sigs, simplex.Signature{
			Signer: config.Ctx.NodeID[:],
			Value:  sig,
		})
	}

	_, verifier := NewBLSAuth(configs[0])
	agg := SignatureAggregator{verifier: &verifier}

	qc, err := agg.Aggregate(sigs)
	require.NoError(t, err)
	return qc.Bytes()
}

// -----------------------------------------------------------------------------
// QC-Dependent Message Constructors
// -----------------------------------------------------------------------------

func NewNotarizationMessage(qcBytes []byte) *p2p.Simplex {
	return &p2p.Simplex{
		ChainId: []byte("chain-1"),
		Message: &p2p.Simplex_Notarization{
			Notarization: &p2p.QuorumCertificate{
				BlockHeader: &p2p.BlockHeader{
					Metadata: &p2p.ProtocolMetadata{
						Version: 1, Epoch: 3, Round: 8, Seq: 75, Prev: digest[:],
					},
					Digest: digest[:],
				},
				QuorumCertificate: qcBytes,
			},
		},
	}
}

func NewEmptyNotarizationMessage(qcBytes []byte) *p2p.Simplex {
	return &p2p.Simplex{
		ChainId: []byte("chain-1"),
		Message: &p2p.Simplex_EmptyNotarization{
			EmptyNotarization: &p2p.EmptyNotarization{
				Metadata:          &p2p.EmptyVoteMetadata{Epoch: 1, Round: 1},
				QuorumCertificate: qcBytes,
			},
		},
	}
}

func NewFinalizationMessage(qcBytes []byte) *p2p.Simplex {
	return &p2p.Simplex{
		ChainId: []byte("chain-1"),
		Message: &p2p.Simplex_Finalization{
			Finalization: &p2p.QuorumCertificate{
				BlockHeader: &p2p.BlockHeader{
					Metadata: &p2p.ProtocolMetadata{
						Version: 1, Epoch: 5, Round: 11, Seq: 120, Prev: digest[:],
					},
					Digest: digest[:],
				},
				QuorumCertificate: qcBytes,
			},
		},
	}
}

func NewReplicationResponseMessage(qcBytes []byte) *p2p.Simplex {
	return &p2p.Simplex{
		ChainId: []byte("chain-1"),
		Message: &p2p.Simplex_ReplicationResponse{
			ReplicationResponse: &p2p.ReplicationResponse{
				Data: []*p2p.QuorumRound{
					{
						Block: blockBytes,
						Notarization: &p2p.QuorumCertificate{
							BlockHeader: &p2p.BlockHeader{
								Metadata: &p2p.ProtocolMetadata{
									Version: 1, Epoch: 6, Round: 13, Seq: 150, Prev: digest[:],
								},
								Digest: digest[:],
							},
							QuorumCertificate: qcBytes,
						},
					},
				},
				LatestRound: &p2p.QuorumRound{Block: blockBytes},
			},
		},
	}
}

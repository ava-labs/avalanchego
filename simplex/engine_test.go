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
	ctx := context.Background()
	engine, configs := setupEngine(t)

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
			_, err := engine.p2pToSimplexMessage(ctx, tt.msg)
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

func buildQCWithBytes(t testing.TB, configs []*Config, msg []byte) []byte {
	t.Helper()

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

func buildQCBytes(t testing.TB, configs []*Config) []byte {
	return buildQCWithBytes(t, configs, []byte("test message"))
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

func FuzzSimplexVotes(f *testing.F) {
	f.Add(digest[:], signer, uint64(1), uint64(1), uint64(1), uint32(1))
	f.Fuzz(func(t *testing.T, blockDigest []byte, signer []byte, epoch, round, seq uint64, version uint32) {
		ctx := context.Background()
		engine, configs := setupEngine(t)

		msg := &p2p.Simplex{
			Message: &p2p.Simplex_Vote{
				Vote: &p2p.Vote{
					BlockHeader: &p2p.BlockHeader{
						Metadata: &p2p.ProtocolMetadata{
							Version: version,
							Epoch:   epoch,
							Round:   round,
							Seq:     seq,
							Prev:    blockDigest,
						},
						Digest: blockDigest,
					},
					Signature: &p2p.Signature{Signer: signer, Value: []byte("vote-sig")},
				},
			},
		}

		require.NoError(t, engine.SimplexMessage(ctx, configs[1].Ctx.NodeID, msg))
	})
}

func FuzzSimplexEmptyVotes(f *testing.F) {
	f.Add(signer, uint64(1), uint64(1), []byte("emptyvote-sig"))
	f.Fuzz(func(t *testing.T, signer []byte, epoch, round uint64, signerValue []byte) {
		ctx := context.Background()
		engine, configs := setupEngine(t)

		msg := &p2p.Simplex{
			Message: &p2p.Simplex_EmptyVote{
				EmptyVote: &p2p.EmptyVote{
					Metadata:  &p2p.EmptyVoteMetadata{Epoch: epoch, Round: round},
					Signature: &p2p.Signature{Signer: signer, Value: signerValue},
				},
			},
		}

		require.NoError(t, engine.SimplexMessage(ctx, configs[1].Ctx.NodeID, msg))
	})
}

func FuzzSimplexFinalizeVotes(f *testing.F) {
	f.Add(digest[:], signer, []byte("signervalue"), uint64(2), uint64(5), uint64(50), uint32(1))
	f.Fuzz(func(t *testing.T, signer []byte, signerValue []byte, blockDigest []byte, epoch, round, seq uint64, version uint32) {
		ctx := context.Background()
		engine, configs := setupEngine(t)

		msg := &p2p.Simplex{
			Message: &p2p.Simplex_FinalizeVote{
				FinalizeVote: &p2p.Vote{
					BlockHeader: &p2p.BlockHeader{
						Metadata: &p2p.ProtocolMetadata{
							Version: version,
							Epoch:   epoch,
							Round:   round,
							Seq:     seq,
							Prev:    blockDigest,
						},
						Digest: blockDigest,
					},
					Signature: &p2p.Signature{Signer: signer, Value: signerValue},
				},
			},
		}

		require.NoError(t, engine.SimplexMessage(ctx, configs[1].Ctx.NodeID, msg))
	})
}

func FuzzSimplexNotarizations(f *testing.F) {
	f.Add([]byte("qc-data"), digest[:], uint64(3), uint64(8), uint64(75), uint32(1))
	f.Fuzz(func(t *testing.T, qcData, blockDigest []byte, epoch, round, seq uint64, version uint32) {
		ctx := context.Background()
		engine, configs := setupEngine(t)
		qc := buildQCWithBytes(t, configs, qcData)

		msg := &p2p.Simplex{
			Message: &p2p.Simplex_Notarization{
				Notarization: &p2p.QuorumCertificate{
					BlockHeader: &p2p.BlockHeader{
						Metadata: &p2p.ProtocolMetadata{
							Version: version,
							Epoch:   epoch,
							Round:   round,
							Seq:     seq,
							Prev:    blockDigest,
						},
						Digest: blockDigest,
					},
					QuorumCertificate: qc,
				},
			},
		}

		require.NoError(t, engine.SimplexMessage(ctx, configs[1].Ctx.NodeID, msg))
	})
}

func FuzzSimplexFinalizations(f *testing.F) {
	f.Add([]byte("qc-data"), digest[:], uint64(5), uint64(11), uint64(120), uint32(1))
	f.Fuzz(func(t *testing.T, qcData, blockDigest []byte, epoch, round, seq uint64, version uint32) {
		ctx := context.Background()
		engine, configs := setupEngine(t)
		qc := buildQCWithBytes(t, configs, qcData)

		msg := &p2p.Simplex{
			Message: &p2p.Simplex_Finalization{
				Finalization: &p2p.QuorumCertificate{
					BlockHeader: &p2p.BlockHeader{
						Metadata: &p2p.ProtocolMetadata{
							Version: version,
							Epoch:   epoch,
							Round:   round,
							Seq:     seq,
							Prev:    blockDigest,
						},
						Digest: blockDigest,
					},
					QuorumCertificate: qc,
				},
			},
		}

		require.NoError(t, engine.SimplexMessage(ctx, configs[1].Ctx.NodeID, msg))
	})
}

func FuzzSimplexReplicationRequests(f *testing.F) {
	f.Add(uint64(1), uint64(2), uint64(3), uint64(42))
	f.Fuzz(func(t *testing.T, seq1, seq2, seq3, latestRound uint64) {
		ctx := context.Background()
		engine, configs := setupEngine(t)

		msg := &p2p.Simplex{
			Message: &p2p.Simplex_ReplicationRequest{
				ReplicationRequest: &p2p.ReplicationRequest{
					Seqs:        []uint64{seq1, seq2, seq3},
					LatestRound: latestRound,
				},
			},
		}

		require.NoError(t, engine.SimplexMessage(ctx, configs[1].Ctx.NodeID, msg))
	})
}

func FuzzSimplexReplicationResponses(f *testing.F) {
	f.Log("hello!!!!")
	f.Add([]byte("qc-data"), digest[:], uint64(6), uint64(13), uint64(150), uint32(1))
	f.Fuzz(func(t *testing.T, qcData, blockDigest []byte, epoch, round, seq uint64, version uint32) {
		t.Log("!!FuzzSimplexReplicationResponses called with:", qcData, blockDigest, epoch, round, seq, version)
		ctx := context.Background()
		engine, configs := setupEngine(t)
		qc := buildQCWithBytes(t, configs, qcData)

		msg := &p2p.Simplex{
			Message: &p2p.Simplex_ReplicationResponse{
				ReplicationResponse: &p2p.ReplicationResponse{
					Data: []*p2p.QuorumRound{
						{
							Block: blockBytes,
							Notarization: &p2p.QuorumCertificate{
								BlockHeader: &p2p.BlockHeader{
									Metadata: &p2p.ProtocolMetadata{
										Version: version,
										Epoch:   epoch,
										Round:   round,
										Seq:     seq,
										Prev:    blockDigest,
									},
									Digest: blockDigest,
								},
								QuorumCertificate: qc,
							},
						},
					},
					LatestRound: &p2p.QuorumRound{Block: blockBytes},
				},
			},
		}

		require.NoError(t, engine.SimplexMessage(ctx, configs[1].Ctx.NodeID, msg))
	})
}

func FuzzSimplexBlockProposals(f *testing.F) {
	f.Add(blockBytes, digest[:], uint64(112), uint64(18), uint64(7), uint32(1))
	f.Fuzz(func(t *testing.T, blockBytes []byte, blockDigest []byte, round, epoch, seq uint64, version uint32) {
		ctx := context.Background()
		engine, configs := setupEngine(t)
		msg := &p2p.Simplex{
			ChainId: []byte("chain-1"),
			Message: &p2p.Simplex_BlockProposal{
				BlockProposal: &p2p.BlockProposal{
					Block: blockBytes,
					Vote: &p2p.Vote{
						BlockHeader: &p2p.BlockHeader{
							Metadata: &p2p.ProtocolMetadata{
								Version: version,
								Epoch:   epoch,
								Round:   round,
								Seq:     seq,
								Prev:    blockDigest,
							},
							Digest: blockDigest,
						},
						Signature: &p2p.Signature{Signer: signer, Value: []byte("signature")},
					},
				},
			},
		}

		require.NoError(t, engine.SimplexMessage(ctx, configs[1].Ctx.NodeID, msg))
	})
}

func FuzzSimplexEmptyNotarizations(f *testing.F) {
	f.Add([]byte("random msg data i am passing into QC"), uint64(112), uint64(18))
	f.Fuzz(func(t *testing.T, data []byte, round uint64, epoch uint64) {
		ctx := context.Background()
		engine, configs := setupEngine(t)
		qc := buildQCWithBytes(t, configs, data)
		msg := &p2p.Simplex{
			Message: &p2p.Simplex_EmptyNotarization{
				EmptyNotarization: &p2p.EmptyNotarization{
					Metadata:          &p2p.EmptyVoteMetadata{Epoch: epoch, Round: round},
					QuorumCertificate: qc,
				},
			},
		}

		require.NoError(t, engine.SimplexMessage(ctx, configs[1].Ctx.NodeID, msg))
	})
}

func setupEngine(t *testing.T) (*Engine, []*Config) {
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
	return engine, configs
}

package simplex

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/simplex"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/proto/pb/p2p"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
)

// TestSimplexEngineHandlesSimplexMessages tests that the Simplex engine can handle
// various types of Simplex messages without errors. The contents of the messages do not have
// to be valid, as long as they can be parsed and processed by the engine.
func TestSimplexEngineHandlesSimplexMessages(t *testing.T) {
	configs := newNetworkConfigs(t, 4)
	ctx := context.Background()

	config := configs[0]
	engine, err := NewEngine(ctx, config)
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
		err := engine.SimplexMessage(configs[1].Ctx.NodeID, msg)
		require.NoError(t, err)
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
	// BlockProposal
	simplexBlockProposal = &p2p.Simplex{
		ChainId: []byte("chain-1"),
		Message: &p2p.Simplex_BlockProposal{
			BlockProposal: &p2p.BlockProposal{
				Block: blockBytes,
				Vote: &p2p.Vote{
					BlockHeader: &p2p.BlockHeader{
						Metadata: &p2p.ProtocolMetadata{
							Version: 1,
							Epoch:   42,
							Round:   7,
							Seq:     100,
							Prev:    digest[:],
						},
						Digest: digest[:],
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

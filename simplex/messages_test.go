// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package simplex

import (
	"testing"

	"github.com/ava-labs/simplex"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/hashing"
)

func TestNewBlockProposal(t *testing.T) {
	chainID := ids.GenerateTestID()
	genesisBlock := newTestBlock(t, newBlockConfig{})

	msg := &simplex.VerifiedBlockMessage{
		VerifiedBlock: genesisBlock,
		Vote: simplex.Vote{
			Vote: simplex.ToBeSignedVote{
				BlockHeader: genesisBlock.BlockHeader(),
			},
			Signature: simplex.Signature{
				Signer: []byte("test_signer"),
				Value:  []byte("test_signature"),
			},
		},
	}

	got, err := newBlockProposal(chainID, msg)
	require.NoError(t, err)
	require.NotNil(t, got)
	require.Equal(t, chainID[:], got.ChainId)
	require.NotNil(t, got.GetBlockProposal())

	bp := got.GetBlockProposal()
	require.NotNil(t, bp.Block)
	require.NotNil(t, bp.Vote)
	require.Equal(t, []byte(msg.Vote.Signature.Signer), bp.Vote.Signature.Signer)
	require.Equal(t, []byte(msg.Vote.Signature.Value), bp.Vote.Signature.Value)
}

func TestNewVote(t *testing.T) {
	chainID := ids.GenerateTestID()
	genesisBlock := newTestBlock(t, newBlockConfig{})
	block := newTestBlock(t, newBlockConfig{prev: genesisBlock})

	vote := &simplex.Vote{
		Vote: simplex.ToBeSignedVote{
			BlockHeader: block.BlockHeader(),
		},
		Signature: simplex.Signature{
			Signer: []byte("test_signer"),
			Value:  []byte("test_signature"),
		},
	}

	got := newVote(chainID, vote)
	require.NotNil(t, got)
	require.Equal(t, chainID[:], got.ChainId)
	require.NotNil(t, got.GetVote())

	v := got.GetVote()
	require.NotNil(t, v.BlockHeader)
	require.Equal(t, uint32(vote.Vote.BlockHeader.ProtocolMetadata.Version), v.BlockHeader.Metadata.Version)
	require.Equal(t, vote.Vote.BlockHeader.ProtocolMetadata.Epoch, v.BlockHeader.Metadata.Epoch)
	require.Equal(t, vote.Vote.BlockHeader.ProtocolMetadata.Round, v.BlockHeader.Metadata.Round)
	require.Equal(t, vote.Vote.BlockHeader.ProtocolMetadata.Seq, v.BlockHeader.Metadata.Seq)
	require.Equal(t, vote.Vote.BlockHeader.Digest[:], v.BlockHeader.Digest)
	require.Equal(t, []byte(vote.Signature.Signer), v.Signature.Signer)
	require.Equal(t, []byte(vote.Signature.Value), v.Signature.Value)
}

func TestNewEmptyVote(t *testing.T) {
	chainID := ids.GenerateTestID()

	emptyVote := &simplex.EmptyVote{
		Vote: simplex.ToBeSignedEmptyVote{
			EmptyVoteMetadata: simplex.EmptyVoteMetadata{
				Epoch: 1,
				Round: 1,
			},
		},
		Signature: simplex.Signature{
			Signer: []byte("test_signer"),
			Value:  []byte("test_signature"),
		},
	}

	got := newEmptyVote(chainID, emptyVote)
	require.NotNil(t, got)
	require.Equal(t, chainID[:], got.ChainId)
	require.NotNil(t, got.GetEmptyVote())

	ev := got.GetEmptyVote()
	require.NotNil(t, ev.Metadata)
	require.Equal(t, emptyVote.Vote.EmptyVoteMetadata.Epoch, ev.Metadata.Epoch)
	require.Equal(t, emptyVote.Vote.EmptyVoteMetadata.Round, ev.Metadata.Round)
	require.Equal(t, []byte(emptyVote.Signature.Signer), ev.Signature.Signer)
	require.Equal(t, []byte(emptyVote.Signature.Value), ev.Signature.Value)
}

func TestNewFinalizeVote(t *testing.T) {
	chainID := ids.GenerateTestID()
	genesisBlock := newTestBlock(t, newBlockConfig{})
	block := newTestBlock(t, newBlockConfig{prev: genesisBlock})

	finalizeVote := &simplex.FinalizeVote{
		Finalization: simplex.ToBeSignedFinalization{
			BlockHeader: block.BlockHeader(),
		},
		Signature: simplex.Signature{
			Signer: []byte("test_signer"),
			Value:  []byte("test_signature"),
		},
	}

	got := newFinalizeVote(chainID, finalizeVote)
	require.NotNil(t, got)
	require.Equal(t, chainID[:], got.ChainId)
	require.NotNil(t, got.GetFinalizeVote())

	fv := got.GetFinalizeVote()
	require.NotNil(t, fv.BlockHeader)
	require.Equal(t, uint32(finalizeVote.Finalization.BlockHeader.ProtocolMetadata.Version), fv.BlockHeader.Metadata.Version)
	require.Equal(t, finalizeVote.Finalization.BlockHeader.ProtocolMetadata.Epoch, fv.BlockHeader.Metadata.Epoch)
	require.Equal(t, finalizeVote.Finalization.BlockHeader.ProtocolMetadata.Round, fv.BlockHeader.Metadata.Round)
	require.Equal(t, finalizeVote.Finalization.BlockHeader.Digest[:], fv.BlockHeader.Digest)
	require.Equal(t, []byte(finalizeVote.Signature.Signer), fv.Signature.Signer)
	require.Equal(t, []byte(finalizeVote.Signature.Value), fv.Signature.Value)
}

func TestNewNotarization(t *testing.T) {
	chainID := ids.GenerateTestID()
	genesisBlock := newTestBlock(t, newBlockConfig{})
	block := newTestBlock(t, newBlockConfig{prev: genesisBlock})

	notarization := &simplex.Notarization{
		Vote: simplex.ToBeSignedVote{
			BlockHeader: block.BlockHeader(),
		},
		QC: &mockQC{
			bytes:   []byte("test_qc_bytes"),
			signers: []simplex.NodeID{},
		},
	}

	got := newNotarization(chainID, notarization)
	require.NotNil(t, got)
	require.Equal(t, chainID[:], got.ChainId)
	require.NotNil(t, got.GetNotarization())

	n := got.GetNotarization()
	require.NotNil(t, n.BlockHeader)
	require.Equal(t, uint32(notarization.Vote.BlockHeader.ProtocolMetadata.Version), n.BlockHeader.Metadata.Version)
	require.Equal(t, notarization.Vote.BlockHeader.ProtocolMetadata.Epoch, n.BlockHeader.Metadata.Epoch)
	require.Equal(t, notarization.Vote.BlockHeader.ProtocolMetadata.Round, n.BlockHeader.Metadata.Round)
	require.Equal(t, notarization.Vote.BlockHeader.Digest[:], n.BlockHeader.Digest)
	require.Equal(t, []byte("test_qc_bytes"), n.QuorumCertificate)
}

func TestNewEmptyNotarization(t *testing.T) {
	chainID := ids.GenerateTestID()

	emptyNotarization := &simplex.EmptyNotarization{
		Vote: simplex.ToBeSignedEmptyVote{
			EmptyVoteMetadata: simplex.EmptyVoteMetadata{
				Epoch: 1,
				Round: 1,
			},
		},
		QC: &mockQC{
			bytes:   []byte("test_empty_qc_bytes"),
			signers: []simplex.NodeID{},
		},
	}

	got := newEmptyNotarization(chainID, emptyNotarization)
	require.NotNil(t, got)
	require.Equal(t, chainID[:], got.ChainId)
	require.NotNil(t, got.GetEmptyNotarization())

	en := got.GetEmptyNotarization()
	require.NotNil(t, en.Metadata)
	require.Equal(t, emptyNotarization.Vote.EmptyVoteMetadata.Epoch, en.Metadata.Epoch)
	require.Equal(t, emptyNotarization.Vote.EmptyVoteMetadata.Round, en.Metadata.Round)
	require.Equal(t, []byte("test_empty_qc_bytes"), en.QuorumCertificate)
}

func TestNewFinalization(t *testing.T) {
	chainID := ids.GenerateTestID()
	genesisBlock := newTestBlock(t, newBlockConfig{})
	block := newTestBlock(t, newBlockConfig{prev: genesisBlock})

	finalization := &simplex.Finalization{
		Finalization: simplex.ToBeSignedFinalization{
			BlockHeader: block.BlockHeader(),
		},
		QC: &mockQC{
			bytes:   []byte("test_finalization_qc_bytes"),
			signers: []simplex.NodeID{},
		},
	}

	got := newFinalization(chainID, finalization)
	require.NotNil(t, got)
	require.Equal(t, chainID[:], got.ChainId)
	require.NotNil(t, got.GetFinalization())

	f := got.GetFinalization()
	require.NotNil(t, f.BlockHeader)
	require.Equal(t, uint32(finalization.Finalization.BlockHeader.ProtocolMetadata.Version), f.BlockHeader.Metadata.Version)
	require.Equal(t, finalization.Finalization.BlockHeader.ProtocolMetadata.Epoch, f.BlockHeader.Metadata.Epoch)
	require.Equal(t, finalization.Finalization.BlockHeader.ProtocolMetadata.Round, f.BlockHeader.Metadata.Round)
	require.Equal(t, finalization.Finalization.BlockHeader.Digest[:], f.BlockHeader.Digest)
	require.Equal(t, []byte("test_finalization_qc_bytes"), f.QuorumCertificate)
}

func TestNewReplicationRequest(t *testing.T) {
	chainID := ids.GenerateTestID()

	tests := []struct {
		name               string
		chainID            ids.ID
		replicationRequest *simplex.ReplicationRequest
	}{
		{
			name:    "empty seqs",
			chainID: chainID,
			replicationRequest: &simplex.ReplicationRequest{
				Seqs:        []uint64{},
				LatestRound: 10,
			},
		},
		{
			name:    "multiple seqs",
			chainID: chainID,
			replicationRequest: &simplex.ReplicationRequest{
				Seqs:        []uint64{1, 2, 3, 4, 5},
				LatestRound: 100,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := newReplicationRequest(tt.chainID, tt.replicationRequest)
			require.NotNil(t, got)
			require.Equal(t, tt.chainID[:], got.ChainId)
			require.NotNil(t, got.GetReplicationRequest())

			rr := got.GetReplicationRequest()
			require.Equal(t, tt.replicationRequest.Seqs, rr.Seqs)
			require.Equal(t, tt.replicationRequest.LatestRound, rr.LatestRound)
		})
	}
}

func TestNewReplicationResponse(t *testing.T) {
	chainID := ids.GenerateTestID()
	genesisBlock := newTestBlock(t, newBlockConfig{})

	tests := []struct {
		name        string
		chainID     ids.ID
		resp        *simplex.VerifiedReplicationResponse
		expectedErr error
	}{
		{
			name:    "nil latest round",
			chainID: chainID,
			resp: &simplex.VerifiedReplicationResponse{
				Data: []simplex.VerifiedQuorumRound{
					{
						VerifiedBlock: genesisBlock,
					},
				},
				LatestRound: nil,
			},
			expectedErr: nil,
		},
		{
			name:    "empty data",
			chainID: chainID,
			resp: &simplex.VerifiedReplicationResponse{
				Data:        []simplex.VerifiedQuorumRound{},
				LatestRound: nil,
			},
			expectedErr: nil,
		},
		{
			name:    "non-nil latest round",
			chainID: chainID,
			resp: &simplex.VerifiedReplicationResponse{
				Data: []simplex.VerifiedQuorumRound{},
				LatestRound: &simplex.VerifiedQuorumRound{
					VerifiedBlock: genesisBlock,
				},
			},
			expectedErr: nil,
		},
		{
			name:    "with notarization",
			chainID: chainID,
			resp: &simplex.VerifiedReplicationResponse{
				Data: []simplex.VerifiedQuorumRound{
					{
						VerifiedBlock: genesisBlock,
						Notarization: &simplex.Notarization{
							Vote: simplex.ToBeSignedVote{
								BlockHeader: simplex.BlockHeader{
									ProtocolMetadata: genesisMetadata,
									Digest:           genesisBlock.digest,
								},
							},
							QC: &mockQC{
								bytes:   []byte("notarization_qc"),
								signers: []simplex.NodeID{},
							},
						},
					},
				},
				LatestRound: nil,
			},
			expectedErr: nil,
		},
		{
			name:    "with finalization",
			chainID: chainID,
			resp: &simplex.VerifiedReplicationResponse{
				Data: []simplex.VerifiedQuorumRound{
					{
						VerifiedBlock: genesisBlock,
						Finalization: &simplex.Finalization{
							Finalization: simplex.ToBeSignedFinalization{
								BlockHeader: simplex.BlockHeader{
									ProtocolMetadata: genesisMetadata,
									Digest:           genesisBlock.digest,
								},
							},
							QC: &mockQC{
								bytes:   []byte("finalization_qc"),
								signers: []simplex.NodeID{},
							},
						},
					},
				},
				LatestRound: nil,
			},
			expectedErr: nil,
		},
		{
			name:    "with empty notarization",
			chainID: chainID,
			resp: &simplex.VerifiedReplicationResponse{
				Data: []simplex.VerifiedQuorumRound{
					{
						VerifiedBlock: genesisBlock,
						EmptyNotarization: &simplex.EmptyNotarization{
							Vote: simplex.ToBeSignedEmptyVote{
								EmptyVoteMetadata: simplex.EmptyVoteMetadata{
									Epoch: 1,
									Round: 1,
								},
							},
							QC: &mockQC{
								bytes:   []byte("empty_notarization_qc"),
								signers: []simplex.NodeID{},
							},
						},
					},
				},
				LatestRound: nil,
			},
			expectedErr: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := newReplicationResponse(tt.chainID, tt.resp)
			require.ErrorIs(t, err, tt.expectedErr)
			if tt.expectedErr != nil {
				return
			}

			require.NotNil(t, got)
			require.Equal(t, tt.chainID[:], got.ChainId)
			require.NotNil(t, got.GetReplicationResponse())

			rr := got.GetReplicationResponse()
			require.Len(t, rr.Data, len(tt.resp.Data))

			if tt.resp.LatestRound == nil {
				require.Nil(t, rr.LatestRound)
			} else {
				require.NotNil(t, rr.LatestRound)
			}
		})
	}
}

func TestBlockHeaderToP2P(t *testing.T) {
	genesisBlock := newTestBlock(t, newBlockConfig{})
	block := newTestBlock(t, newBlockConfig{prev: genesisBlock})

	bh := block.BlockHeader()

	got := blockHeaderToP2P(bh)
	require.NotNil(t, got)
	require.NotNil(t, got.Metadata)
	require.Equal(t, uint32(bh.ProtocolMetadata.Version), got.Metadata.Version)
	require.Equal(t, bh.ProtocolMetadata.Epoch, got.Metadata.Epoch)
	require.Equal(t, bh.ProtocolMetadata.Round, got.Metadata.Round)
	require.Equal(t, bh.ProtocolMetadata.Seq, got.Metadata.Seq)
	require.Equal(t, bh.ProtocolMetadata.Prev[:], got.Metadata.Prev)
	require.Equal(t, bh.Digest[:], got.Digest)
}

func TestProtocolMetadataToP2P(t *testing.T) {
	genesisBlock := newTestBlock(t, newBlockConfig{})
	block := newTestBlock(t, newBlockConfig{prev: genesisBlock})

	tests := []struct {
		name string
		md   simplex.ProtocolMetadata
	}{
		{
			name: "valid metadata",
			md:   block.metadata,
		},
		{
			name: "genesis metadata",
			md:   genesisBlock.metadata,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := protocolMetadataToP2P(tt.md)
			require.NotNil(t, got)
			require.Equal(t, uint32(tt.md.Version), got.Version)
			require.Equal(t, tt.md.Epoch, got.Epoch)
			require.Equal(t, tt.md.Round, got.Round)
			require.Equal(t, tt.md.Seq, got.Seq)
			require.Equal(t, tt.md.Prev[:], got.Prev)
		})
	}
}

func TestQuorumRoundToP2P(t *testing.T) {
	genesisBlock := newTestBlock(t, newBlockConfig{})

	tests := []struct {
		name        string
		qr          *simplex.VerifiedQuorumRound
		wantNil     bool
		expectedErr error
	}{
		{
			name: "with block only",
			qr: &simplex.VerifiedQuorumRound{
				VerifiedBlock: genesisBlock,
			},
			wantNil:     false,
			expectedErr: nil,
		},
		{
			name: "with notarization",
			qr: &simplex.VerifiedQuorumRound{
				VerifiedBlock: genesisBlock,
				Notarization: &simplex.Notarization{
					Vote: simplex.ToBeSignedVote{
						BlockHeader: simplex.BlockHeader{
							ProtocolMetadata: genesisMetadata,
							Digest:           genesisBlock.digest,
						},
					},
					QC: &mockQC{
						bytes:   []byte("test_qc"),
						signers: []simplex.NodeID{},
					},
				},
			},
			wantNil:     false,
			expectedErr: nil,
		},
		{
			name: "with finalization",
			qr: &simplex.VerifiedQuorumRound{
				VerifiedBlock: genesisBlock,
				Finalization: &simplex.Finalization{
					Finalization: simplex.ToBeSignedFinalization{
						BlockHeader: simplex.BlockHeader{
							ProtocolMetadata: genesisMetadata,
							Digest:           genesisBlock.digest,
						},
					},
					QC: &mockQC{
						bytes:   []byte("test_finalization_qc"),
						signers: []simplex.NodeID{},
					},
				},
			},
			wantNil:     false,
			expectedErr: nil,
		},
		{
			name: "with genesis finalization (nil QC)",
			qr: &simplex.VerifiedQuorumRound{
				VerifiedBlock: genesisBlock,
				Finalization: &simplex.Finalization{
					Finalization: simplex.ToBeSignedFinalization{
						BlockHeader: simplex.BlockHeader{
							ProtocolMetadata: genesisMetadata,
							Digest:           genesisBlock.digest,
						},
					},
					QC: nil,
				},
			},
			wantNil:     true,
			expectedErr: nil,
		},
		{
			name: "with empty notarization",
			qr: &simplex.VerifiedQuorumRound{
				VerifiedBlock: genesisBlock,
				EmptyNotarization: &simplex.EmptyNotarization{
					Vote: simplex.ToBeSignedEmptyVote{
						EmptyVoteMetadata: simplex.EmptyVoteMetadata{
							Epoch: 1,
							Round: 1,
						},
					},
					QC: &mockQC{
						bytes:   []byte("test_empty_qc"),
						signers: []simplex.NodeID{},
					},
				},
			},
			wantNil:     false,
			expectedErr: nil,
		},
		{
			name: "nil block",
			qr: &simplex.VerifiedQuorumRound{
				VerifiedBlock: nil,
			},
			wantNil:     false,
			expectedErr: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := quorumRoundToP2P(tt.qr)
			require.ErrorIs(t, err, tt.expectedErr)
			if tt.expectedErr != nil {
				return
			}

			if tt.wantNil {
				require.Nil(t, got)
				return
			}

			require.NotNil(t, got)

			if tt.qr.VerifiedBlock != nil {
				require.NotNil(t, got.Block)
			} else {
				require.Nil(t, got.Block)
			}

			if tt.qr.Notarization != nil {
				require.NotNil(t, got.Notarization)
				require.Equal(t, []byte("test_qc"), got.Notarization.QuorumCertificate)
			}

			if tt.qr.Finalization != nil && tt.qr.Finalization.QC != nil {
				require.NotNil(t, got.Finalization)
				require.Equal(t, []byte("test_finalization_qc"), got.Finalization.QuorumCertificate)
			}

			if tt.qr.EmptyNotarization != nil {
				require.NotNil(t, got.EmptyNotarization)
				require.Equal(t, []byte("test_empty_qc"), got.EmptyNotarization.QuorumCertificate)
			}
		})
	}
}

func TestEmptyVoteMetadataToP2P(t *testing.T) {
	tests := []struct {
		name string
		ev   simplex.EmptyVoteMetadata
	}{
		{
			name: "valid metadata",
			ev: simplex.EmptyVoteMetadata{
				Epoch: 1,
				Round: 1,
			},
		},
		{
			name: "zero values",
			ev: simplex.EmptyVoteMetadata{
				Epoch: 0,
				Round: 0,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := emptyVoteMetadataToP2P(tt.ev)
			require.NotNil(t, got)
			require.Equal(t, tt.ev.Epoch, got.Epoch)
			require.Equal(t, tt.ev.Round, got.Round)
		})
	}
}

// mockQC is a mock implementation of simplex.QuorumCertificate for testing.
type mockQC struct {
	bytes   []byte
	signers []simplex.NodeID
}

func (m *mockQC) Bytes() []byte {
	return m.bytes
}

func (m *mockQC) Signers() []simplex.NodeID {
	return m.signers
}

func (m *mockQC) Verify([]byte) error {
	return nil
}

// testDigest creates a simplex.Digest from arbitrary bytes for testing.
func testDigest(data []byte) simplex.Digest {
	return hashing.ComputeHash256Array(data)
}

// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package simplex

import (
	"github.com/ava-labs/simplex"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/proto/pb/p2p"
)

func newBlockProposal(
	chainID ids.ID,
	block []byte,
	vote simplex.Vote,
) *p2p.Simplex {
	return &p2p.Simplex{
		ChainId: chainID[:],
		Message: &p2p.Simplex_BlockProposal{
			BlockProposal: &p2p.BlockProposal{
				Block: block,
				Vote: &p2p.Vote{
					BlockHeader: blockHeaderToP2P(vote.Vote.BlockHeader),
					Signature: &p2p.Signature{
						Signer: vote.Signature.Signer,
						Value:  vote.Signature.Value,
					},
				},
			},
		},
	}
}

func newVote(
	chainID ids.ID,
	vote *simplex.Vote,
) *p2p.Simplex {
	return &p2p.Simplex{
		ChainId: chainID[:],
		Message: &p2p.Simplex_Vote{
			Vote: &p2p.Vote{
				BlockHeader: blockHeaderToP2P(vote.Vote.BlockHeader),
				Signature: &p2p.Signature{
					Signer: vote.Signature.Signer,
					Value:  vote.Signature.Value,
				},
			},
		},
	}
}

func newEmptyVote(
	chainID ids.ID,
	emptyVote *simplex.EmptyVote,
) *p2p.Simplex {
	return &p2p.Simplex{
		ChainId: chainID[:],
		Message: &p2p.Simplex_EmptyVote{
			EmptyVote: &p2p.EmptyVote{
				Metadata: emptyVoteMetadataToP2P(emptyVote.Vote.EmptyVoteMetadata),
				Signature: &p2p.Signature{
					Signer: emptyVote.Signature.Signer,
					Value:  emptyVote.Signature.Value,
				},
			},
		},
	}
}

func newFinalizeVote(
	chainID ids.ID,
	finalizeVote *simplex.FinalizeVote,
) *p2p.Simplex {
	return &p2p.Simplex{
		ChainId: chainID[:],
		Message: &p2p.Simplex_FinalizeVote{
			FinalizeVote: &p2p.Vote{
				BlockHeader: blockHeaderToP2P(finalizeVote.Finalization.BlockHeader),
				Signature: &p2p.Signature{
					Signer: finalizeVote.Signature.Signer,
					Value:  finalizeVote.Signature.Value,
				},
			},
		},
	}
}

func newNotarization(
	chainID ids.ID,
	notarization *simplex.Notarization,
) *p2p.Simplex {
	return &p2p.Simplex{
		ChainId: chainID[:],
		Message: &p2p.Simplex_Notarization{
			Notarization: &p2p.QuorumCertificate{
				BlockHeader:       blockHeaderToP2P(notarization.Vote.BlockHeader),
				QuorumCertificate: notarization.QC.Bytes(),
			},
		},
	}
}

func newEmptyNotarization(
	chainID ids.ID,
	emptyNotarization *simplex.EmptyNotarization,
) *p2p.Simplex {
	return &p2p.Simplex{
		ChainId: chainID[:],
		Message: &p2p.Simplex_EmptyNotarization{
			EmptyNotarization: &p2p.EmptyNotarization{
				Metadata:          emptyVoteMetadataToP2P(emptyNotarization.Vote.EmptyVoteMetadata),
				QuorumCertificate: emptyNotarization.QC.Bytes(),
			},
		},
	}
}

func newFinalization(
	chainID ids.ID,
	finalization *simplex.Finalization,
) *p2p.Simplex {
	return &p2p.Simplex{
		ChainId: chainID[:],
		Message: &p2p.Simplex_Finalization{
			Finalization: &p2p.QuorumCertificate{
				BlockHeader:       blockHeaderToP2P(finalization.Finalization.BlockHeader),
				QuorumCertificate: finalization.QC.Bytes(),
			},
		},
	}
}

func newReplicationRequest(
	chainID ids.ID,
	replicationRequest *simplex.ReplicationRequest,
) *p2p.Simplex {
	return &p2p.Simplex{
		ChainId: chainID[:],
		Message: &p2p.Simplex_ReplicationRequest{
			ReplicationRequest: &p2p.ReplicationRequest{
				Seqs:        replicationRequest.Seqs,
				LatestRound: replicationRequest.LatestRound,
			},
		},
	}
}

func newReplicationResponse(
	chainID ids.ID,
	replicationResponse *simplex.VerifiedReplicationResponse,
) (*p2p.Simplex, error) {
	data := replicationResponse.Data
	latestRound := replicationResponse.LatestRound

	qrs := make([]*p2p.QuorumRound, 0, len(data))
	for _, qr := range data {
		p2pQR, err := quorumRoundToP2P(&qr)
		if err != nil {
			return nil, err
		}
		qrs = append(qrs, p2pQR)
	}

	latestQR, err := quorumRoundToP2P(latestRound)
	if err != nil {
		return nil, err
	}

	return &p2p.Simplex{
		ChainId: chainID[:],
		Message: &p2p.Simplex_ReplicationResponse{
			ReplicationResponse: &p2p.ReplicationResponse{
				Data:        qrs,
				LatestRound: latestQR,
			},
		},
	}, nil
}

func blockHeaderToP2P(bh simplex.BlockHeader) *p2p.BlockHeader {
	return &p2p.BlockHeader{
		Metadata: protocolMetadataToP2P(bh.ProtocolMetadata),
		Digest:   bh.Digest[:],
	}
}

func protocolMetadataToP2P(md simplex.ProtocolMetadata) *p2p.ProtocolMetadata {
	return &p2p.ProtocolMetadata{
		Version: uint32(md.Version),
		Epoch:   md.Epoch,
		Round:   md.Round,
		Seq:     md.Seq,
		Prev:    md.Prev[:],
	}
}

func quorumRoundToP2P(qr *simplex.VerifiedQuorumRound) (*p2p.QuorumRound, error) {
	p2pQR := &p2p.QuorumRound{}

	if qr.VerifiedBlock != nil {
		bytes, err := qr.VerifiedBlock.Bytes()
		if err != nil {
			return nil, err
		}

		p2pQR.Block = bytes
	}
	if qr.Notarization != nil {
		p2pQR.Notarization = &p2p.QuorumCertificate{
			BlockHeader:       blockHeaderToP2P(qr.Notarization.Vote.BlockHeader),
			QuorumCertificate: qr.Notarization.QC.Bytes(),
		}
	}
	if qr.Finalization != nil {
		p2pQR.Finalization = &p2p.QuorumCertificate{
			BlockHeader:       blockHeaderToP2P(qr.Finalization.Finalization.BlockHeader),
			QuorumCertificate: qr.Finalization.QC.Bytes(),
		}
	}
	if qr.EmptyNotarization != nil {
		p2pQR.EmptyNotarization = &p2p.EmptyNotarization{
			Metadata:          emptyVoteMetadataToP2P(qr.EmptyNotarization.Vote.EmptyVoteMetadata),
			QuorumCertificate: qr.EmptyNotarization.QC.Bytes(),
		}
	}
	return p2pQR, nil
}

func emptyVoteMetadataToP2P(ev simplex.EmptyVoteMetadata) *p2p.EmptyVoteMetadata {
	return &p2p.EmptyVoteMetadata{
		Epoch: ev.Epoch,
		Round: ev.Round,
	}
}

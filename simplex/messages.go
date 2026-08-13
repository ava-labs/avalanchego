// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package simplex

import (
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/proto/pb/p2p"

	simplexcommon "github.com/ava-labs/simplex/common"
)

func newBlockProposal(
	chainID ids.ID,
	msg *simplexcommon.VerifiedBlockMessage,
) *p2p.Simplex {
	bytes := msg.VerifiedBlock.Bytes()
	vote := msg.Vote

	return &p2p.Simplex{
		ChainId: chainID[:],
		Message: &p2p.Simplex_BlockProposal{
			BlockProposal: &p2p.BlockProposal{
				Block: bytes,
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
	vote *simplexcommon.Vote,
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
	emptyVote *simplexcommon.EmptyVote,
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
	finalizeVote *simplexcommon.FinalizeVote,
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
	notarization *simplexcommon.Notarization,
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
	emptyNotarization *simplexcommon.EmptyNotarization,
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
	finalization *simplexcommon.Finalization,
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
	replicationRequest *simplexcommon.ReplicationRequest,
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
	replicationResponse *simplexcommon.VerifiedReplicationResponse,
) *p2p.Simplex {
	data := replicationResponse.Data
	latestRound := replicationResponse.LatestRound

	qrs := make([]*p2p.QuorumRound, 0, len(data))
	for _, qr := range data {
		p2pQR := quorumRoundToP2P(&qr)
		if p2pQR == nil {
			continue
		}
		qrs = append(qrs, p2pQR)
	}

	var latestQR *p2p.QuorumRound
	if latestRound != nil {
		qr := quorumRoundToP2P(latestRound)
		if qr == nil {
			return nil
		}
		latestQR = qr
	}
	return &p2p.Simplex{
		ChainId: chainID[:],
		Message: &p2p.Simplex_ReplicationResponse{
			ReplicationResponse: &p2p.ReplicationResponse{
				Data:        qrs,
				LatestRound: latestQR,
			},
		},
	}
}

func blockHeaderToP2P(bh simplexcommon.BlockHeader) *p2p.BlockHeader {
	return &p2p.BlockHeader{
		Metadata: protocolMetadataToP2P(bh.ProtocolMetadata),
		Digest:   bh.Digest[:],
	}
}

func protocolMetadataToP2P(md simplexcommon.ProtocolMetadata) *p2p.ProtocolMetadata {
	return &p2p.ProtocolMetadata{
		Version: uint32(md.Version),
		Epoch:   md.Epoch,
		Round:   md.Round,
		Seq:     md.Seq,
		Prev:    md.Prev[:],
	}
}

func quorumRoundToP2P(qr *simplexcommon.VerifiedQuorumRound) *p2p.QuorumRound {
	p2pQR := &p2p.QuorumRound{}

	if qr.VerifiedBlock != nil {
		bytes := qr.VerifiedBlock.Bytes()
		p2pQR.Block = bytes
	}
	if qr.Notarization != nil {
		p2pQR.Notarization = &p2p.QuorumCertificate{
			BlockHeader:       blockHeaderToP2P(qr.Notarization.Vote.BlockHeader),
			QuorumCertificate: qr.Notarization.QC.Bytes(),
		}
	}
	if qr.Finalization != nil {
		// This can only happen if the finalization of the genesis block is being sent
		if qr.Finalization.QC == nil {
			return nil
		}
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
	return p2pQR
}

func emptyVoteMetadataToP2P(ev simplexcommon.EmptyVoteMetadata) *p2p.EmptyVoteMetadata {
	return &p2p.EmptyVoteMetadata{
		Epoch: ev.Epoch,
		Round: ev.Round,
	}
}

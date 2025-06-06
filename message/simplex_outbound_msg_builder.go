// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package message

import (
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/proto/pb/p2p"
	"github.com/ava-labs/simplex"
)

type SimplexOutboundMessageBuilder interface {
	BlockProposal(
		chainID ids.ID,
		block []byte,
		vote simplex.Vote,
	) (OutboundMessage, error)

	Vote(
		chainID ids.ID,
		blockHeader simplex.BlockHeader,
		signature simplex.Signature,
	) (OutboundMessage, error)

	EmptyVote(
		chainID ids.ID,
		protocolMetadata simplex.ProtocolMetadata,
		signature simplex.Signature,
	) (OutboundMessage, error)

	FinalizeVote(
		chainID ids.ID,
		blockHeader simplex.BlockHeader,
		signature simplex.Signature,
	) (OutboundMessage, error)

	Notarization(
		chainID ids.ID,
		blockHeader simplex.BlockHeader,
		qc []byte,
	) (OutboundMessage, error)

	EmptyNotarization(
		chainID ids.ID,
		protocolMetadata simplex.ProtocolMetadata,
		qc []byte,
	) (OutboundMessage, error)

	Finalization(
		chainID ids.ID,
		blockHeader simplex.BlockHeader,
		qc []byte,
	) (OutboundMessage, error)

	ReplicationRequest(
		chainID ids.ID,
		seqs []uint64,
		latestRound uint64,
	) (OutboundMessage, error)

	VerifiedReplicationResponse(
		chainID ids.ID,
		data []simplex.VerifiedQuorumRound,
		latestRound *simplex.VerifiedQuorumRound,
	) (OutboundMessage, error)
}

func (b *outMsgBuilder) BlockProposal(
	chainID ids.ID,
	block []byte,
	vote simplex.Vote,
) (OutboundMessage, error) {
	return b.builder.createOutbound(
		&p2p.Message{
			Message: &p2p.Message_Simplex{
				Simplex: &p2p.Simplex{
					ChainId: chainID[:],
					Message: &p2p.Simplex_BlockProposal{
						BlockProposal: &p2p.BlockProposal{
							Block: block,
							Vote: &p2p.Vote{
								Vote: b.simplexMetadataToP2P(vote.Vote.BlockHeader),
								Signature: &p2p.Signature{
									Signer: vote.Signature.Signer[:],
									Value:  vote.Signature.Value[:],
								},
							},
						},
					},
				},
			},
		},
		b.compressionType,
		false,
	)
}

func (b *outMsgBuilder) Vote(
	chainID ids.ID,
	blockHeader simplex.BlockHeader,
	signature simplex.Signature,
) (OutboundMessage, error) {
	return b.builder.createOutbound(
		&p2p.Message{
			Message: &p2p.Message_Simplex{
				Simplex: &p2p.Simplex{
					ChainId: chainID[:],
					Message: &p2p.Simplex_Vote{
						Vote: &p2p.Vote{
							Vote: b.simplexMetadataToP2P(blockHeader),
							Signature: &p2p.Signature{
								Signer: signature.Signer[:],
								Value:  signature.Value[:],
							},
						},
					},
				},
			},
		},
		b.compressionType,
		false,
	)
}

func (b *outMsgBuilder) EmptyVote(
	chainID ids.ID,
	protocolMetadata simplex.ProtocolMetadata,
	signature simplex.Signature,
) (OutboundMessage, error) {
	return b.builder.createOutbound(
		&p2p.Message{
			Message: &p2p.Message_Simplex{
				Simplex: &p2p.Simplex{
					ChainId: chainID[:],
					Message: &p2p.Simplex_EmptyVote{
						EmptyVote: &p2p.EmptyVote{
							Vote: b.simplexProtocolMetadataToP2p(protocolMetadata),
							Signature: &p2p.Signature{
								Signer: signature.Signer[:],
								Value:  signature.Value[:],
							},
						},
					},
				},
			},
		},
		b.compressionType,
		false,
	)
}

func (b *outMsgBuilder) FinalizeVote(
	chainID ids.ID,
	blockHeader simplex.BlockHeader,
	signature simplex.Signature,
) (OutboundMessage, error) {
	return b.builder.createOutbound(
		&p2p.Message{
			Message: &p2p.Message_Simplex{
				Simplex: &p2p.Simplex{
					ChainId: chainID[:],
					Message: &p2p.Simplex_FinalizeVote{
						FinalizeVote: &p2p.Vote{
							Vote: b.simplexMetadataToP2P(blockHeader),
							Signature: &p2p.Signature{
								Signer: signature.Signer[:],
								Value:  signature.Value[:],
							},
						},
					},
				},
			},
		},
		b.compressionType,
		false,
	)
}

func (b *outMsgBuilder) Notarization(
	chainID ids.ID,
	blockHeader simplex.BlockHeader,
	qc []byte,
) (OutboundMessage, error) {
	return b.builder.createOutbound(
		&p2p.Message{
			Message: &p2p.Message_Simplex{
				Simplex: &p2p.Simplex{
					ChainId: chainID[:],
					Message: &p2p.Simplex_Notarization{
						Notarization: &p2p.QuorumCertificate{
							Finalization:      b.simplexMetadataToP2P(blockHeader),
							QuorumCertificate: qc,
						},
					},
				},
			},
		},
		b.compressionType,
		false,
	)
}

func (b *outMsgBuilder) EmptyNotarization(
	chainID ids.ID,
	protocolMetadata simplex.ProtocolMetadata,
	qc []byte,
) (OutboundMessage, error) {
	return b.builder.createOutbound(
		&p2p.Message{
			Message: &p2p.Message_Simplex{
				Simplex: &p2p.Simplex{
					ChainId: chainID[:],
					Message: &p2p.Simplex_EmptyNotarization{
						EmptyNotarization: &p2p.EmptyNotarization{
							EmptyVote:         b.simplexProtocolMetadataToP2p(protocolMetadata),
							QuorumCertificate: qc,
						},
					},
				},
			},
		},
		b.compressionType,
		false,
	)
}

func (b *outMsgBuilder) Finalization(
	chainID ids.ID,
	blockHeader simplex.BlockHeader,
	qc []byte,
) (OutboundMessage, error) {
	return b.builder.createOutbound(
		&p2p.Message{
			Message: &p2p.Message_Simplex{
				Simplex: &p2p.Simplex{
					ChainId: chainID[:],
					Message: &p2p.Simplex_Finalization{
						Finalization: &p2p.QuorumCertificate{
							Finalization:      b.simplexMetadataToP2P(blockHeader),
							QuorumCertificate: qc,
						},
					},
				},
			},
		},
		b.compressionType,
		false,
	)
}

func (b *outMsgBuilder) ReplicationRequest(
	chainID ids.ID,
	seqs []uint64,
	latestRound uint64,
) (OutboundMessage, error) {
	return b.builder.createOutbound(
		&p2p.Message{
			Message: &p2p.Message_Simplex{
				Simplex: &p2p.Simplex{
					ChainId: chainID[:],
					Message: &p2p.Simplex_ReplicationRequest{
						ReplicationRequest: &p2p.ReplicationRequest{
							Seqs:        seqs,
							LatestRound: latestRound,
						},
					},
				},
			},
		},
		b.compressionType,
		false,
	)
}

func (b *outMsgBuilder) VerifiedReplicationResponse(
	chainID ids.ID,
	data []simplex.VerifiedQuorumRound,
	latestRound *simplex.VerifiedQuorumRound,
) (OutboundMessage, error) {
	var qrs []*p2p.QuorumRound
	for _, qr := range data {
		qrs = append(qrs, b.simplexQuorumRoundToP2P(&qr))
	}

	return b.builder.createOutbound(
		&p2p.Message{
			Message: &p2p.Message_Simplex{
				Simplex: &p2p.Simplex{
					ChainId: chainID[:],
					Message: &p2p.Simplex_ReplicationResponse{
						ReplicationResponse: &p2p.ReplicationResponse{
							Data:        qrs,
							LatestRound: b.simplexQuorumRoundToP2P(latestRound),
						},
					},
				},
			},
		},
		b.compressionType,
		false,
	)
}

func (b *outMsgBuilder) simplexMetadataToP2P(bh simplex.BlockHeader) *p2p.BlockHeader {
	return &p2p.BlockHeader{
		Metadata: b.simplexProtocolMetadataToP2p(bh.ProtocolMetadata),
		Digest:   bh.Digest[:],
	}
}

func (b *outMsgBuilder) simplexProtocolMetadataToP2p(md simplex.ProtocolMetadata) *p2p.ProtocolMetadata {
	return &p2p.ProtocolMetadata{
		Version: uint32(md.Version),
		Epoch:   md.Epoch,
		Round:   md.Round,
		Seq:     md.Seq,
		Prev:    md.Prev[:],
	}
}

func (b *outMsgBuilder) simplexQuorumRoundToP2P(qr *simplex.VerifiedQuorumRound) *p2p.QuorumRound {
	var p2pQR *p2p.QuorumRound
	if qr.VerifiedBlock != nil {
		p2pQR.Block = qr.VerifiedBlock.Bytes()
	}
	if qr.Notarization != nil {
		p2pQR.Notarization = &p2p.QuorumCertificate{
			Finalization:      b.simplexMetadataToP2P(qr.Notarization.Vote.BlockHeader),
			QuorumCertificate: qr.Notarization.QC.Bytes(),
		}
	}
	if qr.Finalization != nil {
		p2pQR.Finalization = &p2p.QuorumCertificate{
			Finalization:      b.simplexMetadataToP2P(qr.Finalization.Finalization.BlockHeader),
			QuorumCertificate: qr.Finalization.QC.Bytes(),
		}
	}
	if qr.EmptyNotarization != nil {
		p2pQR.EmptyNotarization = &p2p.EmptyNotarization{
			EmptyVote:         b.simplexProtocolMetadataToP2p(qr.EmptyNotarization.Vote.ProtocolMetadata),
			QuorumCertificate: qr.EmptyNotarization.QC.Bytes(),
		}
	}
	return p2pQR
}

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
								Vote: &p2p.BlockHeader{
									Metadata: &p2p.ProtocolMetadata{
										Version: uint32(vote.Vote.Version),
										Epoch:   vote.Vote.Epoch,
										Round:   vote.Vote.Round,
										Seq:     vote.Vote.Seq,
										Prev:    vote.Vote.Prev[:],
									},
									Digest: vote.Vote.Digest[:],
								},
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
							Vote: &p2p.BlockHeader{
								Metadata: &p2p.ProtocolMetadata{
									Version: uint32(blockHeader.Version),
									Epoch:   blockHeader.Epoch,
									Round:   blockHeader.Round,
									Seq:     blockHeader.Seq,
									Prev:    blockHeader.Prev[:],
								},
								Digest: blockHeader.Digest[:],
							},
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
							Vote: &p2p.ProtocolMetadata{
								Version: uint32(protocolMetadata.Version),
								Epoch:   protocolMetadata.Epoch,
								Round:   protocolMetadata.Round,
								Seq:     protocolMetadata.Seq,
								Prev:    protocolMetadata.Prev[:],
							},
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
							Vote: &p2p.BlockHeader{
								Metadata: &p2p.ProtocolMetadata{
									Version: uint32(blockHeader.Version),
									Epoch:   blockHeader.Epoch,
									Round:   blockHeader.Round,
									Seq:     blockHeader.Seq,
									Prev:    blockHeader.Prev[:],
								},
								Digest: blockHeader.Digest[:],
							},
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
							Finalization: &p2p.BlockHeader{
								Metadata: &p2p.ProtocolMetadata{
									Version: uint32(blockHeader.Version),
									Epoch:   blockHeader.Epoch,
									Round:   blockHeader.Round,
									Seq:     blockHeader.Seq,
									Prev:    blockHeader.Prev[:],
								},
								Digest: blockHeader.Digest[:],
							},
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
							EmptyVote: &p2p.ProtocolMetadata{
								Version: uint32(protocolMetadata.Version),
								Epoch:   protocolMetadata.Epoch,
								Round:   protocolMetadata.Round,
								Seq:     protocolMetadata.Seq,
								Prev:    protocolMetadata.Prev[:],
							},
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
							Finalization: &p2p.BlockHeader{
								Metadata: &p2p.ProtocolMetadata{
									Version: uint32(blockHeader.Version),
									Epoch:   blockHeader.Epoch,
									Round:   blockHeader.Round,
									Seq:     blockHeader.Seq,
									Prev:    blockHeader.Prev[:],
								},
								Digest: blockHeader.Digest[:],
							},
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

// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package simplex

import (
	"github.com/ava-labs/simplex"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/proto/pb/p2p"
)

func newP2PSimplexBlockProposal(
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
					BlockHeader: simplexBlockheaderToP2P(vote.Vote.BlockHeader),
					Signature: &p2p.Signature{
						Signer: vote.Signature.Signer,
						Value:  vote.Signature.Value,
					},
				},
			},
		},
	}
}

func newP2PSimplexVote(
	chainID ids.ID,
	blockHeader simplex.BlockHeader,
	signature simplex.Signature,
) *p2p.Simplex {
	return &p2p.Simplex{
		ChainId: chainID[:],
		Message: &p2p.Simplex_Vote{
			Vote: &p2p.Vote{
				BlockHeader: simplexBlockheaderToP2P(blockHeader),
				Signature: &p2p.Signature{
					Signer: signature.Signer,
					Value:  signature.Value,
				},
			},
		},
	}
}

func newP2PSimplexEmptyVote(
	chainID ids.ID,
	metadata simplex.ProtocolMetadata,
	signature simplex.Signature,
) *p2p.Simplex {
	return &p2p.Simplex{
		ChainId: chainID[:],
		Message: &p2p.Simplex_EmptyVote{
			EmptyVote: &p2p.EmptyVote{
				Metadata: simplexProtocolMetadataToP2P(metadata),
				Signature: &p2p.Signature{
					Signer: signature.Signer,
					Value:  signature.Value,
				},
			},
		},
	}
}

func newP2PSimplexFinalizeVote(
	chainID ids.ID,
	blockHeader simplex.BlockHeader,
	signature simplex.Signature,
) *p2p.Simplex {
	return &p2p.Simplex{
		ChainId: chainID[:],
		Message: &p2p.Simplex_FinalizeVote{
			FinalizeVote: &p2p.Vote{
				BlockHeader: simplexBlockheaderToP2P(blockHeader),
				Signature: &p2p.Signature{
					Signer: signature.Signer,
					Value:  signature.Value,
				},
			},
		},
	}
}

func newP2PSimplexNotarization(
	chainID ids.ID,
	blockHeader simplex.BlockHeader,
	qc []byte,
) *p2p.Simplex {
	return &p2p.Simplex{
		ChainId: chainID[:],
		Message: &p2p.Simplex_Notarization{
			Notarization: &p2p.QuorumCertificate{
				BlockHeader:       simplexBlockheaderToP2P(blockHeader),
				QuorumCertificate: qc,
			},
		},
	}
}

func newP2PSimplexEmptyNotarization(
	chainID ids.ID,
	metadata simplex.ProtocolMetadata,
	qc []byte,
) *p2p.Simplex {
	return &p2p.Simplex{
		ChainId: chainID[:],
		Message: &p2p.Simplex_EmptyNotarization{
			EmptyNotarization: &p2p.EmptyNotarization{
				Metadata:          simplexProtocolMetadataToP2P(metadata),
				QuorumCertificate: qc,
			},
		},
	}
}

func newP2PSimplexFinalization(
	chainID ids.ID,
	blockHeader simplex.BlockHeader,
	qc []byte,
) *p2p.Simplex {
	return &p2p.Simplex{
		ChainId: chainID[:],
		Message: &p2p.Simplex_Finalization{
			Finalization: &p2p.QuorumCertificate{
				BlockHeader:       simplexBlockheaderToP2P(blockHeader),
				QuorumCertificate: qc,
			},
		},
	}
}

func newP2PSimplexReplicationRequest(
	chainID ids.ID,
	seqs []uint64,
	latestRound uint64,
) *p2p.Simplex {
	return &p2p.Simplex{
		ChainId: chainID[:],
		Message: &p2p.Simplex_ReplicationRequest{
			ReplicationRequest: &p2p.ReplicationRequest{
				Seqs:        seqs,
				LatestRound: latestRound,
			},
		},
	}
}

func newP2PSimplexReplicationResponse(
	chainID ids.ID,
	data []simplex.VerifiedQuorumRound,
	latestRound *simplex.VerifiedQuorumRound,
) (*p2p.Simplex, error) {
	qrs := make([]*p2p.QuorumRound, 0, len(data))
	for _, qr := range data {
		p2pQR, err := simplexQuorumRoundToP2P(&qr)
		if err != nil {
			return nil, err
		}
		qrs = append(qrs, p2pQR)
	}

	latestQR, err := simplexQuorumRoundToP2P(latestRound)
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

func simplexBlockheaderToP2P(bh simplex.BlockHeader) *p2p.BlockHeader {
	return &p2p.BlockHeader{
		Metadata: simplexProtocolMetadataToP2P(bh.ProtocolMetadata),
		Digest:   bh.Digest[:],
	}
}

func simplexProtocolMetadataToP2P(md simplex.ProtocolMetadata) *p2p.ProtocolMetadata {
	return &p2p.ProtocolMetadata{
		Version: uint32(md.Version),
		Epoch:   md.Epoch,
		Round:   md.Round,
		Seq:     md.Seq,
		Prev:    md.Prev[:],
	}
}

func simplexQuorumRoundToP2P(qr *simplex.VerifiedQuorumRound) (*p2p.QuorumRound, error) {
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
			BlockHeader:       simplexBlockheaderToP2P(qr.Notarization.Vote.BlockHeader),
			QuorumCertificate: qr.Notarization.QC.Bytes(),
		}
	}
	if qr.Finalization != nil {
		p2pQR.Finalization = &p2p.QuorumCertificate{
			BlockHeader:       simplexBlockheaderToP2P(qr.Finalization.Finalization.BlockHeader),
			QuorumCertificate: qr.Finalization.QC.Bytes(),
		}
	}
	if qr.EmptyNotarization != nil {
		p2pQR.EmptyNotarization = &p2p.EmptyNotarization{
			Metadata:          simplexProtocolMetadataToP2P(qr.EmptyNotarization.Vote.ProtocolMetadata),
			QuorumCertificate: qr.EmptyNotarization.QC.Bytes(),
		}
	}
	return p2pQR, nil
}

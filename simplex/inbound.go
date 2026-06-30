// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package simplex

import (
	"context"
	"errors"
	"fmt"
	"math"

	"github.com/ava-labs/simplex/common"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/proto/pb/p2p"
)

var (
	errNilField            = errors.New("nil field")
	errInvalidDigestLength = errors.New("invalid digest length")
	errInvalidSigner       = errors.New("invalid signer")
)

func emptyNotarizationMessageFromP2P(emptyNotarization *p2p.EmptyNotarization, qcDeserializer *QCDeserializer) (*common.Message, error) {
	notarization, err := emptyNotarizationFromP2P(emptyNotarization, qcDeserializer)
	if err != nil {
		return nil, fmt.Errorf("failed to convert empty notarization: %w", err)
	}

	return &common.Message{
		EmptyNotarization: notarization,
	}, nil
}

func notarizationMessageFromP2P(notarization *p2p.QuorumCertificate, qcDeserializer *QCDeserializer) (*common.Message, error) {
	note, err := notarizationFromP2P(notarization, qcDeserializer)
	if err != nil {
		return nil, fmt.Errorf("failed to convert notarization: %w", err)
	}

	return &common.Message{
		Notarization: note,
	}, nil
}

func finalizationMessageFromP2P(finalization *p2p.QuorumCertificate, qcDeserializer *QCDeserializer) (*common.Message, error) {
	finalizationMsg, err := finalizationFromP2P(finalization, qcDeserializer)
	if err != nil {
		return nil, fmt.Errorf("failed to convert finalization: %w", err)
	}

	return &common.Message{
		Finalization: finalizationMsg,
	}, nil
}

func blockProposalFromP2P(ctx context.Context, blockProposal *p2p.BlockProposal, deserializer *blockDeserializer) (*common.Message, error) {
	block, err := deserializer.DeserializeBlock(ctx, blockProposal.Block)
	if err != nil {
		return nil, fmt.Errorf("failed to deserialize block: %w", err)
	}

	vote, err := p2pVoteToSimplexVote(blockProposal.Vote)
	if err != nil {
		return nil, fmt.Errorf("failed to deserialize vote: %w", err)
	}

	return &common.Message{
		BlockMessage: &common.BlockMessage{
			Block: block,
			Vote:  vote,
		},
	}, nil
}

func voteFromP2P(vote *p2p.Vote) (*common.Message, error) {
	simplexVote, err := p2pVoteToSimplexVote(vote)
	if err != nil {
		return nil, fmt.Errorf("failed to convert p2p vote to simplex vote: %w", err)
	}
	return &common.Message{
		VoteMessage: &simplexVote,
	}, nil
}

func emptyVoteFromP2P(emptyVote *p2p.EmptyVote) (*common.Message, error) {
	vote, err := emptyVoteMetadataFromP2P(emptyVote.Metadata)
	if err != nil {
		return nil, err
	}

	sig, err := p2pSignatureToSimplexSignature(emptyVote.Signature)
	if err != nil {
		return nil, err
	}

	return &common.Message{
		EmptyVoteMessage: &common.EmptyVote{
			Vote: common.ToBeSignedEmptyVote{
				EmptyVoteMetadata: vote,
			},
			Signature: sig,
		},
	}, nil
}

func finalizeVoteFromP2P(finalizeVote *p2p.Vote) (*common.Message, error) {
	bh, err := p2pBlockHeaderToSimplexBlockHeader(finalizeVote.BlockHeader)
	if err != nil {
		return nil, err
	}

	sig, err := p2pSignatureToSimplexSignature(finalizeVote.Signature)
	if err != nil {
		return nil, err
	}

	return &common.Message{
		FinalizeVote: &common.FinalizeVote{
			Finalization: common.ToBeSignedFinalization{
				BlockHeader: bh,
			},
			Signature: sig,
		},
	}, nil
}

func replicationRequestFromP2P(replicationRequest *p2p.ReplicationRequest) *common.Message {
	return &common.Message{
		ReplicationRequest: &common.ReplicationRequest{
			Seqs:        replicationRequest.Seqs,
			LatestRound: replicationRequest.LatestRound,
		},
	}
}

func replicationResponseFromP2P(ctx context.Context, replicationResponse *p2p.ReplicationResponse, blockDeserializer *blockDeserializer, qcDeserializer *QCDeserializer) (*common.Message, error) {
	latestRound, err := quorumRoundFromP2P(ctx, replicationResponse.LatestRound, blockDeserializer, qcDeserializer)
	if err != nil {
		return nil, err
	}

	data := make([]common.QuorumRound, 0, len(replicationResponse.Data))
	for _, qr := range replicationResponse.Data {
		converted, err := quorumRoundFromP2P(ctx, qr, blockDeserializer, qcDeserializer)
		if err != nil {
			return nil, err
		}
		data = append(data, *converted)
	}

	return &common.Message{
		ReplicationResponse: &common.ReplicationResponse{
			LatestRound: latestRound,
			Data:        data,
		},
	}, nil
}

// HELPERS -----------------
func p2pVoteToSimplexVote(p2pVote *p2p.Vote) (common.Vote, error) {
	if p2pVote == nil {
		return common.Vote{}, errNilField
	}

	bh, err := p2pBlockHeaderToSimplexBlockHeader(p2pVote.BlockHeader)
	if err != nil {
		return common.Vote{}, err
	}

	signature, err := p2pSignatureToSimplexSignature(p2pVote.Signature)
	if err != nil {
		return common.Vote{}, err
	}

	v := common.Vote{
		Vote: common.ToBeSignedVote{
			BlockHeader: bh,
		},
		Signature: signature,
	}

	return v, nil
}

func p2pSignatureToSimplexSignature(p2pSig *p2p.Signature) (common.Signature, error) {
	if p2pSig == nil {
		return common.Signature{}, errNilField
	}

	nodeID, err := ids.ToNodeID(p2pSig.Signer)
	if err != nil {
		return common.Signature{}, fmt.Errorf("%w: %w", errInvalidSigner, err)
	}

	return common.Signature{
		Signer: nodeID[:],
		Value:  p2pSig.Value,
	}, nil
}

func p2pBlockHeaderToSimplexBlockHeader(p2pHeader *p2p.BlockHeader) (common.BlockHeader, error) {
	if p2pHeader == nil {
		return common.BlockHeader{}, errNilField
	}

	md, err := p2pMetadataToSimplexMetadata(p2pHeader.Metadata)
	if err != nil {
		return common.BlockHeader{}, fmt.Errorf("failed to convert previous metadata: %w", err)
	}

	digest, err := digestFromP2P(p2pHeader.Digest)
	if err != nil {
		return common.BlockHeader{}, fmt.Errorf("failed to convert digest: %w", err)
	}

	return common.BlockHeader{
		ProtocolMetadata: md,
		Digest:           digest,
	}, nil
}

func p2pMetadataToSimplexMetadata(p2pMetadata *p2p.ProtocolMetadata) (common.ProtocolMetadata, error) {
	if p2pMetadata == nil {
		return common.ProtocolMetadata{}, errNilField
	}

	if p2pMetadata.Version > math.MaxUint8 {
		return common.ProtocolMetadata{}, fmt.Errorf("version %d exceeds maximum value %d", p2pMetadata.Version, math.MaxUint8)
	}
	prev, err := digestFromP2P(p2pMetadata.Prev)
	if err != nil {
		return common.ProtocolMetadata{}, err
	}

	return common.ProtocolMetadata{
		Version: uint8(p2pMetadata.Version),
		Epoch:   p2pMetadata.Epoch,
		Round:   p2pMetadata.Round,
		Seq:     p2pMetadata.Seq,
		Prev:    prev,
	}, nil
}

func emptyVoteMetadataFromP2P(emptyVote *p2p.EmptyVoteMetadata) (common.EmptyVoteMetadata, error) {
	if emptyVote == nil {
		return common.EmptyVoteMetadata{}, errNilField
	}

	return common.EmptyVoteMetadata{
		Round: emptyVote.Round,
		Epoch: emptyVote.Epoch,
	}, nil
}

func digestFromP2P(p2pDigest []byte) (common.Digest, error) {
	if len(p2pDigest) != 32 {
		return common.Digest{}, fmt.Errorf("%w: got %d, expected %d", errInvalidDigestLength, len(p2pDigest), 32)
	}

	var digest common.Digest
	copy(digest[:], p2pDigest)
	return digest, nil
}

func quorumCertificateFromP2P(qcBytes []byte, qcDeserializer *QCDeserializer) (common.QuorumCertificate, error) {
	if qcBytes == nil {
		return nil, errNilField
	}

	simplexQC, err := qcDeserializer.DeserializeQuorumCertificate(qcBytes)
	if err != nil {
		return nil, err
	}

	return simplexQC, nil
}

func notarizationFromP2P(notarization *p2p.QuorumCertificate, qcDeserializer *QCDeserializer) (*common.Notarization, error) {
	bh, err := p2pBlockHeaderToSimplexBlockHeader(notarization.BlockHeader)
	if err != nil {
		return nil, err
	}

	qc, err := quorumCertificateFromP2P(notarization.QuorumCertificate, qcDeserializer)
	if err != nil {
		return nil, fmt.Errorf("failed to convert quorum certificate: %w", err)
	}

	return &common.Notarization{
		Vote: common.ToBeSignedVote{
			BlockHeader: bh,
		},
		QC: qc,
	}, nil
}

func emptyNotarizationFromP2P(emptyNotarization *p2p.EmptyNotarization, qcDeserializer *QCDeserializer) (*common.EmptyNotarization, error) {
	if emptyNotarization == nil {
		return nil, errNilField
	}

	md, err := emptyVoteMetadataFromP2P(emptyNotarization.Metadata)
	if err != nil {
		return nil, fmt.Errorf("failed to convert metadata: %w", err)
	}

	qc, err := quorumCertificateFromP2P(emptyNotarization.QuorumCertificate, qcDeserializer)
	if err != nil {
		return nil, fmt.Errorf("failed to convert quorum certificate: %w", err)
	}

	return &common.EmptyNotarization{
		Vote: common.ToBeSignedEmptyVote{
			EmptyVoteMetadata: md,
		},
		QC: qc,
	}, nil
}

func finalizationFromP2P(finalization *p2p.QuorumCertificate, qcDeserializer *QCDeserializer) (*common.Finalization, error) {
	bh, err := p2pBlockHeaderToSimplexBlockHeader(finalization.BlockHeader)
	if err != nil {
		return nil, err
	}

	qc, err := quorumCertificateFromP2P(finalization.QuorumCertificate, qcDeserializer)
	if err != nil {
		return nil, fmt.Errorf("failed to convert quorum certificate: %w", err)
	}

	return &common.Finalization{
		Finalization: common.ToBeSignedFinalization{
			BlockHeader: bh,
		},
		QC: qc,
	}, nil
}

func quorumRoundFromP2P(ctx context.Context, qr *p2p.QuorumRound, blockDeserializer *blockDeserializer, qcDeserializer *QCDeserializer) (*common.QuorumRound, error) {
	if qr == nil {
		return nil, errNilField
	}

	var block common.Block
	if qr.Block != nil {
		dBlock, err := blockDeserializer.DeserializeBlock(ctx, qr.Block)
		if err != nil {
			return nil, fmt.Errorf("failed to convert block: %w", err)
		}
		block = dBlock
	}

	var emptyNotarization *common.EmptyNotarization
	if qr.EmptyNotarization != nil {
		eNote, err := emptyNotarizationFromP2P(qr.EmptyNotarization, qcDeserializer)
		if err != nil {
			return nil, fmt.Errorf("failed to convert empty notarization: %w", err)
		}
		emptyNotarization = eNote
	}

	var notarization *common.Notarization
	if qr.Notarization != nil {
		note, err := notarizationFromP2P(qr.Notarization, qcDeserializer)
		if err != nil {
			return nil, fmt.Errorf("failed to convert notarization: %w", err)
		}
		notarization = note
	}

	var finalization *common.Finalization
	if qr.Finalization != nil {
		finalize, err := finalizationFromP2P(qr.Finalization, qcDeserializer)
		if err != nil {
			return nil, fmt.Errorf("failed to convert finalization: %w", err)
		}
		finalization = finalize
	}

	return &common.QuorumRound{
		Block:             block,
		EmptyNotarization: emptyNotarization,
		Notarization:      notarization,
		Finalization:      finalization,
	}, nil
}

// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package simplex

import (
	"context"
	"errors"
	"fmt"
	"math"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/proto/pb/p2p"

	simplexcommon "github.com/ava-labs/simplex/common"
)

var (
	errNilField            = errors.New("nil field")
	errInvalidDigestLength = errors.New("invalid digest length")
	errInvalidSigner       = errors.New("invalid signer")
)

func emptyNotarizationMessageFromP2P(emptyNotarization *p2p.EmptyNotarization, qcDeserializer *QCDeserializer) (*simplexcommon.Message, error) {
	notarization, err := emptyNotarizationFromP2P(emptyNotarization, qcDeserializer)
	if err != nil {
		return nil, fmt.Errorf("failed to convert empty notarization: %w", err)
	}

	return &simplexcommon.Message{
		EmptyNotarization: notarization,
	}, nil
}

func notarizationMessageFromP2P(notarization *p2p.QuorumCertificate, qcDeserializer *QCDeserializer) (*simplexcommon.Message, error) {
	note, err := notarizationFromP2P(notarization, qcDeserializer)
	if err != nil {
		return nil, fmt.Errorf("failed to convert notarization: %w", err)
	}

	return &simplexcommon.Message{
		Notarization: note,
	}, nil
}

func finalizationMessageFromP2P(finalization *p2p.QuorumCertificate, qcDeserializer *QCDeserializer) (*simplexcommon.Message, error) {
	finalizationMsg, err := finalizationFromP2P(finalization, qcDeserializer)
	if err != nil {
		return nil, fmt.Errorf("failed to convert finalization: %w", err)
	}

	return &simplexcommon.Message{
		Finalization: finalizationMsg,
	}, nil
}

func blockProposalFromP2P(ctx context.Context, blockProposal *p2p.BlockProposal, deserializer *blockDeserializer) (*simplexcommon.Message, error) {
	block, err := deserializer.DeserializeBlock(ctx, blockProposal.Block)
	if err != nil {
		return nil, fmt.Errorf("failed to deserialize block: %w", err)
	}

	vote, err := p2pVoteToSimplexVote(blockProposal.Vote)
	if err != nil {
		return nil, fmt.Errorf("failed to deserialize vote: %w", err)
	}

	return &simplexcommon.Message{
		BlockMessage: &simplexcommon.BlockMessage{
			Block: block,
			Vote:  vote,
		},
	}, nil
}

func voteFromP2P(vote *p2p.Vote) (*simplexcommon.Message, error) {
	simplexVote, err := p2pVoteToSimplexVote(vote)
	if err != nil {
		return nil, fmt.Errorf("failed to convert p2p vote to simplex vote: %w", err)
	}
	return &simplexcommon.Message{
		VoteMessage: &simplexVote,
	}, nil
}

func emptyVoteFromP2P(emptyVote *p2p.EmptyVote) (*simplexcommon.Message, error) {
	vote, err := emptyVoteMetadataFromP2P(emptyVote.Metadata)
	if err != nil {
		return nil, err
	}

	sig, err := p2pSignatureToSimplexSignature(emptyVote.Signature)
	if err != nil {
		return nil, err
	}

	return &simplexcommon.Message{
		EmptyVoteMessage: &simplexcommon.EmptyVote{
			Vote: simplexcommon.ToBeSignedEmptyVote{
				EmptyVoteMetadata: vote,
			},
			Signature: sig,
		},
	}, nil
}

func finalizeVoteFromP2P(finalizeVote *p2p.Vote) (*simplexcommon.Message, error) {
	bh, err := p2pBlockHeaderToSimplexBlockHeader(finalizeVote.BlockHeader)
	if err != nil {
		return nil, err
	}

	sig, err := p2pSignatureToSimplexSignature(finalizeVote.Signature)
	if err != nil {
		return nil, err
	}

	return &simplexcommon.Message{
		FinalizeVote: &simplexcommon.FinalizeVote{
			Finalization: simplexcommon.ToBeSignedFinalization{
				BlockHeader: bh,
			},
			Signature: sig,
		},
	}, nil
}

func replicationRequestFromP2P(replicationRequest *p2p.ReplicationRequest) *simplexcommon.Message {
	return &simplexcommon.Message{
		ReplicationRequest: &simplexcommon.ReplicationRequest{
			Seqs:        replicationRequest.Seqs,
			LatestRound: replicationRequest.LatestRound,
		},
	}
}

func replicationResponseFromP2P(ctx context.Context, replicationResponse *p2p.ReplicationResponse, blockDeserializer *blockDeserializer, qcDeserializer *QCDeserializer) (*simplexcommon.Message, error) {
	latestRound, err := quorumRoundFromP2P(ctx, replicationResponse.LatestRound, blockDeserializer, qcDeserializer)
	if err != nil {
		return nil, err
	}

	data := make([]simplexcommon.QuorumRound, 0, len(replicationResponse.Data))
	for _, qr := range replicationResponse.Data {
		converted, err := quorumRoundFromP2P(ctx, qr, blockDeserializer, qcDeserializer)
		if err != nil {
			return nil, err
		}
		data = append(data, *converted)
	}

	return &simplexcommon.Message{
		ReplicationResponse: &simplexcommon.ReplicationResponse{
			LatestRound: latestRound,
			Data:        data,
		},
	}, nil
}

// HELPERS -----------------
func p2pVoteToSimplexVote(p2pVote *p2p.Vote) (simplexcommon.Vote, error) {
	if p2pVote == nil {
		return simplexcommon.Vote{}, errNilField
	}

	bh, err := p2pBlockHeaderToSimplexBlockHeader(p2pVote.BlockHeader)
	if err != nil {
		return simplexcommon.Vote{}, err
	}

	signature, err := p2pSignatureToSimplexSignature(p2pVote.Signature)
	if err != nil {
		return simplexcommon.Vote{}, err
	}

	v := simplexcommon.Vote{
		Vote: simplexcommon.ToBeSignedVote{
			BlockHeader: bh,
		},
		Signature: signature,
	}

	return v, nil
}

func p2pSignatureToSimplexSignature(p2pSig *p2p.Signature) (simplexcommon.Signature, error) {
	if p2pSig == nil {
		return simplexcommon.Signature{}, errNilField
	}

	nodeID, err := ids.ToNodeID(p2pSig.Signer)
	if err != nil {
		return simplexcommon.Signature{}, fmt.Errorf("%w: %w", errInvalidSigner, err)
	}

	return simplexcommon.Signature{
		Signer: nodeID[:],
		Value:  p2pSig.Value,
	}, nil
}

func p2pBlockHeaderToSimplexBlockHeader(p2pHeader *p2p.BlockHeader) (simplexcommon.BlockHeader, error) {
	if p2pHeader == nil {
		return simplexcommon.BlockHeader{}, errNilField
	}

	md, err := p2pMetadataToSimplexMetadata(p2pHeader.Metadata)
	if err != nil {
		return simplexcommon.BlockHeader{}, fmt.Errorf("failed to convert previous metadata: %w", err)
	}

	digest, err := digestFromP2P(p2pHeader.Digest)
	if err != nil {
		return simplexcommon.BlockHeader{}, fmt.Errorf("failed to convert digest: %w", err)
	}

	return simplexcommon.BlockHeader{
		ProtocolMetadata: md,
		Digest:           digest,
	}, nil
}

func p2pMetadataToSimplexMetadata(p2pMetadata *p2p.ProtocolMetadata) (simplexcommon.ProtocolMetadata, error) {
	if p2pMetadata == nil {
		return simplexcommon.ProtocolMetadata{}, errNilField
	}

	if p2pMetadata.Version > math.MaxUint8 {
		return simplexcommon.ProtocolMetadata{}, fmt.Errorf("version %d exceeds maximum value %d", p2pMetadata.Version, math.MaxUint8)
	}
	prev, err := digestFromP2P(p2pMetadata.Prev)
	if err != nil {
		return simplexcommon.ProtocolMetadata{}, err
	}

	return simplexcommon.ProtocolMetadata{
		Version: uint8(p2pMetadata.Version),
		Epoch:   p2pMetadata.Epoch,
		Round:   p2pMetadata.Round,
		Seq:     p2pMetadata.Seq,
		Prev:    prev,
	}, nil
}

func emptyVoteMetadataFromP2P(emptyVote *p2p.EmptyVoteMetadata) (simplexcommon.EmptyVoteMetadata, error) {
	if emptyVote == nil {
		return simplexcommon.EmptyVoteMetadata{}, errNilField
	}

	return simplexcommon.EmptyVoteMetadata{
		Round: emptyVote.Round,
		Epoch: emptyVote.Epoch,
	}, nil
}

func digestFromP2P(p2pDigest []byte) (simplexcommon.Digest, error) {
	if len(p2pDigest) != 32 {
		return simplexcommon.Digest{}, fmt.Errorf("%w: got %d, expected %d", errInvalidDigestLength, len(p2pDigest), 32)
	}

	var digest simplexcommon.Digest
	copy(digest[:], p2pDigest)
	return digest, nil
}

func quorumCertificateFromP2P(qcBytes []byte, qcDeserializer *QCDeserializer) (simplexcommon.QuorumCertificate, error) {
	if qcBytes == nil {
		return nil, errNilField
	}

	simplexQC, err := qcDeserializer.DeserializeQuorumCertificate(qcBytes)
	if err != nil {
		return nil, err
	}

	return simplexQC, nil
}

func notarizationFromP2P(notarization *p2p.QuorumCertificate, qcDeserializer *QCDeserializer) (*simplexcommon.Notarization, error) {
	bh, err := p2pBlockHeaderToSimplexBlockHeader(notarization.BlockHeader)
	if err != nil {
		return nil, err
	}

	qc, err := quorumCertificateFromP2P(notarization.QuorumCertificate, qcDeserializer)
	if err != nil {
		return nil, fmt.Errorf("failed to convert quorum certificate: %w", err)
	}

	return &simplexcommon.Notarization{
		Vote: simplexcommon.ToBeSignedVote{
			BlockHeader: bh,
		},
		QC: qc,
	}, nil
}

func emptyNotarizationFromP2P(emptyNotarization *p2p.EmptyNotarization, qcDeserializer *QCDeserializer) (*simplexcommon.EmptyNotarization, error) {
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

	return &simplexcommon.EmptyNotarization{
		Vote: simplexcommon.ToBeSignedEmptyVote{
			EmptyVoteMetadata: md,
		},
		QC: qc,
	}, nil
}

func finalizationFromP2P(finalization *p2p.QuorumCertificate, qcDeserializer *QCDeserializer) (*simplexcommon.Finalization, error) {
	bh, err := p2pBlockHeaderToSimplexBlockHeader(finalization.BlockHeader)
	if err != nil {
		return nil, err
	}

	qc, err := quorumCertificateFromP2P(finalization.QuorumCertificate, qcDeserializer)
	if err != nil {
		return nil, fmt.Errorf("failed to convert quorum certificate: %w", err)
	}

	return &simplexcommon.Finalization{
		Finalization: simplexcommon.ToBeSignedFinalization{
			BlockHeader: bh,
		},
		QC: qc,
	}, nil
}

func quorumRoundFromP2P(ctx context.Context, qr *p2p.QuorumRound, blockDeserializer *blockDeserializer, qcDeserializer *QCDeserializer) (*simplexcommon.QuorumRound, error) {
	if qr == nil {
		return nil, errNilField
	}

	var block simplexcommon.Block
	if qr.Block != nil {
		dBlock, err := blockDeserializer.DeserializeBlock(ctx, qr.Block)
		if err != nil {
			return nil, fmt.Errorf("failed to convert block: %w", err)
		}
		block = dBlock
	}

	var emptyNotarization *simplexcommon.EmptyNotarization
	if qr.EmptyNotarization != nil {
		eNote, err := emptyNotarizationFromP2P(qr.EmptyNotarization, qcDeserializer)
		if err != nil {
			return nil, fmt.Errorf("failed to convert empty notarization: %w", err)
		}
		emptyNotarization = eNote
	}

	var notarization *simplexcommon.Notarization
	if qr.Notarization != nil {
		note, err := notarizationFromP2P(qr.Notarization, qcDeserializer)
		if err != nil {
			return nil, fmt.Errorf("failed to convert notarization: %w", err)
		}
		notarization = note
	}

	var finalization *simplexcommon.Finalization
	if qr.Finalization != nil {
		finalize, err := finalizationFromP2P(qr.Finalization, qcDeserializer)
		if err != nil {
			return nil, fmt.Errorf("failed to convert finalization: %w", err)
		}
		finalization = finalize
	}

	return &simplexcommon.QuorumRound{
		Block:             block,
		EmptyNotarization: emptyNotarization,
		Notarization:      notarization,
		Finalization:      finalization,
	}, nil
}

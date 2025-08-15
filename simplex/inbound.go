package simplex

import (
	"context"
	"errors"
	"fmt"
	"math"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/proto/pb/p2p"
	"github.com/ava-labs/simplex"
)

var errNilField = errors.New("nil field")


// -> MESSAGES 
func emptyNotarizationFromP2P(emptyNotarization *p2p.EmptyNotarization, qcDeserializer *QCDeserializer) (*simplex.Message, error) {
	md, err := emptyVoteMetadataFromP2P(emptyNotarization.Metadata)
	if err != nil {
		return nil, fmt.Errorf("failed to convert metadata: %w", err)
	}

	qc, err := quorumCertificateFromP2P(emptyNotarization.QuorumCertificate, qcDeserializer)
	if err != nil {
		return nil, fmt.Errorf("failed to convert quorum certificate: %w", err)
	}

	return &simplex.Message{
		EmptyNotarization: &simplex.EmptyNotarization{
			Vote: simplex.ToBeSignedEmptyVote{
				EmptyVoteMetadata: md,
			},
			QC: qc,
		},
	}, nil
}

func blockProposalFromP2P(ctx context.Context, blockProposal *p2p.BlockProposal, deserializer *blockDeserializer) (*simplex.Message, error) {
	block, err := deserializer.DeserializeBlock(ctx, blockProposal.Block)
	if err != nil {
		return nil, fmt.Errorf("failed to deserialize block: %w", err)
	}

	vote, err := p2pVoteToSimplexVote(blockProposal.Vote)
	if err != nil {
		return nil, fmt.Errorf("failed to deserialize vote: %w", err)
	}

	return &simplex.Message{
		BlockMessage: &simplex.BlockMessage{
			Block: block,
			Vote:  vote,
		},
	}, nil
}

func voteFromP2P(vote *p2p.Vote) (*simplex.Message, error) {
	simplexVote, err := p2pVoteToSimplexVote(vote)
	if err != nil {
		return nil, fmt.Errorf("failed to convert p2p vote to simplex vote: %w", err)
	}
	return &simplex.Message{
		VoteMessage: &simplexVote,
	}, nil
}

func emptyVoteFromP2P(emptyVote *p2p.EmptyVote) (*simplex.Message, error) {
	vote, err := emptyVoteMetadataFromP2P(emptyVote.Metadata)
	if err != nil {
		return nil, err
	}

	sig, err := p2pSignatureToSimplexSignature(emptyVote.Signature)
	if err != nil {
		return nil, err
	}

	return &simplex.Message{
		EmptyVoteMessage: &simplex.EmptyVote{
			Vote: simplex.ToBeSignedEmptyVote{
				EmptyVoteMetadata: vote,
			},
			Signature: sig,
		},
	}, nil
}

func notarizationFromP2P(notarization *p2p.QuorumCertificate, qcDeserializer *QCDeserializer) (*simplex.Message, error) {
	bh, err := p2pBlockHeaderToSimplexBlockHeader(notarization.BlockHeader)
	if err != nil {
		return nil, err
	}

	qc, err := quorumCertificateFromP2P(notarization.QuorumCertificate, qcDeserializer)
	if err != nil {
		return nil, fmt.Errorf("failed to convert quorum certificate: %w", err)
	}

	return &simplex.Message{
		Notarization: &simplex.Notarization{
			Vote: simplex.ToBeSignedVote{
				BlockHeader: bh,
			},
			QC: qc,
		},
	}, nil
}

func finalizationFromP2P(finalization *p2p.QuorumCertificate, qcDeserializer *QCDeserializer) (*simplex.Message, error) {
	bh, err := p2pBlockHeaderToSimplexBlockHeader(finalization.BlockHeader)
	if err != nil {
		return nil, err
	}

	qc, err := quorumCertificateFromP2P(finalization.QuorumCertificate, qcDeserializer)
	if err != nil {
		return nil, fmt.Errorf("failed to convert quorum certificate: %w", err)
	}

	return &simplex.Message{
		Finalization: &simplex.Finalization{
			Finalization: simplex.ToBeSignedFinalization{
				BlockHeader: bh,
			},
			QC: qc,
		},
	}, nil
}

func finalizeVoteFromP2P(finalizeVote *p2p.Vote, qcDeserializer *QCDeserializer) (*simplex.Message, error) {
	bh, err := p2pBlockHeaderToSimplexBlockHeader(finalizeVote.BlockHeader)
	if err != nil {
		return nil, err
	}

	sig, err := p2pSignatureToSimplexSignature(finalizeVote.Signature)
	if err != nil {
		return nil, err
	}

	return &simplex.Message{
		FinalizeVote: &simplex.FinalizeVote{
			Finalization: simplex.ToBeSignedFinalization{
				BlockHeader: bh,
			},
			Signature: sig,
		},
	}, nil
}

// HELPERS _-----------------
func p2pVoteToSimplexVote(p2pVote *p2p.Vote) (simplex.Vote, error) {
	bh, err := p2pBlockHeaderToSimplexBlockHeader(p2pVote.BlockHeader)
	if err != nil {
		return simplex.Vote{}, err
	}

	signature, err := p2pSignatureToSimplexSignature(p2pVote.Signature)
	if err != nil {
		return simplex.Vote{}, err
	}

	v := simplex.Vote{
		Vote: simplex.ToBeSignedVote{
			BlockHeader: bh,
		},
		Signature: signature,
	}

	return v, nil
}

func p2pSignatureToSimplexSignature(p2pSig *p2p.Signature) (simplex.Signature, error) {
	if p2pSig == nil {
		return simplex.Signature{}, errNilField
	}

	nodeID, err := ids.ToNodeID(p2pSig.Signer)
	if err != nil {
		return simplex.Signature{}, fmt.Errorf("failed to convert signer to NodeID: %w", err)
	}

	return simplex.Signature{
		Signer: nodeID[:],
		Value:  p2pSig.Value,
	}, nil
}

func p2pBlockHeaderToSimplexBlockHeader(p2pHeader *p2p.BlockHeader) (simplex.BlockHeader, error) {
	md, err := p2pMetadataToSimplexMetadata(p2pHeader.Metadata)
	if err != nil {
		return simplex.BlockHeader{}, fmt.Errorf("failed to convert previous metadata: %w", err)
	}

	digest, err := digestFromP2P(p2pHeader.Digest)
	if err != nil {
		return simplex.BlockHeader{}, fmt.Errorf("failed to convert digest: %w", err)
	}

	return simplex.BlockHeader{
		ProtocolMetadata: md,
		Digest:          digest,
	}, nil
}

func p2pMetadataToSimplexMetadata(p2pMetadata *p2p.ProtocolMetadata) (simplex.ProtocolMetadata, error) {
	if p2pMetadata.Version > math.MaxUint8 {
		return simplex.ProtocolMetadata{}, fmt.Errorf("version %d exceeds maximum value %d", p2pMetadata.Version, math.MaxUint8)
	}
	prev, err := digestFromP2P(p2pMetadata.Prev)
	if err != nil {
		return simplex.ProtocolMetadata{}, err
	}

	return simplex.ProtocolMetadata{
		Version: uint8(p2pMetadata.Version),
		Epoch:   p2pMetadata.Epoch,
		Round:   p2pMetadata.Round,
		Seq:     p2pMetadata.Seq,
		Prev:    prev,
	}, nil
}

func emptyVoteMetadataFromP2P(emptyVote *p2p.EmptyVoteMetadata) (simplex.EmptyVoteMetadata, error) {
	if emptyVote == nil {
		return simplex.EmptyVoteMetadata{}, errNilField
	}

	return simplex.EmptyVoteMetadata{
		Round: emptyVote.Round,
		Epoch: emptyVote.Epoch,
	}, nil
}

func digestFromP2P(p2pDigest []byte) (simplex.Digest, error) {
	if len(p2pDigest) != 32 {
		return simplex.Digest{}, fmt.Errorf("invalid digest length %d, expected %d", len(p2pDigest), 32)
	}

	var digest simplex.Digest
	copy(digest[:], p2pDigest)
	return digest, nil
}

func quorumCertificateFromP2P(qcBytes []byte, qcDeserializer *QCDeserializer) (simplex.QuorumCertificate, error) {
	if qcBytes == nil {
		return nil, errNilField
	}

	simplexQC, err := qcDeserializer.DeserializeQuorumCertificate(qcBytes)
	if err != nil {
		return nil, err
	}

	return simplexQC, nil
}

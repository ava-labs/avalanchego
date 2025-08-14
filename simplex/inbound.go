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
			BlockHeader: simplex.BlockHeader{
				ProtocolMetadata: bh.ProtocolMetadata,
			},
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

func p2pBlockHeaderToSimplexBlockHeader(p2pHeader *p2p.BlockHeader) (*simplex.BlockHeader, error) {
	md, err := p2pMetadataToSimplexMetadata(p2pHeader.Metadata)
	if err != nil {
		return nil, fmt.Errorf("failed to convert previous digest: %w", err)
	}

	digest, err := p2pDigestToSimplexDigest(p2pHeader.Digest)
	if err != nil {
		return nil, fmt.Errorf("failed to convert digest: %w", err)
	}

	return &simplex.BlockHeader{
		ProtocolMetadata: md,
		Digest:          digest,
	}, nil
}

func p2pMetadataToSimplexMetadata(p2pMetadata *p2p.ProtocolMetadata) (simplex.ProtocolMetadata, error) {
	if p2pMetadata.Version > math.MaxUint8 {
		return simplex.ProtocolMetadata{}, fmt.Errorf("version %d exceeds maximum value %d", p2pMetadata.Version, math.MaxUint8)
	}
	prev, err := p2pDigestToSimplexDigest(p2pMetadata.Prev)
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

func p2pDigestToSimplexDigest(p2pDigest []byte) (simplex.Digest, error) {
	if len(p2pDigest) != 32 {
		return simplex.Digest{}, fmt.Errorf("invalid digest length %d, expected %d", len(p2pDigest), 32)
	}

	var digest simplex.Digest
	copy(digest[:], p2pDigest)
	return digest, nil
}

func emptyNotarizationFromP2P(emptyNotarization *p2p.EmptyNotarization) (*simplex.Message, error) {
	if emptyNotarization == nil {
		return nil, errNilField
	}
	emptyNotarization.
	md, err := p2pMetadataToSimplexMetadata(emptyNotarization.Metadata)
	if err != nil {
		return nil, fmt.Errorf("failed to convert metadata: %w", err)
	}

	vote, err := p2pVoteToSimplexVote(emptyNotarization.)
	if err != nil {
		return nil, err
	}

	return &simplex.Message{
		EmptyNotarization: &simplex.EmptyNotarization{
			Vote: simplex.EmptyNotarization{
				Vote: 
			}
		},
	}, nil
}
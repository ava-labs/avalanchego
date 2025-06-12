// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package simplex

import (
	"context"
	"crypto/sha256"
	"fmt"
	"sync"

	"github.com/ava-labs/simplex"
	"google.golang.org/protobuf/proto"

	"github.com/ava-labs/avalanchego/proto/pb/p2p"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
)

type VerifiedBlock struct {
	computeDigestOnce sync.Once
	digest            simplex.Digest // cached, not serialized

	metadata   simplex.ProtocolMetadata
	innerBlock []byte
}

// BlockHeader returns the block header for the verified block.
func (v *VerifiedBlock) BlockHeader() simplex.BlockHeader {
	v.computeDigestOnce.Do(v.computeDigest)
	return simplex.BlockHeader{
		ProtocolMetadata: v.metadata,
		Digest:           v.digest,
	}
}

// Bytes returns the serialized bytes of the verified block 
// as the asn1 encoding of `encodedVerifiedBlock`.
func (v *VerifiedBlock) Bytes() []byte {
	cBlock := p2p.VerifiedBlock{
		Metadata:   v.metadata.Bytes(),
		Block: v.innerBlock,
	}

	buff, err := proto.Marshal(&cBlock)
	if err != nil {
		panic(fmt.Errorf("failed to marshal verified block: %w", err))
	}

	return buff
}

// computeDigest computes the digest of the block.
func (v *VerifiedBlock) computeDigest() {
	h := sha256.New()
	h.Write(v.Bytes())
	digest := h.Sum(nil)
	v.digest = simplex.Digest(digest)
}

type blockDeserializer struct {
	parser block.Parser
}

func (b *blockDeserializer) DeserializeBlock(bytes []byte) (simplex.VerifiedBlock, error) {
	vb, err := verifiedBlockFromBytes(bytes)
	if err != nil {
		return nil, fmt.Errorf("failed to deserialize verified block: %w", err)
	}

	_, err = b.parser.ParseBlock(context.Background(), vb.innerBlock)
	if err != nil {
		return nil, err
	}

	return vb, nil
}

func verifiedBlockFromBytes(buff []byte) (*VerifiedBlock, error) {
	var protoBlock p2p.VerifiedBlock

	if err := proto.Unmarshal(buff, &protoBlock); err != nil {
		return nil, fmt.Errorf("failed to unmarshal verified block: %w", err)
	}
	
	md, err := simplex.ProtocolMetadataFromBytes(protoBlock.Metadata)
	if err != nil {
		return nil, fmt.Errorf("failed to parse protocol metadata: %w", err)
	}

	v := &VerifiedBlock{
		metadata:   *md,
		innerBlock: protoBlock.Block,
	}

	return v, nil
}

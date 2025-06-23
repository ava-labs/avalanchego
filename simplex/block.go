// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package simplex

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/ava-labs/simplex"
	"google.golang.org/protobuf/proto"

	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/utils/hashing"

	pSimplex "github.com/ava-labs/avalanchego/proto/pb/simplex"
)

var _ simplex.Block = (*Block)(nil)
var _ simplex.VerifiedBlock = (*VerifiedBlock)(nil)
var _ simplex.BlockDeserializer = (*blockDeserializer)(nil)
var maxBlockVerifyTimeout = 30 * time.Second // Maximum time to wait for block verification

type Block struct {
	block *VerifiedBlock 
	parser    block.Parser
}

func (b *Block) BlockHeader() simplex.BlockHeader {
	return b.block.BlockHeader()
}

func (b *Block) Verify(ctx context.Context) (simplex.VerifiedBlock, error) {
	ctx, cancel := context.WithTimeout(ctx, maxBlockVerifyTimeout) // todo: should this be passed in via config?
	defer cancel()

	block, err := b.parser.ParseBlock(ctx, b.block.innerBlock)
	if err != nil {
		return nil, err
	}

	md := b.BlockHeader()
	rejection := func(ctx context.Context) error {
		b.e.removeDigestToIDMapping(md.Digest)
		return block.Reject(ctx)
	}

	b.e.blockTracker.trackBlock(md.Round, md.Digest, rejection)
	b.verifiedBlock.accept = func(ctx context.Context) error {
		b.e.removeDigestToIDMapping(md.Digest)
		b.e.ChainVM.SetPreference(context.Background(), block.ID())
		b.e.blockTracker.rejectSiblingsAndUncles(md.Round, md.Digest)
		return block.Accept(ctx)
	}

	err = block.Verify(ctx)

	if err == nil {
		b.e.observeDigestToIDMapping(md.Digest, block.ID())
	}

	return b.block, err
}

type VerifiedBlock struct {
	computeDigestOnce sync.Once
	digest            simplex.Digest // cached, not serialized

	metadata   simplex.ProtocolMetadata
	innerBlock []byte // inner block bytes
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
	cBlock := pSimplex.VerifiedBlock{
		Metadata: v.metadata.Bytes(),
		Block:    v.innerBlock,
	}

	buff, err := proto.Marshal(&cBlock)
	if err != nil {
		panic(fmt.Errorf("failed to marshal verified block: %w", err))
	}

	return buff
}

// computeDigest computes the digest of the block.
func (v *VerifiedBlock) computeDigest() {
	v.digest = hashing.ComputeHash256Array(v.Bytes())
}

type blockDeserializer struct {
	parser block.Parser
}

func (b *blockDeserializer) DeserializeBlock(bytes []byte) (simplex.Block, error) {
	vb, err := verifiedBlockFromBytes(bytes)
	if err != nil {
		return nil, fmt.Errorf("failed to deserialize verified block: %w", err)
	}

	_, err = b.parser.ParseBlock(context.Background(), vb.innerBlock)
	if err != nil {
		return nil, err
	}

	return &Block{
		block:  &VerifiedBlock{
			metadata:  vb.metadata,
			innerBlock: vb.innerBlock,
		},
		parser: b.parser,
	}, nil
}

func verifiedBlockFromBytes(buff []byte) (*VerifiedBlock, error) {
	var protoBlock pSimplex.VerifiedBlock

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

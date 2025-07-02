// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package simplex

import (
	"context"
	"errors"
	"fmt"

	"github.com/ava-labs/simplex"

	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/utils/hashing"
)

var (
	_                       simplex.BlockDeserializer = (*blockDeserializer)(nil)
	_                       simplex.Block             = (*Block)(nil)
	_                       simplex.VerifiedBlock     = (*Block)(nil)
	errBlockAlreadyVerified                           = errors.New("block has already been verified")
)

type Block struct {
	digest simplex.Digest

	// metadata contains protocol metadata for the block
	metadata simplex.ProtocolMetadata

	verified bool // whether vmBlock.verify() function has been called

	// the parsed block
	vmBlock snowman.Block
}

// BlockHeader returns the block header for the block.
func (b *Block) BlockHeader() simplex.BlockHeader {
	return simplex.BlockHeader{
		ProtocolMetadata: b.metadata,
		Digest:           b.digest,
	}
}

// Bytes returns the serialized bytes of the block.
func (b *Block) Bytes() ([]byte, error) {
	cBlock := &CanotoSimplexBlock{
		Metadata:   b.metadata.Bytes(),
		InnerBlock: b.vmBlock.Bytes(),
	}

	return cBlock.MarshalCanoto(), nil
}

func (b *Block) Verify(ctx context.Context) (simplex.VerifiedBlock, error) {
	if b.verified {
		return b, errBlockAlreadyVerified
	}

	// TODO: track blocks that have been verified to ensure they are either rejected or accepted
	b.verified = true
	err := b.vmBlock.Verify(ctx)

	return b, err
}

type blockDeserializer struct {
	parser block.Parser
}

func (d *blockDeserializer) DeserializeBlock(bytes []byte) (simplex.Block, error) {
	var canotoBlock CanotoSimplexBlock

	if err := canotoBlock.UnmarshalCanoto(bytes); err != nil {
		return nil, fmt.Errorf("failed to unmarshal block: %w", err)
	}

	md, err := simplex.ProtocolMetadataFromBytes(canotoBlock.Metadata)
	if err != nil {
		return nil, fmt.Errorf("failed to parse protocol metadata: %w", err)
	}

	vmblock, err := d.parser.ParseBlock(context.Background(), canotoBlock.InnerBlock)
	if err != nil {
		return nil, err
	}

	digest := hashing.ComputeHash256Array(bytes)

	return &Block{
		metadata: *md,
		vmBlock:  vmblock,
		digest:   digest,
	}, nil
}

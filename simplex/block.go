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

var _ simplex.BlockDeserializer = (*blockDeserializer)(nil)

var _ simplex.Block = (*Block)(nil)

var _ simplex.VerifiedBlock = (*Block)(nil)

var errBlockAlreadyVerified = errors.New("block has already been verified")

type Block struct {
	digest simplex.Digest

	// metadata contains protocol metadata for the block
	metadata simplex.ProtocolMetadata

	verified bool // whether vmBlock.verify() function has been called

	// the parsed block
	vmBlock snowman.Block
}

// BlockHeader returns the block header for the verified block.
func (b *Block) BlockHeader() simplex.BlockHeader {
	return simplex.BlockHeader{
		ProtocolMetadata: b.metadata,
		Digest:           b.digest,
	}
}

// Bytes returns the serialized bytes of the verified block.
func (b *Block) Bytes() ([]byte, error) {
	cBlock := &CanotoSimplexBlock{
		Metadata:   b.metadata.Bytes(),
		InnerBlock: b.vmBlock.Bytes(),
	}

	buff := cBlock.MarshalCanoto()

	return buff, nil
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
	return d.deserializeBlockBytes(bytes)
}

func (d *blockDeserializer) deserializeBlockBytes(buff []byte) (simplex.Block, error) {
	var canotoBlock CanotoSimplexBlock

	if err := canotoBlock.UnmarshalCanoto(buff); err != nil {
		return nil, fmt.Errorf("failed to unmarshal verified block: %w", err)
	}

	md, err := simplex.ProtocolMetadataFromBytes(canotoBlock.Metadata)
	if err != nil {
		return nil, fmt.Errorf("failed to parse protocol metadata: %w", err)
	}

	vmblock, err := d.parser.ParseBlock(context.Background(), canotoBlock.InnerBlock)
	if err != nil {
		return nil, err
	}

	digest := hashing.ComputeHash256Array(buff)

	return &Block{
		metadata: *md,
		vmBlock:  vmblock,
		digest:   digest,
	}, nil
}

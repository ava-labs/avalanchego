// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package simplex

//go:generate go run github.com/StephenButtolph/canoto/canoto $GOFILE

import (
	"context"
	"fmt"

	"github.com/ava-labs/simplex"

	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/utils/hashing"
)

var (
	_ simplex.BlockDeserializer = (*blockDeserializer)(nil)
	_ simplex.Block             = (*Block)(nil)
	_ simplex.VerifiedBlock     = (*Block)(nil)
)

type Block struct {
	digest simplex.Digest

	// metadata contains protocol metadata for the block
	metadata simplex.ProtocolMetadata

	// the parsed block
	vmBlock snowman.Block
}

// CanotoSimplexBlock is the Canoto representation of a block
type canotoSimplexBlock struct {
	Metadata   []byte `canoto:"bytes,1"`
	InnerBlock []byte `canoto:"bytes,2"`

	canotoData canotoData_canotoSimplexBlock
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
	cBlock := &canotoSimplexBlock{
		Metadata:   b.metadata.Bytes(),
		InnerBlock: b.vmBlock.Bytes(),
	}

	return cBlock.MarshalCanoto(), nil
}

func (b *Block) Verify(ctx context.Context) (simplex.VerifiedBlock, error) {
	// TODO: track blocks that have been verified to ensure they are either rejected or accepted
	return b, b.vmBlock.Verify(ctx)
}

type blockDeserializer struct {
	parser block.Parser
}

func (d *blockDeserializer) DeserializeBlock(bytes []byte) (simplex.Block, error) {
	var canotoBlock canotoSimplexBlock

	if err := canotoBlock.UnmarshalCanoto(bytes); err != nil {
		return nil, fmt.Errorf("failed to unmarshal block: %w", err)
	}

	md, err := simplex.ProtocolMetadataFromBytes(canotoBlock.Metadata)
	if err != nil {
		return nil, fmt.Errorf("failed to parse protocol metadata: %w", err)
	}

	vmblock, err := d.parser.ParseBlock(context.TODO(), canotoBlock.InnerBlock)
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

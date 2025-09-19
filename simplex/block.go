// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package simplex

//go:generate go run github.com/StephenButtolph/canoto/canoto $GOFILE

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/ava-labs/simplex"

	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/utils/hashing"
	"github.com/ava-labs/avalanchego/utils/tree"
)

var (
	_ simplex.BlockDeserializer = (*blockDeserializer)(nil)
	_ simplex.Block             = (*Block)(nil)
	_ simplex.VerifiedBlock     = (*Block)(nil)

	errDigestNotFound       = errors.New("digest not found in block tracker")
	errMismatchedPrevDigest = errors.New("prev digest does not match block parent")
	errGenesisVerification  = errors.New("genesis block should not be verified")
)

type Block struct {
	digest simplex.Digest

	// metadata contains protocol metadata for the block
	metadata simplex.ProtocolMetadata

	// the parsed block
	vmBlock snowman.Block

	blockTracker *blockTracker
}

func newBlock(metadata simplex.ProtocolMetadata, vmBlock snowman.Block, blockTracker *blockTracker) (*Block, error) {
	block := &Block{
		metadata:     metadata,
		vmBlock:      vmBlock,
		blockTracker: blockTracker,
	}
	bytes, err := block.Bytes()
	if err != nil {
		return nil, fmt.Errorf("failed to serialize block: %w", err)
	}
	block.digest = computeDigest(bytes)
	return block, nil
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

// Verify verifies the block.
func (b *Block) Verify(ctx context.Context) (simplex.VerifiedBlock, error) {
	// we should not verify the genesis block
	if b.metadata.Seq == 0 {
		return nil, errGenesisVerification
	}

	if err := b.verifyParentMatchesPrevBlock(); err != nil {
		return nil, err
	}

	if err := b.blockTracker.verifyAndTrackBlock(ctx, b); err != nil {
		return nil, fmt.Errorf("failed to verify block: %w", err)
	}

	return b, nil
}

// verifyParentMatchesPrevBlock verifies that the previous block referenced in the current block's metadata
// matches the parent of the current block's vmBlock.
func (b *Block) verifyParentMatchesPrevBlock() error {
	prevBlock, ok := b.blockTracker.getBlockByDigest(b.metadata.Prev)
	if !ok {
		return fmt.Errorf("%w: %s", errDigestNotFound, b.metadata.Prev)
	}

	if b.vmBlock.Parent() != prevBlock.vmBlock.ID() {
		return fmt.Errorf("%w: parentID %s, prevID %s", errMismatchedPrevDigest, b.vmBlock.Parent(), prevBlock.vmBlock.ID())
	}

	return nil
}

func computeDigest(bytes []byte) simplex.Digest {
	return hashing.ComputeHash256Array(bytes)
}

type blockDeserializer struct {
	parser block.Parser
}

func (d *blockDeserializer) DeserializeBlock(ctx context.Context, bytes []byte) (simplex.Block, error) {
	var canotoBlock canotoSimplexBlock

	if err := canotoBlock.UnmarshalCanoto(bytes); err != nil {
		return nil, fmt.Errorf("failed to unmarshal block: %w", err)
	}

	md, err := simplex.ProtocolMetadataFromBytes(canotoBlock.Metadata)
	if err != nil {
		return nil, fmt.Errorf("failed to parse protocol metadata: %w", err)
	}

	vmblock, err := d.parser.ParseBlock(ctx, canotoBlock.InnerBlock)
	if err != nil {
		return nil, err
	}

	return &Block{
		metadata: *md,
		vmBlock:  vmblock,
		digest:   computeDigest(bytes),
	}, nil
}

// blockTracker is used to ensure that blocks are properly rejected, if competing blocks are accepted.
type blockTracker struct {
	lock sync.Mutex

	// tracks the simplex digests to the blocks that have been verified
	simplexDigestsToBlock map[simplex.Digest]*Block

	// handles block acceptance and rejection of inner blocks
	tree tree.Tree
}

func newBlockTracker(latestBlock *Block) *blockTracker {
	return &blockTracker{
		tree: tree.New(),
		simplexDigestsToBlock: map[simplex.Digest]*Block{
			latestBlock.digest: latestBlock,
		},
	}
}

func (bt *blockTracker) getBlockByDigest(digest simplex.Digest) (*Block, bool) {
	bt.lock.Lock()
	defer bt.lock.Unlock()

	block, exists := bt.simplexDigestsToBlock[digest]
	return block, exists
}

// verifyAndTrackBlock verifies the block and tracks it in the block tracker.
// If the block is already verified, it does nothing.
func (bt *blockTracker) verifyAndTrackBlock(ctx context.Context, block *Block) error {
	bt.lock.Lock()
	defer bt.lock.Unlock()

	// check if the block is already verified
	if _, exists := bt.tree.Get(block.vmBlock); exists {
		bt.simplexDigestsToBlock[block.digest] = block
		return nil
	}

	if err := block.vmBlock.Verify(ctx); err != nil {
		return fmt.Errorf("failed to verify block: %w", err)
	}

	// track the block
	bt.simplexDigestsToBlock[block.digest] = block
	bt.tree.Add(block.vmBlock)
	return nil
}

// indexBlock calls accept on the block with the given digest, and reject on competing blocks.
func (bt *blockTracker) indexBlock(ctx context.Context, digest simplex.Digest) error {
	bt.lock.Lock()
	defer bt.lock.Unlock()

	bd, exists := bt.simplexDigestsToBlock[digest]
	if !exists {
		return fmt.Errorf("%w: %s", errDigestNotFound, digest)
	}

	// removes all digests with a lower seq
	for d, block := range bt.simplexDigestsToBlock {
		if block.metadata.Seq < bd.metadata.Seq {
			delete(bt.simplexDigestsToBlock, d)
		}
	}

	// notify the VM that we are accepting this block, and reject all competing blocks
	return bt.tree.Accept(ctx, bd.vmBlock)
}

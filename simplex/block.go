// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
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

	err := b.verifyParentMatchesPrevBlock()
	if err != nil {
		return nil, err
	}

	if b.blockTracker.isBlockAlreadyVerified(b.vmBlock) {
		return b, nil
	}

	err = b.vmBlock.Verify(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to verify block: %w", err)
	}

	// once verified, the block tracker will eventually either accept or reject the block
	b.blockTracker.trackBlock(b)
	return b, nil
}

// verifyParentMatchesPrevBlock verifies that the previous block referenced in the current block's metadata
// matches the parent of the current block's vmBlock.
func (b *Block) verifyParentMatchesPrevBlock() error {
	prevBlock := b.blockTracker.getBlockByDigest(b.metadata.Prev)
	if prevBlock == nil {
		return fmt.Errorf("%w: %s", errDigestNotFound, b.metadata.Prev)
	}

	if b.vmBlock.Parent() != prevBlock.vmBlock.ID() || b.metadata.Prev != prevBlock.digest {
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
	simplexDigestsToBlock := make(map[simplex.Digest]*Block)
	simplexDigestsToBlock[latestBlock.digest] = latestBlock

	return &blockTracker{
		tree:                  tree.New(),
		simplexDigestsToBlock: simplexDigestsToBlock,
	}
}

func (bt *blockTracker) getBlockByDigest(digest simplex.Digest) *Block {
	bt.lock.Lock()
	defer bt.lock.Unlock()

	return bt.simplexDigestsToBlock[digest]
}

func (bt *blockTracker) isBlockAlreadyVerified(block snowman.Block) bool {
	bt.lock.Lock()
	defer bt.lock.Unlock()

	_, exists := bt.tree.Get(block)
	return exists
}

func (bt *blockTracker) trackBlock(b *Block) {
	bt.lock.Lock()
	defer bt.lock.Unlock()

	bt.simplexDigestsToBlock[b.digest] = b
	bt.tree.Add(b.vmBlock)
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

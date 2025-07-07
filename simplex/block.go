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
	"github.com/ava-labs/avalanchego/vms/proposervm/tree"
)

var (
	_ simplex.BlockDeserializer = (*blockDeserializer)(nil)
	_ simplex.Block             = (*Block)(nil)
	_ simplex.VerifiedBlock     = (*Block)(nil)

	errDigestNotFound       = errors.New("digest not found in block tracker")
	errMismatchedPrevDigest = errors.New("prev digest does not match block parent")
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

// Verify calls the Verify method on the underlying vmBlock or returns an error if the block is not able to be verified.
// We ensure `b.vmBlock.parent` references the same block as `b.metadata.prev.vmBlock` to keep the two hash chains in sync.
func (b *Block) Verify(ctx context.Context) (simplex.VerifiedBlock, error) {
	if b.metadata.Seq != 0 {
		err := b.verifyParentMatchesPrevBlock(ctx)
		if err != nil {
			return nil, err
		}
	}

	err := b.vmBlock.Verify(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to verify block: %w", err)
	}

	// once verified, the block tracker will eventually either accept or reject the block
	b.blockTracker.trackBlock(b)

	return b, nil
}

// verifyPrevBlock verifies that the previous block referenced in the current block's metadata
// matches the parent of the current block's vmBlock.
func (b *Block) verifyParentMatchesPrevBlock(ctx context.Context) error {
	prevBlock := b.blockTracker.getBlock(b.metadata.Prev)
	if prevBlock != nil {
		if b.vmBlock.Parent() != prevBlock.vmBlock.ID() {
			return fmt.Errorf("%w: parentID %s, prevID %s", errMismatchedPrevDigest, b.vmBlock.Parent(), prevBlock.vmBlock.ID())
		}

		return nil
	}

	// if we do not have it in the map, it's possible for it to be the last accepted block
	lastID, err := b.blockTracker.vm.LastAccepted(ctx)
	if err != nil {
		return fmt.Errorf("failed to get last accepted block: %w", err)
	}

	if lastID != b.vmBlock.Parent() {
		return fmt.Errorf("%w: last accepted block %s, parentID %s", errDigestNotFound, lastID, b.vmBlock.Parent())
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

	vm block.ChainVM

	// handles block acceptance and rejection of inner blocks
	tree tree.Tree
}

func newBlockTracker(vm block.ChainVM) *blockTracker {
	return &blockTracker{
		tree:                  tree.New(),
		vm:                    vm,
		simplexDigestsToBlock: make(map[simplex.Digest]*Block),
	}
}

func (bt *blockTracker) getBlock(digest simplex.Digest) *Block {
	bt.lock.Lock()
	defer bt.lock.Unlock()

	return bt.simplexDigestsToBlock[digest]
}

func (bt *blockTracker) trackBlock(b *Block) {
	bt.lock.Lock()
	defer bt.lock.Unlock()

	// add the block to the block tracker
	bt.simplexDigestsToBlock[b.digest] = b
	bt.tree.Add(b.vmBlock)
}

// indexBlock indexes the block in the block tracker and sets up the onIndex callback.
func (bt *blockTracker) indexBlock(ctx context.Context, digest simplex.Digest) error {
	// check if we have already verified this block
	bt.lock.Lock()
	defer bt.lock.Unlock()

	bd, exists := bt.simplexDigestsToBlock[digest]
	if !exists {
		return fmt.Errorf("%w: %s", errDigestNotFound, digest)
	}

	// delete all digests from the map with a lower seq as this block's seq
	// we keep the seq in the map so we can reference it in verify
	for d, block := range bt.simplexDigestsToBlock {
		if block.metadata.Seq < bd.metadata.Seq {
			// remove the block from the map
			delete(bt.simplexDigestsToBlock, d)
		}
	}

	// notify the VM that we are accepting this block, and reject all competing blocks
	return bt.tree.Accept(ctx, bd.vmBlock)
}

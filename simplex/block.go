// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package simplex

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/ava-labs/simplex"
	"google.golang.org/protobuf/proto"

	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/utils/hashing"

	pSimplex "github.com/ava-labs/avalanchego/proto/pb/simplex"
)

var (
	_ simplex.Block             = (*Block)(nil)
	_ simplex.VerifiedBlock     = (*VerifiedBlock)(nil)
	_ simplex.BlockDeserializer = (*blockDeserializer)(nil)

	errBlockNotVerified = errors.New("cannot index a block that has not been verified")

	maxBlockVerifyTimeout = 30 * time.Second // Maximum time to wait for block verification
)

type Block struct {
	block  *VerifiedBlock
	parser block.Parser

	tracker *blockTracker // tracks the block's state in the engine
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

	b.tracker.trackBlock(block, b.block.BlockHeader())
	err = block.Verify(ctx)

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
		block: &VerifiedBlock{
			metadata:   vb.metadata,
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

type blockData struct {
	block  snowman.Block
	digest simplex.Digest
}

// blockTracker is used to ensure that blocks are properly rejected, if competing blocks are accepted.
type blockTracker struct {
	lock sync.Mutex

	// acceptedBlocks maps round numbers to blocks that were accepted in that round.
	acceptedBlocks map[uint64][]blockData

	// so the block tracker can set the preference of the VM
	vm block.ChainVM
}

func newBlockTracker() *blockTracker {
	return &blockTracker{
		acceptedBlocks: make(map[uint64][]blockData),
	}
}

// rejectAllStaleBlocks calls reject on blocks that have previously been verified, but will never be accepted.
// This could be blocks that were verified in the same round as acceptedDigest,
// or blocks that were verified in past rounds that were not accepted.
func (bt *blockTracker) rejectAllStaleBlocks(round uint64, acceptedDigest simplex.Digest) error {
	bt.lock.Lock()
	defer bt.lock.Unlock()

	// siblings: reject all accepted blocks in the given round that do not match the accepted digest
	for _, acceptedBlock := range bt.acceptedBlocks[round] {
		if !bytes.Equal(acceptedBlock.digest[:], acceptedDigest[:]) {
			err := acceptedBlock.block.Reject(context.Background())
			if err != nil {
				return fmt.Errorf("failed rejecting block %d: %w", acceptedBlock.digest[:], err)
			}
		}
	}
	// remove the round from the map
	delete(bt.acceptedBlocks, round)

	// uncles: reject all accepted blocks of past rounds and remove those rounds from the map
	for r, blocks := range bt.acceptedBlocks {
		if r < round {
			// reject all blocks of past rounds
			for _, block := range blocks {
				err := block.block.Reject(context.Background())
				if err != nil {
					return fmt.Errorf("failed rejecting block %d: %w", block.digest[:], err)
				}
			}
			delete(bt.acceptedBlocks, r)
		}
	}

	return nil
}

func (bt *blockTracker) trackBlock(block snowman.Block, bh simplex.BlockHeader) {
	bt.lock.Lock()
	defer bt.lock.Unlock()

	bt.acceptedBlocks[bh.Round] = append(bt.acceptedBlocks[bh.Round], blockData{
		digest: bh.Digest,
		block:  block,
	})
}

// indexBlock indexes the block in the block tracker and sets up the onIndex callback.
func (bt *blockTracker) indexBlock(ctx context.Context, round uint64, digest simplex.Digest) error {
	// check if we have already verified this block
	var bd *blockData
	for _, blockData := range bt.acceptedBlocks[round] {
		if bytes.Equal(blockData.digest[:], digest[:]) {
			bd = &blockData
			break
		}
	}

	if bd == nil {
		return errBlockNotVerified
	}

	err := bt.vm.SetPreference(context.Background(), bd.block.ID())
	if err != nil {
		return fmt.Errorf("failed to set preference for block %s: %w", bd.block.ID(), err)
	}

	err = bt.rejectAllStaleBlocks(round, bd.digest)
	if err != nil {
		return err
	}

	return bd.block.Accept(ctx)
}

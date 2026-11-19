// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package simplex

import (
	"context"
	"fmt"
	"sync"

	"github.com/ava-labs/simplex/avalanchego"
	"github.com/ava-labs/simplex/common"
	metadata "github.com/ava-labs/simplex/msm"
	"github.com/ava-labs/simplex/simplex"
)

type communication struct {
	validators func() common.Nodes
	Sender
	Broadcaster
}

func (c *communication) Validators() common.Nodes {
	return c.validators()
}

// CallbackStorage is a wrapper around Storage that skips indexing Telocks
// and delegates post-index handling to a caller-provided onIndex hook.
type CallbackStorage struct {
	// CachedStorage is used to ensure that we prune the cache on Index.
	*CachedStorage

	msm *metadata.StateMachine

	onIndex func(block *ParsedBlock) error
}

func NewCallbackStorage(storage *CachedStorage, msm *metadata.StateMachine, onIndex func(block *ParsedBlock) error) *CallbackStorage {
	return &CallbackStorage{
		CachedStorage: storage,
		msm:           msm,
		onIndex:       onIndex,
	}
}

func (s *CallbackStorage) Retrieve(seq uint64) (common.VerifiedBlock, common.Finalization, error) {
	block, finalization, err := s.GetBlock(seq)
	if err != nil {
		return nil, common.Finalization{}, err
	}
	parsedBlock := &ParsedBlock{
		legacyBlock:       seq <= s.lastNonSimplexHeight,
		msm:               s.msm,
		StateMachineBlock: block,
	}
	return parsedBlock, *finalization, nil
}

func (s *CallbackStorage) Index(ctx context.Context, block common.VerifiedBlock, certificate common.Finalization) error {
	pb, ok := block.(*ParsedBlock)
	if !ok {
		return fmt.Errorf("expected ParsedBlock, got %T", block)
	}

	// A Telock only extends time until the epoch transition finalizes, so we never index it.
	if pb.Type() == metadata.BlockTypeTelock {
		return nil
	}

	if err := s.CachedStorage.Index(ctx, block, certificate); err != nil {
		return err
	}

	return s.onIndex(pb)
}

// cachedBlock is a wrapper around ParsedBlock that caches the block in the CachedStorage upon verification.
// It is needed for the MSM because the MSM needs to be able to retrieve blocks that aren't finalized during its execution.
// These blocks are cached in the CachedStorage upon verification, and removed from the cache upon finalization (indexing).
type cachedBlock struct {
	cache *CachedStorage
	*ParsedBlock
}

func (cb *cachedBlock) Verify(ctx context.Context, verifyOpts ...common.VerifyOptions) (common.VerifiedBlock, error) {
	vb, err := cb.ParsedBlock.Verify(ctx, verifyOpts...)
	if err == nil {
		cb.cache.insertBlock(cb.ParsedBlock)
	}
	return vb, err
}

type CachedStorage struct {
	// lastNonSimplexHeight is the height of the last block that is not a Simplex block.
	// Blocks with height <= this are called legacy blocks.
	lastNonSimplexHeight uint64
	msm                  *metadata.StateMachine
	lock                 sync.RWMutex
	Storage
	cache map[common.Digest]cachedBlock
}

func NewCachedStorage(storage Storage, lastNonSimplexHeight uint64) *CachedStorage {
	return &CachedStorage{
		lastNonSimplexHeight: lastNonSimplexHeight,
		Storage:              storage,
		cache:                make(map[common.Digest]cachedBlock),
	}
}

func (cs *CachedStorage) RetrieveBlock(seq uint64, digest common.Digest) (metadata.StateMachineBlock, *common.Finalization, error) {
	block, finalization, err := cs.Retrieve(seq, digest)
	if err != nil {
		return metadata.StateMachineBlock{}, nil, err
	}

	return block.(*ParsedBlock).Clone(), finalization, nil
}

func (cs *CachedStorage) Retrieve(seq uint64, digest common.Digest) (common.VerifiedBlock, *common.Finalization, error) {
	cs.lock.RLock()

	item, exists := cs.cache[digest]
	if exists {
		cs.lock.RUnlock()
		return item.ParsedBlock, nil, nil
	}

	for _, cb := range cs.cache {
		if cb.BlockHeader().Seq == seq {
			if cb.Digest() == digest || digest == (common.Digest{}) {
				cs.lock.RUnlock()
				return cb.ParsedBlock, nil, nil
			}
		}
	}
	cs.lock.RUnlock()

	// We don't populate the cache here because we populate it externally.
	block, finalization, err := cs.GetBlock(seq)
	if err != nil {
		return nil, nil, err
	}

	legacy := seq <= cs.lastNonSimplexHeight

	if digest != (common.Digest{}) {
		// A block predating Simplex is identified by its inner block's digest, not by a Simplex block digest.
		blockDigest := block.Digest()
		if legacy && block.InnerBlock != nil {
			blockDigest = common.Digest(block.InnerBlock.Digest())
		}
		if blockDigest != digest {
			return nil, nil, common.ErrBlockNotFound
		}
	}

	return &ParsedBlock{
		legacyBlock:       legacy,
		StateMachineBlock: block,
		msm:               cs.msm,
	}, finalization, err
}

func (cs *CachedStorage) Index(ctx context.Context, block common.VerifiedBlock, certificate common.Finalization) error {
	cs.lock.Lock()
	defer cs.lock.Unlock()

	err := cs.Storage.Index(ctx, block, certificate)

	if err == nil {
		// We delete the block from the cache after it has been indexed because now that it is persisted,
		// we can just lookup by sequence number instead of digest.
		delete(cs.cache, block.BlockHeader().Digest)

		// We also delete all blocks that are older than the indexed block, including the finalized block because they are now finalized and persisted.
		for digest, cachedBlock := range cs.cache {
			if cachedBlock.BlockHeader().Seq <= block.BlockHeader().Seq {
				delete(cs.cache, digest)
			}
		}
	}

	return err
}

func (cs *CachedStorage) insertBlock(block *ParsedBlock) {
	cs.lock.Lock()
	defer cs.lock.Unlock()

	// inserting is async, so ensure the sequence we are inserting
	// hasn't been indexed already
	if block.BlockHeader().Seq < cs.NumBlocks() {
		return
	}

	cs.cache[block.Digest()] = cachedBlock{
		ParsedBlock: block,
	}
}

type NoopAuxiliaryInfoApp struct{}

func (n *NoopAuxiliaryInfoApp) IsLegalAppend(versionID common.VersionID, nodes metadata.NodeBLSMappings, history [][]byte, x []byte) error {
	if len(x) > 0 {
		return fmt.Errorf("input should be empty")
	}
	return nil
}

func (n *NoopAuxiliaryInfoApp) IsSufficient(versionID common.VersionID, nodes metadata.NodeBLSMappings, history [][]byte) (bool, error) {
	return true, nil
}

func (n *NoopAuxiliaryInfoApp) Generate(common.VersionID, metadata.NodeBLSMappings, [][]byte) ([]byte, error) {
	return nil, nil
}

func (n *NoopAuxiliaryInfoApp) DefaultVersionID() common.VersionID {
	return 0
}

type blockBuilderWaiter struct {
	lock   sync.Mutex
	cancel context.CancelFunc
	msm    *metadata.StateMachine
	cs     *CachedStorage
	e      *simplex.Epoch
	vm     VM
}

func newBlockBuilderWaiter(msm *metadata.StateMachine, cs *CachedStorage, vm VM) *blockBuilderWaiter {
	return &blockBuilderWaiter{
		msm: msm,
		cs:  cs,
		vm:  vm,
	}
}

func (bw *blockBuilderWaiter) stop() {
	bw.lock.Lock()
	defer bw.lock.Unlock()
	if bw.cancel != nil {
		bw.cancel()
		bw.cancel = nil
	}
}

func (bw *blockBuilderWaiter) WaitForPendingBlock(ctx context.Context) {
	bw.lock.Lock()
	if bw.cancel != nil {
		bw.cancel()
	}
	ctx, cancel := context.WithCancel(ctx)
	bw.cancel = cancel
	bw.lock.Unlock()
	defer cancel()

	md := bw.e.Metadata()
	bw.msm.WaitForPendingBlock(ctx, md)
}

func (bw *blockBuilderWaiter) BuildBlock(ctx context.Context, metadata common.ProtocolMetadata, blacklist common.Blacklist) (common.VerifiedBlock, bool) {
	block, err := bw.msm.BuildBlock(ctx, metadata, blacklist)
	if err != nil {
		return nil, false
	}

	pb := &ParsedBlock{
		legacyBlock:       metadata.Seq <= bw.cs.lastNonSimplexHeight,
		StateMachineBlock: *block,
		msm:               bw.msm,
	}

	// Ensure the builders block is in the cache after verification
	bw.cs.insertBlock(pb)

	return pb, true
}

type blockDeserializer struct {
	vm VM
	cs *CachedStorage
}

func (bd *blockDeserializer) DeserializeBlock(ctx context.Context, bytes []byte) (common.Block, error) {
	var rawBlock metadata.RawBlock
	if err := rawBlock.UnmarshalCanoto(bytes); err != nil {
		return nil, err
	}

	var innerBlock avalanchego.VMBlock
	if len(rawBlock.InnerBlockBytes) > 0 {
		block, err := bd.vm.ParseBlock(ctx, rawBlock.InnerBlockBytes)
		if err != nil {
			return nil, err
		}
		innerBlock = block
	}

	seq := rawBlock.Metadata.SimplexProtocolMetadata.Seq
	legacy := seq <= bd.cs.lastNonSimplexHeight

	return &cachedBlock{
		ParsedBlock: &ParsedBlock{
			legacyBlock: legacy,
			StateMachineBlock: metadata.StateMachineBlock{
				InnerBlock: innerBlock,
				Metadata:   rawBlock.Metadata,
			},
			msm: bd.cs.msm,
		},
		cache: bd.cs,
	}, nil
}

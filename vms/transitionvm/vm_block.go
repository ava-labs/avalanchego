// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package transitionvm

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"

	smblock "github.com/ava-labs/avalanchego/snow/engine/snowman/block"
)

var (
	_ snowman.Block             = (*block)(nil)
	_ smblock.WithVerifyContext = (*block)(nil)
)

// block wraps a block produced by the currently active chain.
//
// On Verify, it ensures the inner block was derived from the correct chain or
// fails verification if that isn't possible.
//
// On Acceptance of the transition block, it transitions the chain to the
// post-transition chain.
//
// The block's immutable metadata (ID, parent, bytes, height, timestamp) is
// cached at construction so these accessors don't need to consult the wrapped
// block. This ensures that pre-transition blocks are safe to access even after
// the transition.
type block struct {
	vm *VM

	// lock is ordered after [VM.transitionLock]. The code MUST never acquire
	// [VM.transitionLock] while holding `block.lock`.
	lock         sync.Mutex
	blk          snowman.Block
	transitioned bool

	id        ids.ID
	parentID  ids.ID
	bytes     []byte
	height    uint64
	timestamp time.Time
}

func (b *block) ID() ids.ID           { return b.id }
func (b *block) Parent() ids.ID       { return b.parentID }
func (b *block) Bytes() []byte        { return b.bytes }
func (b *block) Height() uint64       { return b.height }
func (b *block) Timestamp() time.Time { return b.timestamp }

func (b *block) Verify(ctx context.Context) error {
	b.vm.transitionLock.RLock()
	defer b.vm.transitionLock.RUnlock()
	b.vm.current.chainCtx.Lock.Lock()
	defer b.vm.current.chainCtx.Lock.Unlock()

	if err := b.maybeTransition(ctx); err != nil {
		return err
	}
	return b.blk.Verify(ctx)
}

func (b *block) ShouldVerifyWithContext(ctx context.Context) (bool, error) {
	b.vm.transitionLock.RLock()
	defer b.vm.transitionLock.RUnlock()
	b.vm.current.chainCtx.Lock.Lock()
	defer b.vm.current.chainCtx.Lock.Unlock()

	if err := b.maybeTransition(ctx); err != nil {
		return false, err
	}

	blkWithCtx, ok := b.blk.(smblock.WithVerifyContext)
	if !ok {
		return false, nil
	}
	return blkWithCtx.ShouldVerifyWithContext(ctx)
}

var errBlockDoesNotImplementWithVerifyContext = errors.New("block does not implement WithVerifyContext")

func (b *block) VerifyWithContext(ctx context.Context, blockCtx *smblock.Context) error {
	b.vm.transitionLock.RLock()
	defer b.vm.transitionLock.RUnlock()
	b.vm.current.chainCtx.Lock.Lock()
	defer b.vm.current.chainCtx.Lock.Unlock()

	if err := b.maybeTransition(ctx); err != nil {
		return err
	}

	blkWithCtx, ok := b.blk.(smblock.WithVerifyContext)
	if !ok {
		return errBlockDoesNotImplementWithVerifyContext
	}
	return blkWithCtx.VerifyWithContext(ctx, blockCtx)
}

var errPostTransitionBlockBeforeTransition = errors.New("post-transition block before transition")

// maybeTransition updates the block to reference the post-transition VM if the
// block's ancestry includes the transition block. It returns an error if the
// block should be updated to reference the post-transition VM but isn't able
// to yet.
//
// If maybeTransition returns nil, then all future calls to maybeTransition will
// also return nil and will not modify the block.
//
// It is assumed that [VM.transitionLock] is held.
func (b *block) maybeTransition(ctx context.Context) error {
	b.lock.Lock()
	defer b.lock.Unlock()

	// Since `block.transitioned` is set to true after modifying the block, this
	// check enforces the documented invariant that the block is only modified
	// once.
	if b.transitioned {
		return nil
	}

	parent, err := b.vm.current.chain.GetBlock(ctx, b.parentID)
	if err != nil {
		return err
	}
	// The parent timestamp is deterministic, so preTransition enforces the
	// invariant that future modifications will not modify the inner block.
	switch preTransition := parent.Timestamp().Before(b.vm.transitionTime); {
	case preTransition:
		return nil
	case !b.vm.transitioned:
		return errPostTransitionBlockBeforeTransition
	}

	newBlock, err := b.vm.current.chain.ParseBlock(ctx, b.bytes)
	if err != nil {
		return err
	}
	b.blk = newBlock
	b.transitioned = true
	return nil
}

// Accept does not hold transitionLock: accepting a block at or after the
// transition time triggers the transition, which takes the write lock itself.
func (b *block) Accept(ctx context.Context) error {
	b.vm.current.chainCtx.Lock.Lock()
	err := b.blk.Accept(ctx)
	b.vm.current.chainCtx.Lock.Unlock()
	if err != nil {
		return err
	}
	// `block.lock` doesn't need to be held here because the block is immutable
	// after either `block.Verify` or `block.VerifyWithContext` return nil.
	if !b.vm.transitioned && !b.timestamp.Before(b.vm.transitionTime) {
		return b.vm.transition(ctx, b.blk)
	}
	return nil
}

func (b *block) Reject(ctx context.Context) error {
	b.vm.transitionLock.RLock()
	defer b.vm.transitionLock.RUnlock()
	b.vm.current.chainCtx.Lock.Lock()
	defer b.vm.current.chainCtx.Lock.Unlock()

	// `block.lock` doesn't need to be held here because the block is immutable
	// after either `block.Verify` or `block.VerifyWithContext` return nil.
	//
	// Once transitioned, the pre-transition chain is shut down. Forwarding
	// Reject into it could return an error, which the engine treats as fatal.
	if b.transitioned != b.vm.transitioned {
		return nil
	}
	return b.blk.Reject(ctx)
}

func (vm *VM) BuildBlock(ctx context.Context) (snowman.Block, error) {
	vm.transitionLock.RLock()
	defer vm.transitionLock.RUnlock()
	vm.current.chainCtx.Lock.Lock()
	defer vm.current.chainCtx.Lock.Unlock()

	return vm.wrapBlock(vm.current.chain.BuildBlock(ctx))
}

func (vm *VM) BuildBlockWithContext(ctx context.Context, blockCtx *smblock.Context) (snowman.Block, error) {
	vm.transitionLock.RLock()
	defer vm.transitionLock.RUnlock()
	vm.current.chainCtx.Lock.Lock()
	defer vm.current.chainCtx.Lock.Unlock()

	return vm.wrapBlock(vm.current.chain.BuildBlockWithContext(ctx, blockCtx))
}

func (vm *VM) ParseBlock(ctx context.Context, blockBytes []byte) (snowman.Block, error) {
	vm.transitionLock.RLock()
	defer vm.transitionLock.RUnlock()
	vm.current.chainCtx.Lock.Lock()
	defer vm.current.chainCtx.Lock.Unlock()

	return vm.wrapBlock(vm.current.chain.ParseBlock(ctx, blockBytes))
}

func (vm *VM) GetBlock(ctx context.Context, blkID ids.ID) (snowman.Block, error) {
	vm.transitionLock.RLock()
	defer vm.transitionLock.RUnlock()
	vm.current.chainCtx.Lock.Lock()
	defer vm.current.chainCtx.Lock.Unlock()

	return vm.wrapBlock(vm.current.chain.GetBlock(ctx, blkID))
}

func (vm *VM) wrapBlock(b snowman.Block, err error) (snowman.Block, error) {
	if err != nil {
		return nil, err
	}
	return &block{
		vm:           vm,
		blk:          b,
		transitioned: vm.transitioned,
		id:           b.ID(),
		parentID:     b.Parent(),
		bytes:        b.Bytes(),
		height:       b.Height(),
		timestamp:    b.Timestamp(),
	}, nil
}

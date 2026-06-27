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
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
)

var (
	_ snowman.Block           = (*preBlock)(nil)
	_ block.WithVerifyContext = (*preBlock)(nil)
)

// preBlock wraps a block from the pre-transition chain. It triggers the
// transition when a block at or after the transition time is accepted, and
// fails verification of a block whose parent is at or after it.
//
// The block's immutable metadata (ID, parent, bytes, height, timestamp) is
// cached at construction so these accessors don't need to consult the wrapped
// block.
type preBlock struct {
	vm *VM

	lock         sync.RWMutex
	blk          snowman.Block
	transitioned bool

	id        ids.ID
	parentID  ids.ID
	bytes     []byte
	height    uint64
	timestamp time.Time
}

func (p *preBlock) ID() ids.ID           { return p.id }
func (p *preBlock) Parent() ids.ID       { return p.parentID }
func (p *preBlock) Bytes() []byte        { return p.bytes }
func (p *preBlock) Height() uint64       { return p.height }
func (p *preBlock) Timestamp() time.Time { return p.timestamp }

func (p *preBlock) Verify(ctx context.Context) error {
	p.vm.transitionLock.RLock()
	defer p.vm.transitionLock.RUnlock()

	p.lock.Lock()
	defer p.lock.Unlock()

	if err := p.maybeTransition(ctx); err != nil {
		return err
	}
	return p.blk.Verify(ctx)
}

// ShouldVerifyWithContext forwards to the inner block, or returns false if it
// doesn't implement [block.WithVerifyContext].
func (p *preBlock) ShouldVerifyWithContext(ctx context.Context) (bool, error) {
	p.vm.transitionLock.RLock()
	defer p.vm.transitionLock.RUnlock()

	p.lock.Lock()
	defer p.lock.Unlock()

	if err := p.maybeTransition(ctx); err != nil {
		return false, err
	}

	blkWithCtx, ok := p.blk.(block.WithVerifyContext)
	if !ok {
		return false, nil
	}
	return blkWithCtx.ShouldVerifyWithContext(ctx)
}

var errBlockDoesNotImplementWithVerifyContext = errors.New("block does not implement WithVerifyContext")

// VerifyWithContext forwards to the inner block after the same parent check as
// [preBlock.Verify].
func (p *preBlock) VerifyWithContext(ctx context.Context, blockCtx *block.Context) error {
	p.vm.transitionLock.RLock()
	defer p.vm.transitionLock.RUnlock()

	p.lock.Lock()
	defer p.lock.Unlock()

	if err := p.maybeTransition(ctx); err != nil {
		return err
	}

	blkWithCtx, ok := p.blk.(block.WithVerifyContext)
	if !ok {
		return errBlockDoesNotImplementWithVerifyContext
	}
	return blkWithCtx.VerifyWithContext(ctx, blockCtx)
}

var errPostTransitionBlockBeforeTransition = errors.New("post-transition block before transition")

func (p *preBlock) maybeTransition(ctx context.Context) error {
	if p.transitioned {
		return nil
	}

	parent, err := p.vm.current.chain.GetBlock(ctx, p.parentID)
	if err != nil {
		return err
	}
	if parentTime := parent.Timestamp(); parentTime.Before(p.vm.transitionTime) {
		return nil
	}

	if !p.vm.transitioned {
		return errPostTransitionBlockBeforeTransition
	}

	newBlock, err := p.vm.current.chain.ParseBlock(ctx, p.bytes)
	if err != nil {
		return err
	}
	p.blk = newBlock
	p.transitioned = true
	return nil
}

// Accept does not hold transitionLock: accepting a block at or after the
// transition time triggers the transition, which takes the write lock itself.
func (p *preBlock) Accept(ctx context.Context) error {
	if err := p.blk.Accept(ctx); err != nil {
		return err
	}
	if p.transitioned || p.timestamp.Before(p.vm.transitionTime) {
		return nil
	}
	return p.vm.transition(ctx, p.blk)
}

func (p *preBlock) Reject(ctx context.Context) error {
	p.vm.transitionLock.RLock()
	defer p.vm.transitionLock.RUnlock()

	p.lock.RLock()
	defer p.lock.RUnlock()

	// Once transitioned, the pre-transition chain is shut down. Forwarding
	// Reject into it could return an error, which the engine treats as fatal.
	if p.transitioned != p.vm.transitioned {
		return nil
	}
	return p.blk.Reject(ctx)
}

func (vm *VM) BuildBlock(ctx context.Context) (snowman.Block, error) {
	vm.transitionLock.RLock()
	defer vm.transitionLock.RUnlock()

	return vm.wrapBlock(vm.current.chain.BuildBlock(ctx))
}

func (vm *VM) BuildBlockWithContext(ctx context.Context, blockCtx *block.Context) (snowman.Block, error) {
	vm.transitionLock.RLock()
	defer vm.transitionLock.RUnlock()

	return vm.wrapBlock(vm.current.chain.BuildBlockWithContext(ctx, blockCtx))
}

func (vm *VM) ParseBlock(ctx context.Context, blockBytes []byte) (snowman.Block, error) {
	vm.transitionLock.RLock()
	defer vm.transitionLock.RUnlock()

	return vm.wrapBlock(vm.current.chain.ParseBlock(ctx, blockBytes))
}

func (vm *VM) GetBlock(ctx context.Context, blkID ids.ID) (snowman.Block, error) {
	vm.transitionLock.RLock()
	defer vm.transitionLock.RUnlock()

	return vm.wrapBlock(vm.current.chain.GetBlock(ctx, blkID))
}

func (vm *VM) wrapBlock(b snowman.Block, err error) (snowman.Block, error) {
	if err != nil {
		return nil, err
	}
	return &preBlock{
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

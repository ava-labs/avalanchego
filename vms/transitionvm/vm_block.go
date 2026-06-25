// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package transitionvm

import (
	"context"
	"errors"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
)

var (
	_ snowman.Block           = (*preBlock)(nil)
	_ block.WithVerifyContext = (*preBlock)(nil)
)

type preBlock struct {
	snowman.Block
	v *VM
}

var errPreTransitionBlockAfterTransition = errors.New("pre-transition block after transition")

func (p *preBlock) Verify(ctx context.Context) error {
	p.v.transitionLock.RLock()
	defer p.v.transitionLock.RUnlock()

	if err := p.verifyPreTransition(ctx); err != nil {
		return err
	}
	return p.Block.Verify(ctx)
}

// ShouldVerifyWithContext forwards to the inner block so that blocks requiring
// the P-Chain context (e.g. ICM predicate verification) are still verified with
// it while wrapped by the transition VM. Blocks that don't implement
// [block.WithVerifyContext] never require a context.
func (p *preBlock) ShouldVerifyWithContext(ctx context.Context) (bool, error) {
	blkWithCtx, ok := p.Block.(block.WithVerifyContext)
	if !ok {
		return false, nil
	}
	return blkWithCtx.ShouldVerifyWithContext(ctx)
}

var errBlockDoesNotImplementWithVerifyContext = errors.New("block does not implement WithVerifyContext")

// VerifyWithContext forwards to the inner block's VerifyWithContext, applying
// the same pre-transition parent check as [preBlock.Verify].
func (p *preBlock) VerifyWithContext(ctx context.Context, blockCtx *block.Context) error {
	p.v.transitionLock.RLock()
	defer p.v.transitionLock.RUnlock()

	if err := p.verifyPreTransition(ctx); err != nil {
		return err
	}
	blkWithCtx, ok := p.Block.(block.WithVerifyContext)
	if !ok {
		return errBlockDoesNotImplementWithVerifyContext
	}
	return blkWithCtx.VerifyWithContext(ctx, blockCtx)
}

// verifyPreTransition ensures the block's parent is a pre-transition block.
//
// Callers must hold p.v.transitionLock.
func (p *preBlock) verifyPreTransition(ctx context.Context) error {
	parent, err := p.v.current.chain.GetBlock(ctx, p.Parent())
	if err != nil {
		return err
	}

	// Make sure the parent block is a pre-transition block.
	if parentTime := parent.Timestamp(); !parentTime.Before(p.v.transitionTime) {
		return errPreTransitionBlockAfterTransition
	}
	return nil
}

// Accept is basically the only function that does not immediately prevent
// transitions. This is because Accept is the function that actually triggers
// the transition.
func (p *preBlock) Accept(ctx context.Context) error {
	if err := p.Block.Accept(ctx); err != nil {
		return err
	}
	if time := p.Timestamp(); time.Before(p.v.transitionTime) {
		return nil
	}
	return p.v.transition(ctx, p.Block)
}

func (p *preBlock) Reject(ctx context.Context) error {
	p.v.transitionLock.RLock()
	defer p.v.transitionLock.RUnlock()

	// If the VM transitioned with some blocks still in consensus, they are
	// guaranteed to all be rejected. However, we don't want to run into a
	// situation where we call into the now-closed pre-transition VM. So we just
	// do nothing for this block.
	if p.v.transitioned {
		return nil
	}
	return p.Block.Reject(ctx)
}

func (vm *VM) BuildBlock(ctx context.Context) (snowman.Block, error) {
	vm.transitionLock.RLock()
	defer vm.transitionLock.RUnlock()

	b, err := vm.current.chain.BuildBlock(ctx)
	if err != nil {
		return nil, err
	}
	return vm.wrapBlock(b), nil
}

func (vm *VM) BuildBlockWithContext(ctx context.Context, blockCtx *block.Context) (snowman.Block, error) {
	vm.transitionLock.RLock()
	defer vm.transitionLock.RUnlock()

	b, err := vm.current.chain.BuildBlockWithContext(ctx, blockCtx)
	if err != nil {
		return nil, err
	}
	return vm.wrapBlock(b), nil
}

func (vm *VM) ParseBlock(ctx context.Context, blockBytes []byte) (snowman.Block, error) {
	vm.transitionLock.RLock()
	defer vm.transitionLock.RUnlock()

	b, err := vm.current.chain.ParseBlock(ctx, blockBytes)
	if err != nil {
		return nil, err
	}
	return vm.wrapBlock(b), nil
}

func (vm *VM) GetBlock(ctx context.Context, blkID ids.ID) (snowman.Block, error) {
	vm.transitionLock.RLock()
	defer vm.transitionLock.RUnlock()

	b, err := vm.current.chain.GetBlock(ctx, blkID)
	if err != nil {
		return nil, err
	}
	return vm.wrapBlock(b), nil
}

func (vm *VM) wrapBlock(b snowman.Block) snowman.Block {
	if vm.transitioned {
		return b
	}
	return &preBlock{b, vm}
}

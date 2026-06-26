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

// preBlock wraps a block from the pre-transition chain. It triggers the
// transition when a block at or after the transition time is accepted, and
// fails verification of a block whose parent is at or after it.
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

// ShouldVerifyWithContext forwards to the inner block, or returns false if it
// doesn't implement [block.WithVerifyContext].
func (p *preBlock) ShouldVerifyWithContext(ctx context.Context) (bool, error) {
	blkWithCtx, ok := p.Block.(block.WithVerifyContext)
	if !ok {
		return false, nil
	}
	return blkWithCtx.ShouldVerifyWithContext(ctx)
}

var errBlockDoesNotImplementWithVerifyContext = errors.New("block does not implement WithVerifyContext")

// VerifyWithContext forwards to the inner block after the same parent check as
// [preBlock.Verify].
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

// verifyPreTransition ensures the parent precedes the transition time.
//
// Callers must hold transitionLock.
func (p *preBlock) verifyPreTransition(ctx context.Context) error {
	parent, err := p.v.current.chain.GetBlock(ctx, p.Parent())
	if err != nil {
		return err
	}

	if parentTime := parent.Timestamp(); !parentTime.Before(p.v.transitionTime) {
		return errPreTransitionBlockAfterTransition
	}
	return nil
}

// Accept does not hold transitionLock: accepting a block at or after the
// transition time triggers the transition, which takes the write lock itself.
func (p *preBlock) Accept(ctx context.Context) error {
	if err := p.Block.Accept(ctx); err != nil {
		return err
	}
	if p.Timestamp().Before(p.v.transitionTime) {
		return nil
	}
	return p.v.transition(ctx, p.Block)
}

func (p *preBlock) Reject(ctx context.Context) error {
	p.v.transitionLock.RLock()
	defer p.v.transitionLock.RUnlock()

	// Once transitioned, the pre-transition chain is shut down. Forwarding
	// Reject into it could return an error, which the engine treats as fatal.
	if p.v.transitioned {
		return nil
	}
	return p.Block.Reject(ctx)
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
	if vm.transitioned {
		return b, nil
	}
	return &preBlock{b, vm}, nil
}

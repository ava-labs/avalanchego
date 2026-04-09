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

var _ snowman.Block = &preBlock{}

type preBlock struct {
	snowman.Block
	v *VM
}

var errPreTransitionBlockAfterTransition = errors.New("pre-transition block after transition")

func (p *preBlock) Verify(ctx context.Context) error {
	p.v.transitionLock.RLock()
	defer p.v.transitionLock.RUnlock()

	parent, err := p.v.current.chain.GetBlock(ctx, p.Parent())
	if err != nil {
		return err
	}

	// Make sure the parent block is a pre-transition block.
	if parentTime := parent.Timestamp(); !parentTime.Before(p.v.transitionTime) {
		return errPreTransitionBlockAfterTransition
	}
	return p.Block.Verify(ctx)
}

func (p *preBlock) Accept(ctx context.Context) error {
	if err := p.Block.Accept(ctx); err != nil {
		return err
	}

	if time := p.Timestamp(); time.Before(p.v.transitionTime) {
		return nil
	}
	return p.v.transition(ctx)
}

func (p *preBlock) Reject(ctx context.Context) error {
	p.v.transitionLock.RLock()
	defer p.v.transitionLock.RUnlock()

	// If the VM transitioned with some blocks still in consensus, they are
	// guranteed to all be rejected. However, we don't want to run into a
	// situation where we call into the now-closed pre-transition VM. So we just
	// do nothing for this block.
	if p.v.transitioned {
		return nil
	}
	return p.Block.Reject(ctx)
}

func (v *VM) BuildBlock(ctx context.Context) (snowman.Block, error) {
	v.transitionLock.RLock()
	defer v.transitionLock.RUnlock()

	b, err := v.current.chain.BuildBlock(ctx)
	if err != nil {
		return nil, err
	}
	return v.wrapBlock(b), nil
}

func (v *VM) BuildBlockWithContext(ctx context.Context, blockCtx *block.Context) (snowman.Block, error) {
	v.transitionLock.RLock()
	defer v.transitionLock.RUnlock()

	b, err := v.current.chain.BuildBlockWithContext(ctx, blockCtx)
	if err != nil {
		return nil, err
	}
	return v.wrapBlock(b), nil
}

func (v *VM) ParseBlock(ctx context.Context, blockBytes []byte) (snowman.Block, error) {
	v.transitionLock.RLock()
	defer v.transitionLock.RUnlock()

	b, err := v.current.chain.ParseBlock(ctx, blockBytes)
	if err != nil {
		return nil, err
	}
	return v.wrapBlock(b), nil
}

func (v *VM) GetBlock(ctx context.Context, blkID ids.ID) (snowman.Block, error) {
	v.transitionLock.RLock()
	defer v.transitionLock.RUnlock()

	b, err := v.current.chain.GetBlock(ctx, blkID)
	if err != nil {
		return nil, err
	}
	return v.wrapBlock(b), nil
}

func (v *VM) wrapBlock(b snowman.Block) snowman.Block {
	if v.transitioned {
		return b
	}
	return &preBlock{b, v}
}

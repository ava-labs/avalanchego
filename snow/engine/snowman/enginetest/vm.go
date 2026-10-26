// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package enginetest

import (
	"context"
	"errors"
	"testing"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman/snowmantest"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block/blocktest"
	"github.com/ava-labs/avalanchego/snow/snowtest"
)

// ErrUnknownParent is returned by a block's Verify when its parent is neither
// accepted nor verified.
var ErrUnknownParent = errors.New("unknown parent")

// VM is a fake [block.ChainVM]
type VM struct {
	blocktest.VM

	// VerifyF, if non-nil, is called by a block's Verify after the VM checked
	// that the block's parent is known. A non-nil error fails verification.
	VerifyF func(*snowmantest.Block) error

	byBytes          map[string]*snowmantest.Block
	verified         map[ids.ID]*snowmantest.Block // verified and undecided
	accepted         map[ids.ID]*snowmantest.Block
	acceptedByHeight map[uint64]*snowmantest.Block
	lastAccepted     *snowmantest.Block
}

// NewVM returns a [VM] that can parse the given chain. The heights must ascend
// and be contiguous. The genesis block, and any prefix of [chain] that is
// already accepted, are the VM's accepted blocks.
func NewVM(t *testing.T, chain []*snowmantest.Block) *VM {
	vm := &VM{
		byBytes:          make(map[string]*snowmantest.Block, len(chain)+1),
		verified:         make(map[ids.ID]*snowmantest.Block),
		accepted:         make(map[ids.ID]*snowmantest.Block, len(chain)+1),
		acceptedByHeight: make(map[uint64]*snowmantest.Block, len(chain)+1),
	}
	vm.byBytes[string(snowmantest.Genesis.Bytes())] = snowmantest.Genesis
	vm.markAccepted(snowmantest.Genesis)
	for _, blk := range chain {
		vm.byBytes[string(blk.Bytes())] = blk
		if blk.Status == snowtest.Accepted {
			vm.markAccepted(blk)
		}
	}

	vm.T = t
	vm.Default(true)
	vm.CantSetState = false
	vm.CantSetPreference = false
	return vm
}

// BuildBlock returns the block built by [VM.BuildBlockF], tracked by the VM.
func (vm *VM) BuildBlock(ctx context.Context) (snowman.Block, error) {
	blk, err := vm.VM.BuildBlock(ctx)
	if err != nil {
		return nil, err
	}
	return vm.ParseBlock(ctx, blk.Bytes())
}

// GetBlock returns a block that is accepted, or verified and not decided.
func (vm *VM) GetBlock(_ context.Context, blkID ids.ID) (snowman.Block, error) {
	if blk, ok := vm.accepted[blkID]; ok {
		return vm.wrap(blk), nil
	}
	if blk, ok := vm.verified[blkID]; ok {
		return vm.wrap(blk), nil
	}
	return nil, database.ErrNotFound
}

// ParseBlock returns any block in the chain. It does this even if the node does
// not hold the block, because a node can parse a block that it never received.
func (vm *VM) ParseBlock(_ context.Context, blkBytes []byte) (snowman.Block, error) {
	blk, ok := vm.byBytes[string(blkBytes)]
	if !ok {
		return nil, database.ErrNotFound
	}
	return vm.wrap(blk), nil
}

// LastAccepted returns the last accepted block.
func (vm *VM) LastAccepted(context.Context) (ids.ID, error) {
	return vm.lastAccepted.ID(), nil
}

// GetBlockIDAtHeight returns the ID of the accepted block at the given height.
func (vm *VM) GetBlockIDAtHeight(_ context.Context, height uint64) (ids.ID, error) {
	blk, ok := vm.acceptedByHeight[height]
	if !ok {
		return ids.Empty, database.ErrNotFound
	}
	return blk.ID(), nil
}

func (vm *VM) wrap(blk *snowmantest.Block) snowman.Block {
	return &block{Block: blk, vm: vm}
}

func (vm *VM) markAccepted(blk *snowmantest.Block) {
	delete(vm.verified, blk.ID())
	vm.accepted[blk.ID()] = blk
	vm.acceptedByHeight[blk.Height()] = blk
	if vm.lastAccepted == nil || blk.Height() > vm.lastAccepted.Height() {
		vm.lastAccepted = blk
	}
}

// block is a [snowmantest.Block] whose verification and decision update the
// state of the [VM] that returned it.
type block struct {
	*snowmantest.Block
	vm *VM
}

func (b *block) Verify(ctx context.Context) error {
	_, parentAccepted := b.vm.accepted[b.Parent()]
	_, parentVerified := b.vm.verified[b.Parent()]
	if !parentAccepted && !parentVerified {
		return ErrUnknownParent
	}
	if b.vm.VerifyF != nil {
		if err := b.vm.VerifyF(b.Block); err != nil {
			return err
		}
	}
	if err := b.Block.Verify(ctx); err != nil {
		return err
	}
	b.vm.verified[b.ID()] = b.Block
	return nil
}

func (b *block) Accept(ctx context.Context) error {
	if err := b.Block.Accept(ctx); err != nil {
		return err
	}
	b.vm.markAccepted(b.Block)
	return nil
}

func (b *block) Reject(ctx context.Context) error {
	if err := b.Block.Reject(ctx); err != nil {
		return err
	}
	delete(b.vm.verified, b.ID())
	return nil
}

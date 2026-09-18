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

// ErrParentNotAccepted is returned by a block's Verify when the VM is
// configured with [VM.VerifyRequiresAcceptedParent] and the block's parent has
// not been accepted yet.
var ErrParentNotAccepted = errors.New("parent not accepted")

// VM is a fake [block.ChainVM].
type VM struct {
	blocktest.VM

	// Has reports if this node holds a block.
	// The node always holds its last accepted block.
	// If nil we have all blocks in the chain.
	Has func(*snowmantest.Block) bool

	// RequireAcceptedParentToVerify, when non-nil, gates block verification:
	// a block for which it returns true fails Verify with [ErrParentNotAccepted]
	// until its parent has been accepted.
	RequireAcceptedParentToVerify func(*snowmantest.Block) bool

	// LastAcceptedBlock is the block that LastAccepted returns.
	// If LastAcceptedBlock is nil, LastAccepted reports the highest accepted block in
	// the chain.
	LastAcceptedBlock *snowmantest.Block

	byID            map[ids.ID]*snowmantest.Block
	byBytes         map[string]*snowmantest.Block
	byHeight        map[uint64]*snowmantest.Block
	highestAccepted *snowmantest.Block
}

// NewVM returns a [VM] for the given chain. The heights must ascend and be contiguous.
func NewVM(t *testing.T, chain []*snowmantest.Block) *VM {
	vm := &VM{
		byID:            make(map[ids.ID]*snowmantest.Block, len(chain)+1),
		byBytes:         make(map[string]*snowmantest.Block, len(chain)+1),
		byHeight:        make(map[uint64]*snowmantest.Block, len(chain)+1),
		highestAccepted: snowmantest.Genesis,
	}
	for _, blk := range chain {
		vm.byID[blk.ID()] = blk
		vm.byBytes[string(blk.Bytes())] = blk
		vm.byHeight[blk.Height()] = blk
	}
	// The genesis block is always available, even if the caller left it out of the
	// chain.
	vm.byID[snowmantest.GenesisID] = snowmantest.Genesis
	vm.byBytes[string(snowmantest.Genesis.Bytes())] = snowmantest.Genesis
	vm.byHeight[snowmantest.Genesis.Height()] = snowmantest.Genesis

	vm.T = t
	vm.Default(true)
	vm.CantSetState = false
	vm.CantSetPreference = false
	return vm
}

// GetBlock returns a block if this node holds it.
func (vm *VM) GetBlock(_ context.Context, blkID ids.ID) (snowman.Block, error) {
	blk, ok := vm.byID[blkID]
	if !ok {
		return nil, database.ErrNotFound
	}
	if !vm.has(blk) {
		return nil, database.ErrNotFound
	}
	return vm.wrap(blk), nil
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

// wrap returns [blk] as-is, or, if the VM is configured to gate its
// verification on parent acceptance, wrapped so that Verify enforces that.
func (vm *VM) wrap(blk *snowmantest.Block) snowman.Block {
	if vm.RequireAcceptedParentToVerify != nil && vm.RequireAcceptedParentToVerify(blk) {
		return &acceptGatedBlock{Block: blk, vm: vm}
	}
	return blk
}

// acceptGatedBlock is a block whose Verify only succeeds once its parent has
// been accepted, modeling a VM that verifies against accepted parent state.
type acceptGatedBlock struct {
	*snowmantest.Block
	vm *VM
}

func (b *acceptGatedBlock) Verify(ctx context.Context) error {
	if parent, ok := b.vm.byID[b.Parent()]; ok && parent.Status != snowtest.Accepted {
		return ErrParentNotAccepted
	}
	return b.Block.Verify(ctx)
}

// LastAccepted reports [VM.LastAcceptedBlock]. If [VM.LastAcceptedBlock] is nil,
// LastAccepted reports the highest accepted block in the chain.
func (vm *VM) LastAccepted(context.Context) (ids.ID, error) {
	return vm.lastAccepted().ID(), nil
}

// GetBlockIDAtHeight returns the ID of the block at the given height.
func (vm *VM) GetBlockIDAtHeight(_ context.Context, height uint64) (ids.ID, error) {
	blk, ok := vm.byHeight[height]
	if !ok || !vm.has(blk) {
		return ids.Empty, database.ErrNotFound
	}
	return blk.ID(), nil
}

func (vm *VM) lastAccepted() *snowmantest.Block {
	if vm.LastAcceptedBlock != nil {
		return vm.LastAcceptedBlock
	}

	// The engine accepts blocks in ascending height order. The search therefore
	// starts at the highest accepted block that it found before, and does not read
	// the chain again.
	for {
		next, ok := vm.byHeight[vm.highestAccepted.Height()+1]
		if !ok || next.Status != snowtest.Accepted {
			return vm.highestAccepted
		}
		vm.highestAccepted = next
	}
}

func (vm *VM) has(blk *snowmantest.Block) bool {
	if blk.ID() == vm.lastAccepted().ID() {
		return true
	}
	return vm.Has == nil || vm.Has(blk)
}

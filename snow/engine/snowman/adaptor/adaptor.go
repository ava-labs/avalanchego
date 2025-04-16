// Package adaptor provides a generic alternative to the Snowman [block.ChainVM]
// interface, which doesn't require the block to be aware of the VM
// implementation.
package adaptor

import (
	"context"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
)

// ChainVM defines the required functionality to be converted into a Snowman VM.
// See the respective methods on [block.ChainVM] and [snowman.Block] for
// detailed documentation.
type ChainVM[B Block] interface {
	common.VM

	GetBlock(context.Context, ids.ID) (B, error)
	ParseBlock(context.Context, []byte) (B, error)
	BuildBlock(context.Context) (B, error)

	// Transferred from [snowman.Block].
	VerifyBlock(context.Context, B) error
	AcceptBlock(context.Context, B) error
	RejectBlock(context.Context, B) error

	SetPreference(context.Context, ids.ID) error
	LastAccepted(context.Context) (ids.ID, error)
	GetBlockIDAtHeight(context.Context, uint64) (ids.ID, error)
}

// Block is a read-only subset of [snowman.Block]. The state-modifying methods
// required by Snowman consensus are, instead, present on [ChainVM].
type Block interface {
	ID() ids.ID
	Parent() ids.ID
	Bytes() []byte
	Height() uint64
	Timestamp() time.Time
}

// Convert transforms a generic [ChainVM] into a standard [block.ChainVM].
func Convert[B Block](vm ChainVM[B]) block.ChainVM {
	return &adaptor[B]{vm}
}

type adaptor[B Block] struct {
	ChainVM[B]
}

func (vm adaptor[B]) newBlock(b B, err error) (snowman.Block, error) {
	if err != nil {
		return nil, err
	}
	return blk[B]{b, vm.ChainVM}, nil
}

func (vm adaptor[B]) GetBlock(ctx context.Context, blkID ids.ID) (snowman.Block, error) {
	return vm.newBlock(vm.ChainVM.GetBlock(ctx, blkID))
}

func (vm adaptor[B]) ParseBlock(ctx context.Context, blockBytes []byte) (snowman.Block, error) {
	return vm.newBlock(vm.ChainVM.ParseBlock(ctx, blockBytes))
}

func (vm adaptor[B]) BuildBlock(ctx context.Context) (snowman.Block, error) {
	return vm.newBlock(vm.ChainVM.BuildBlock(ctx))
}

type blk[B Block] struct {
	b  B
	vm ChainVM[B]
}

func (b blk[B]) Verify(ctx context.Context) error { return b.vm.VerifyBlock(ctx, b.b) }
func (b blk[B]) Accept(ctx context.Context) error { return b.vm.AcceptBlock(ctx, b.b) }
func (b blk[B]) Reject(ctx context.Context) error { return b.vm.RejectBlock(ctx, b.b) }

func (b blk[B]) ID() ids.ID           { return b.b.ID() }
func (b blk[B]) Parent() ids.ID       { return b.b.Parent() }
func (b blk[B]) Bytes() []byte        { return b.b.Bytes() }
func (b blk[B]) Height() uint64       { return b.b.Height() }
func (b blk[B]) Timestamp() time.Time { return b.b.Timestamp() }

// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sae

import (
	"context"
	"fmt"

	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/event"
	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/vms/saevm/blocks"
)

// SetPreference updates the VM's currently [preferred block] with the given block context,
// which MAY be nil.
//
// [preferred block]: https://github.com/ava-labs/avalanchego/tree/master/vms#set-preference
func (vm *VM) SetPreference(ctx context.Context, id ids.ID, _ *block.Context) error {
	b, err := vm.GetBlock(ctx, id)
	if err != nil {
		return err
	}
	vm.preference.Store(b)
	return nil
}

// AcceptBlock marks the block as [accepted], resulting in:
//   - All blocks settled by this block having their [blocks.Block.MarkSettled]
//     method called; and
//   - The block being propagated to [saexec.Executor.Enqueue].
//
// [accepted]: https://github.com/ava-labs/avalanchego/tree/master/vms#block-statuses
func (vm *VM) AcceptBlock(ctx context.Context, b *blocks.Block) error {
	// Recall the terminology and ordering from the invariants document:
	// - (D)isk then (M)emory then (I)nternal then e(X)ternal.
	// - B accepted after all of B.Settles() settled

	settles := b.Settles()
	{
		batch := vm.db.NewBatch()

		// D(Σ ⊂ S); yes, I know it's in a batch
		if len(settles) > 0 {
			// Note that while we treat SAFE and FINALIZED identically, there is
			// no notion of the former in [rawdb]. Furthermore, the LAST label
			// is reserved for the last executed so it too isn't persisted here.
			rawdb.WriteFinalizedBlockHash(batch, b.LastSettled().Hash())
		}

		rawdb.WriteBlock(batch, b.EthBlock())
		rawdb.WriteTxLookupEntriesByBlock(batch, b.EthBlock())
		// D(b ∈ A)
		rawdb.WriteCanonicalHash(batch, b.Hash(), b.NumberU64())

		if err := batch.Write(); err != nil {
			return err
		}
	}

	// If the block being accepted settles its parent then we will lose access
	// to the ancestry, which is needed at the end of this method to avoid
	// leaking blocks by keeping them in the in-memory store.
	parentLastSettled := b.ParentBlock().LastSettled()

	// f(b_{n-1}) before f(b_n)
	//
	// [blocks.Block.MarkSettled] guarantees M before I (i.e. `vm.last.settled`)
	for _, s := range settles {
		if err := s.MarkSettled(&vm.last.settled); err != nil {
			return err
		}
	}

	// I(s ∈ S) above, before I(b ∈ A) before X(b ∈ A)
	vm.last.accepted.Store(b)
	vm.acceptedBlocks.Send(b)
	if err := vm.exec.Enqueue(ctx, b); err != nil {
		return err
	}

	// When the chain is bootstrapping, avalanchego expects to be able to call
	// `Verify` and `Accept` in a loop over blocks. Reporting an error during
	// either `Verify` or `Accept` is considered FATAL during this process.
	// Therefore, we must ensure that avalanchego does not get too far ahead of
	// the execution thread and FATAL during block verification.
	if vm.consensusState.Get() == snow.Bootstrapping {
		if err := b.WaitUntilExecuted(ctx); err != nil {
			return fmt.Errorf("waiting for block %d to execute: %v", b.Height(), err)
		}
	}

	vm.log().Debug(
		"Accepted block",
		zap.Uint64("height", b.Height()),
		zap.Stringer("hash", b.Hash()),
	)

	// Same rationale as the invariant described in [blocks.Block]. Praised be
	// the GC!
	// The executor's [saedb.Tracker] handles removing state roots once the reference
	// count is 0. Since on execution, each root has a reference added,
	// this count is decremented once the block's state is no longer needed by
	// consensus.
	keep := b.LastSettled().Hash()
	for _, s := range settles {
		if s.Hash() == keep {
			continue
		}
		vm.consensusCritical.Delete(s.Hash())
	}
	if h := parentLastSettled.Hash(); h != keep { // i.e. `parentLastSettled` was the last block's `keep`
		vm.consensusCritical.Delete(h)
	}
	return nil
}

// LastAccepted returns the ID of the last block received by [VM.AcceptBlock].
func (vm *VM) LastAccepted(context.Context) (ids.ID, error) {
	return vm.last.accepted.Load().ID(), nil
}

// RejectBlock is a no-op in SAE because execution only occurs after acceptance.
func (vm *VM) RejectBlock(ctx context.Context, b *blocks.Block) error {
	vm.consensusCritical.Delete(b.Hash())
	return nil
}

// SubscribeAcceptedBlocks returns a new subscription for each [*blocks.Block]
// emitted after consensus acceptance via [VM.AcceptBlock].
func (vm *VM) SubscribeAcceptedBlocks(ch chan<- *blocks.Block) event.Subscription {
	return vm.acceptedBlocks.Subscribe(ch)
}

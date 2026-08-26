// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package blocks

import (
	"errors"
	"fmt"
	"slices"
	"sync/atomic"
	"time"

	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/vms/components/gas"
	"github.com/ava-labs/avalanchego/vms/saevm/proxytime"
)

var errBlockResettled = errors.New("block re-settled")

// MarkSettled marks the block as having been settled. This function MUST NOT be
// called more than once. The atomic pointer to the last-settled block is
// updated before [Block.WaitUntilSettled] returns.
//
// After a call to MarkSettled, future calls to [Block.ParentBlock] and
// [Block.LastSettled] will return nil.
func (b *Block) MarkSettled(lastSettled *atomic.Pointer[Block]) error {
	if lastSettled == nil {
		return errors.New("atomic pointer to last-settled block MUST NOT be nil")
	}
	return b.markSettled(lastSettled)
}

func (b *Block) markSettled(lastSettled *atomic.Pointer[Block]) error {
	if b.Settled() {
		b.log.Error(errBlockResettled.Error())
		return fmt.Errorf("%w: block height %d", errBlockResettled, b.Height())
	}
	b.parent.Store(nil)

	if lastSettled != nil {
		lastSettled.Store(b)
	}
	close(b.settled)
	return nil
}

// Settled reports whether [Block.MarkSettled] has been called without resulting
// in an error, or the block was constructed by [RestoreSettledBlock].
func (b *Block) Settled() bool {
	select {
	case <-b.settled:
		return true
	default:
		return false
	}
}

// Synchronous reports whether the block was marked as synchronous during
// [RestoreSettledBlock] or [Block.RestoreExecutionArtefacts].
func (b *Block) Synchronous() bool {
	return b.synchronous
}

const (
	getParentOfSettledErrMsg  = "Get parent of settled block"
	getSettledOfSettledErrMsg = getParentOfSettledErrMsg + " while finding last-settled"
)

// ParentBlock returns the block's parent unless [Block.MarkSettled] has been
// called, in which case it returns nil and logs an error.
func (b *Block) ParentBlock() *Block {
	p := b.parent.Load()
	if p == nil && b.Settled() {
		b.log.Error(getParentOfSettledErrMsg)
	}
	return p
}

// settledHeight returns the height of the block that b settles, as recorded in
// its header. A synchronous block settles itself, mirroring [Block.LastSettled]
// returning b, because the settlement marker of such a block is the zero value
// and hence conveys no height.
func (b *Block) settledHeight() uint64 {
	if b.Synchronous() {
		return b.Height()
	}
	return b.hooks.SettledBy(b.Header()).Height
}

// LastSettled returns the last-settled block at the time of b's acceptance.
//
// Settlement never regresses, so if an ancestor of b has already been settled at
// a height above the one recorded in b's header, that ancestor is returned
// instead; the ancestry of a settled block is severed so it is, by definition,
// the furthest that the lookback can reach. This is the case for every block
// building on the last pre-SAE block, which is restored in a settled state at a
// mid-chain transition to SAE.
//
// If [Block.MarkSettled] has been called on b itself, LastSettled returns nil
// and logs an error. It also returns nil, without logging, if the ancestry
// between b and the last-settled block is incomplete, which is only possible for
// a [Block] that hasn't had its parent set (see [Block.CopyParentFrom]).
//
// Note that this value might not be distinct between contiguous blocks. If the
// block is synchronous, LastSettled always returns b itself, without logging.
func (b *Block) LastSettled() *Block {
	if b.Synchronous() {
		return b
	}

	parent := b.parent.Load()
	if parent == nil {
		if b.Settled() {
			b.log.Error(getSettledOfSettledErrMsg)
		}
		return nil
	}
	return b.lastSettledFrom(parent)
}

// lastSettledFrom is the implementation of [Block.LastSettled] for a non-
// synchronous block with the specified (non-nil) parent.
func (b *Block) lastSettledFrom(parent *Block) *Block {
	settledHeight := b.settledHeight()
	for parent.Height() > settledHeight {
		// Settlement never regresses so an already-settled ancestor is the
		// last-settled block, irrespective of b claiming to settle a lower one.
		// This clamps the lookback at the last pre-SAE block, which is restored
		// in a settled state at a mid-chain transition and hence has a severed
		// ancestry.
		if parent.Settled() {
			return parent
		}
		parent = parent.ParentBlock() // a settled intermediate logs for itself
		if parent == nil {
			return nil
		}
	}
	return parent
}

// Settles returns the executed blocks that b settles if it is accepted by
// consensus. If `x` is the settled height recorded in the header of
// `b.ParentBlock()` and `y` is the height of the `b.LastSettled()`, then Settles
// returns the contiguous, half-open range (x,y] or an empty slice i.f.f. x==y.
// Every block therefore returns a disjoint (and possibly empty) set of
// historical blocks.
//
// The recorded height `x` is equivalent to `b.ParentBlock().LastSettled()`
// without the ancestry walk, and remains valid even once the parent's own
// ancestry has been severed (e.g. it was restored by [RestoreSettledBlock]).
// Unlike [Block.LastSettled] it is not clamped at an already-settled ancestor
// because block verification guarantees that the recorded height is that of the
// first settled block found when walking up from the parent, hence never below
// it; see the use of [LastToSettleAt] by, and the settled-height check in, the
// VM's block verification.
//
// An error is returned i.f.f. the last-settled block can't be determined, which
// is a broken invariant of the caller: it is not valid to call Settles after a
// call to [Block.MarkSettled] on either b or its parent, nor on a [Block] that
// hasn't had its parent set. As settlement is consensus-critical, the error MUST
// be treated as fatal rather than as "nothing to settle".
//
// If the block is synchronous, Settles always returns a single-element slice of
// `b` itself, and every block that would settle such a block therefore settles
// nothing.
func (b *Block) Settles() ([]*Block, error) {
	if b.Synchronous() {
		return []*Block{b}, nil
	}

	parent := b.ParentBlock() // logs if b was settled
	if parent == nil {
		return nil, fmt.Errorf("%w: parent of block %d", errIncompleteBlockHistory, b.Height())
	}

	end := b.lastSettledFrom(parent)
	switch {
	case end == nil:
		return nil, fmt.Errorf("%w: ancestry of block %d", errIncompleteBlockHistory, b.Height())
	case end.Settled():
		// A settled block can't be settled again, i.e. x==y. This is the case
		// for all blocks settling the last pre-SAE block, which settles itself
		// at a mid-chain transition.
		return nil, nil
	}
	return Range(parent.settledHeight(), end), nil
}

// Range returns the blocks in the continuous, half-open interval (start, end]
// in order of increasing height.
//
// The `start` block MAY be settled, but all other blocks in the range MUST NOT
// be settled. It is assumed that `start` can be reached by traversing up the
// chain from `end`.
//
// If the two arguments are the same block, Range returns an empty slice.
func Range(startHeight uint64, end *Block) []*Block {
	endHeight := end.Height()
	if endHeight <= startHeight {
		return nil
	}

	var (
		chain = make([]*Block, endHeight-startHeight)
		b     = end
	)
	for i := range chain {
		chain[i] = b
		b = b.ParentBlock()
	}
	slices.Reverse(chain)
	return chain
}

var errIncompleteBlockHistory = errors.New("incomplete block history when determining last-settled block")

// LastToSettleAt returns (a) the last block to be settled at time `settleAt` if
// building on the specified parent block, and (b) a boolean to indicate if
// settlement is currently possible. If the returned boolean is false, the
// execution stream is lagging and LastToSettleAt MAY be called again after some
// indeterminate delay.
//
// It is not valid to call LastToSettleAt with a parent on which
// [Block.MarkSettled] was called directly. However, it is valid with a
// synchronous parent.
//
// See the Example for [Block.WhenChildSettles] for one usage of the returned
// block.
func LastToSettleAt(settleAt time.Time, parent *Block) (b *Block, ok bool, _ error) {
	defer func() {
		// Avoids having to perform this check at every return.
		if !ok {
			b = nil
		}
	}()

	settleAtGasTime := proxytime.Of[gas.Gas](settleAt)

	// A block can be the last to settle at some time i.f.f. two criteria are
	// met:
	//
	// 1. The block has finished execution by said time and;
	//
	// 2. The block's child is known to have *not* finished execution or be
	//    unable to finish by that time.
	//
	// The block currently being built can never finish in time, so we start
	// with criterion (2) being met.
	known := true

	// The only way [Block.ParentBlock] can be nil is if `block` was already
	// settled (see invariant in [Block]). If a block was already settled then
	// only it or a later (i.e. unsettled) block can be returned by this loop,
	// therefore we have a guarantee that the loop update will never result in
	// `block==nil`.
	for block := parent; ; block = block.ParentBlock() {
		if block == nil {
			// Although the below [Block.Settled] check (performed in the last
			// loop iteration) precludes this from happening, that assumes no
			// settlement concurrently with a call to [LastToSettleAt]. While
			// that may be true now, the consequence of a race condition when
			// omitting this check would be a panic for nil-pointer
			// dereferencing.
			parent.log.Error(
				"Race condition when determining last block to settle",
				zap.Stringer("parent_hash", parent.Hash()),
				zap.Uint64("parent_height", parent.Height()),
				zap.Time("settle_at", settleAt),
			)
			return nil, false, fmt.Errorf("%w: settling at %v with parent %#x (%v)", errIncompleteBlockHistory, settleAt, parent.Hash(), parent.Number())
		}
		// Guarantees that the loop will always exit as the last pre-SAE block
		// (perhaps the genesis) is always settled, by definition.
		if block.Settled() {
			return block, known, nil
		}

		if startsNoEarlierThan := block.PreciseTime(); startsNoEarlierThan.Compare(settleAt) > 0 {
			known = true
			continue
		}
		if t := block.interimExecutionTime.Load(); t != nil && t.Compare(settleAtGasTime) > 0 {
			known = true
			continue
		}
		if e := block.execution.Load(); e != nil {
			if e.byGas.Compare(settleAtGasTime) > 0 {
				// There may have been a race between this check and the
				// interim-execution one above, so we have to check again.
				known = true
				continue
			}
			return block, known, nil
		}

		// TODO(arr4n) more fine-grained checks are possible by computing the
		// minimum possible gas consumption of blocks. For example,
		// `block.BuildTime()+block.intrinsicGasSum()` can be compared against
		// `equivSettleAt`, as can the sum of a chain of blocks.

		// Note that a grandchild block having unknown execution completion time
		// does not rule out knowing a child's completion time, so this could be
		// set to true in a future loop iteration.
		known = false
	}
}

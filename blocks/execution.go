// Copyright (C) 2025-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package blocks

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"slices"
	"sync/atomic"
	"time"

	"github.com/ava-labs/avalanchego/vms/components/gas"
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/ethdb"
	"github.com/ava-labs/libevm/params"
	"github.com/ava-labs/libevm/trie"
	"github.com/holiman/uint256"
	"go.uber.org/zap"

	"github.com/ava-labs/strevm/gastime"
	saeparams "github.com/ava-labs/strevm/params"
	"github.com/ava-labs/strevm/proxytime"
	saetypes "github.com/ava-labs/strevm/types"
)

// SetInterimExecutionTime is expected to be called during execution of b's
// transactions, with the highest-known gas time. This MAY be at any resolution
// but MUST be monotonic.
func (b *Block) SetInterimExecutionTime(t *proxytime.Time[gas.Gas]) {
	b.interimExecutionTime.Store(t.Clone())
}

//go:generate go run github.com/StephenButtolph/canoto/canoto $GOFILE

type executionResults struct {
	byGas         gastime.Time `canoto:"value,1"`
	baseFee       uint256.Int  `canoto:"fixed repeated uint,2"`
	receiptRoot   common.Hash  `canoto:"fixed bytes,3"`
	stateRootPost common.Hash  `canoto:"fixed bytes,4"`

	ephemeralExecutionResults // not for canotofication

	canotoData canotoData_executionResults
}

type ephemeralExecutionResults struct {
	// Wall-clock time is for metrics only and MAY be incorrect.
	byWall time.Time
	// Receipts are deliberately not stored by the canoto representation as they
	// are already in the database. All methods that read the stored canoto
	// either accept a [types.Receipts] for comparison against the
	// `receiptRoot`, or don't care about receipts at all. They are, however,
	// carried here for propagation to the settling block.
	receipts types.Receipts
}

func (e *executionResults) setBaseFee(bf *big.Int) error {
	if bf == nil { // genesis blocks
		return nil
	}
	if overflow := e.baseFee.SetFromBig(bf); overflow {
		return fmt.Errorf("base fee %v overflows 256 bits", bf)
	}
	return nil
}

// MarkExecuted marks the block as having been executed at the specified time(s)
// and with the specified results. It also sets the chain's head block to b. The
// [gastime.Time] MUST have already been scaled to the target applicable after
// the block, as defined by the relevant [hook.Points].
//
// MarkExecuted guarantees that state is persisted to the database before
// in-memory indicators of execution are updated. [Block.Executed] returning
// true and [Block.WaitUntilExecuted] returning cleanly are both therefore
// indicative of a successful database write by MarkExecuted. The atomic pointer
// to the last-executed block is updated before [Block.WaitUntilExecuted]
// returns.
//
// This method MUST NOT be called more than once. The wall-clock [time.Time] is
// for metrics only.
func (b *Block) MarkExecuted(
	db ethdb.Database,
	xdb saetypes.ExecutionResults,
	byGas *gastime.Time,
	byWall time.Time,
	baseFee *big.Int,
	receipts types.Receipts,
	stateRootPost common.Hash,
	lastExecuted *atomic.Pointer[Block],
) error {
	if it := b.interimExecutionTime.Load(); it != nil && byGas.Compare(it) < 0 {
		// The final execution time is scaled to the new gas target but interim
		// times are not, which can result in rounding errors. Scaling always
		// rounds up, to maintain a monotonic clock, but we confirm for safety.
		// The logger used in tests will also convert this to a failure.
		b.log.Error("Final execution gas time before last interim time",
			zap.Stringer("interim_time", it),
			zap.Stringer("final_time", byGas.Time),
		)
	}

	e := &executionResults{
		byGas:         *byGas.Clone(),
		receiptRoot:   types.DeriveSha(receipts, trie.NewStackTrie(nil)),
		stateRootPost: stateRootPost,
		ephemeralExecutionResults: ephemeralExecutionResults{
			byWall:   byWall,
			receipts: slices.Clone(receipts),
		},
	}
	if err := e.setBaseFee(baseFee); err != nil {
		return err
	}

	batch := db.NewBatch()
	rawdb.WriteReceipts(batch, b.Hash(), b.NumberU64(), receipts)
	return b.markExecuted(batch, xdb, e, true, lastExecuted)
}

var errMarkBlockExecutedAgain = errors.New("block re-marked as executed")

// markExecuted is the intersection point of [Block.MarkExecuted],
// [Block.MarkSynchronous], and [Block.RestoreExecutionArtefacts], all of which
// have side effects drawn from the same ordered set of events. This method
// exists to guarantee that the correct selection and ordering of events occurs,
// regardless of the upstream trigger. See documentation re ordering invariants
// for more information.
//
// The batch is `Write()`n (yeah, it's a word now) after all disk artefacts are
// persisted.
func (b *Block) markExecuted(batch ethdb.Batch, xdb saetypes.ExecutionResults, e *executionResults, setAsHeadBlock bool, lastExecuted *atomic.Pointer[Block]) error {
	if err := b.markExecutedOnDisk(batch, xdb, e, setAsHeadBlock); err != nil {
		return err
	}
	return b.markExecutedAfterDiskArtefacts(e, lastExecuted)
}

func (b *Block) markExecutedOnDisk(batch ethdb.Batch, xdb saetypes.ExecutionResults, e *executionResults, setAsHeadBlock bool) error {
	n := b.NumberU64()
	if err := xdb.Put(n, e.MarshalCanoto()); err != nil {
		return err
	}
	if err := xdb.Sync(n, n); err != nil {
		return err
	}
	if setAsHeadBlock {
		b.SetAsHeadBlock(batch)
	}
	return batch.Write()
}

func (b *Block) markExecutedAfterDiskArtefacts(e *executionResults, lastExecuted *atomic.Pointer[Block]) error {
	// Memory
	if !b.execution.CompareAndSwap(nil, e) {
		// This is fatal because we corrupted the database's head block if we
		// got here by [Block.MarkExecuted] being called twice (an invalid use
		// of the API).
		b.log.Fatal("Block re-marked as executed")
		return fmt.Errorf("%w: height %d", errMarkBlockExecutedAgain, b.Height())
	}
	// Internal indicator
	if lastExecuted != nil {
		lastExecuted.Store(b)
	}
	// External indicator
	close(b.executed)
	return nil
}

// SetAsHeadBlock calls all necessary [rawdb] write methods for setting the
// block as head. Said writes are not atomic and an [ethdb.Batch] SHOULD be used
// if this is a desired property.
func (b *Block) SetAsHeadBlock(kv ethdb.KeyValueWriter) {
	h := b.Hash()
	rawdb.WriteHeadBlockHash(kv, h)
	rawdb.WriteHeadHeaderHash(kv, h)
}

// WaitUntilExecuted blocks until [Block.MarkExecuted] is called or the
// [context.Context] is cancelled.
func (b *Block) WaitUntilExecuted(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-b.executed:
		return nil
	}
}

// Executed reports whether [Block.MarkExecuted] has been called without
// resulting in an error.
func (b *Block) Executed() bool {
	return b.execution.Load() != nil
}

// executionArtefact blocks until [Block.MarkExecuted] has been called and then
// returns the requested value. A warning is logged if the caller is blocked for
// longer than [saeparams.MaxQueueWallTime].
func executionArtefact[T any](b *Block, desc string, get func(*executionResults) T) T {
	select {
	case <-b.executed:
	case <-time.After(saeparams.MaxQueueWallTime):
		b.log.Warn("blocking on execution artefact longer than expected",
			zap.String("artefact", desc),
			zap.Duration("waited", saeparams.MaxQueueWallTime),
		)
		<-b.executed
	}

	return get(b.execution.Load())
}

func (e *executionResults) executedByGasTime() *gastime.Time    { return e.byGas.Clone() }
func (e *executionResults) executedByWallTime() time.Time       { return e.byWall }
func (e *executionResults) cloneBaseFee() *uint256.Int          { return e.baseFee.Clone() }
func (e *executionResults) cloneReceiptsSlice() types.Receipts  { return slices.Clone(e.receipts) }
func (e *executionResults) postExecutionStateRoot() common.Hash { return e.stateRootPost }

// ExecutedByGasTime blocks until [Block.MarkExecuted] has been called and
// returns a clone of the gas time passed to it.
func (b *Block) ExecutedByGasTime() *gastime.Time {
	return executionArtefact(b, "execution (gas) time", (*executionResults).executedByGasTime)
}

// ExecutedByWallTime blocks until [Block.MarkExecuted] has been called and
// returns the wall time passed to it.
func (b *Block) ExecutedByWallTime() time.Time {
	return executionArtefact(b, "execution (wall) time", (*executionResults).executedByWallTime)
}

// ExecutedBaseFee blocks until [Block.MarkExecuted] has been called and returns
// a clone of the base fee passed to it.
func (b *Block) ExecutedBaseFee() *uint256.Int {
	return executionArtefact(b, "baseFee", (*executionResults).cloneBaseFee)
}

// Receipts blocks until [Block.MarkExecuted] has been called and returns the
// receipts passed to it.
func (b *Block) Receipts() types.Receipts {
	return executionArtefact(b, "receipts", (*executionResults).cloneReceiptsSlice)
}

// PostExecutionStateRoot blocks until [Block.MarkExecuted] has been called and
// returns the state root passed to it.
func (b *Block) PostExecutionStateRoot() common.Hash {
	return executionArtefact(b, "state root", (*executionResults).postExecutionStateRoot)
}

// RestoreExecutionArtefacts reloads post-execution artefacts persisted by
// [Block.MarkExecuted] such that the block is in an equivalent state to when
// said function was originally called.
func (b *Block) RestoreExecutionArtefacts(db ethdb.Database, xdb saetypes.ExecutionResults, chainConfig *params.ChainConfig) error {
	e, err := loadExecutionResults(xdb, b.NumberU64())
	if err != nil {
		return err
	}
	e.receipts = rawdb.ReadRawReceipts(db, b.Hash(), b.NumberU64())
	if err := e.receipts.DeriveFields(
		chainConfig,
		b.Hash(),
		b.NumberU64(),
		b.BuildTime(),
		e.baseFee.ToBig(),
		nil, // SAE does not support blob transactions.
		b.Transactions(),
	); err != nil {
		return fmt.Errorf("deriving receipt fields: %v", err)
	}
	return b.markExecutedAfterDiskArtefacts(e, nil)
}

func loadExecutionResults(xdb saetypes.ExecutionResults, blockNum uint64) (*executionResults, error) {
	buf, err := xdb.Get(blockNum)
	if err != nil {
		return nil, err
	}
	e := new(executionResults)
	if err := e.UnmarshalCanoto(buf); err != nil {
		return nil, err
	}
	return e, nil
}

func persistedExecutionArtefact[T any](xdb saetypes.ExecutionResults, blockNum uint64, get func(*executionResults) T) (T, error) {
	e, err := loadExecutionResults(xdb, blockNum)
	if err != nil {
		var zero T
		return zero, err
	}
	return get(e), nil
}

// PostExecutionStateRoot mirrors the behaviour of
// [Block.RestoreExecutionArtefacts], without requiring a full [Block], and only
// returning the state root after execution.
func PostExecutionStateRoot(xdb saetypes.ExecutionResults, blockNum uint64) (common.Hash, error) {
	return persistedExecutionArtefact(xdb, blockNum, (*executionResults).postExecutionStateRoot)
}

// ExecutionBaseFee mirrors the behaviour of [Block.RestoreExecutionArtefacts],
// without requiring a full [Block], and only returning the base fee when the
// block was executed (as against the worst-case prediction).
func ExecutionBaseFee(xdb saetypes.ExecutionResults, blockNum uint64) (*uint256.Int, error) {
	return persistedExecutionArtefact(xdb, blockNum, (*executionResults).cloneBaseFee)
}

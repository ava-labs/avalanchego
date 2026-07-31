// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package blocks

import (
	"context"
	"errors"
	"fmt"
	"math"
	"math/big"
	"slices"
	"sync/atomic"
	"time"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/ethdb"
	"github.com/ava-labs/libevm/params"
	"github.com/ava-labs/libevm/trie"
	"github.com/holiman/uint256"
	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/vms/components/gas"
	"github.com/ava-labs/avalanchego/vms/saevm/gastime"
	"github.com/ava-labs/avalanchego/vms/saevm/hook"
	"github.com/ava-labs/avalanchego/vms/saevm/proxytime"

	saeparams "github.com/ava-labs/avalanchego/vms/saevm/params"
	saetypes "github.com/ava-labs/avalanchego/vms/saevm/types"
)

// SetInterimExecutionTime is expected to be called during execution of b's
// transactions, with the highest-known gas time. This MAY be at any resolution
// but MUST be monotonic.
func (b *Block) SetInterimExecutionTime(t *proxytime.Time[gas.Gas]) {
	b.interimExecutionTime.Store(t.Clone())
}

//go:generate go run github.com/StephenButtolph/canoto/canoto $GOFILE

//nolint:revive // struct-tag: canoto allows unexported fields
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

	if err := b.markExecutedOnDisk(batch, xdb, e); err != nil {
		return err
	}
	return b.markExecutedAfterDiskArtefacts(e, lastExecuted)
}

// markExecutedOnDisk updates the [saetypes.ExecutionResults] and the head block
// in the database. The batch is `Write()`n (yeah, it's a word now) after all
// disk artefacts are persisted.
func (b *Block) markExecutedOnDisk(batch ethdb.Batch, xdb saetypes.ExecutionResults, e *executionResults) error {
	n := b.NumberU64()
	if err := xdb.Put(n, e.MarshalCanoto()); err != nil {
		return err
	}
	if err := xdb.Sync(n, n); err != nil {
		return err
	}
	b.SetAsHeadBlock(batch)
	return batch.Write()
}

var errMarkBlockExecutedAgain = errors.New("block re-marked as executed")

// markExecutedAfterDiskArtefacts modifies b to allow readers of the block to see that the
// block has been executed. The caller MUST ensure that the execution results
// are first persisted to disk.
func (b *Block) markExecutedAfterDiskArtefacts(e *executionResults, lastExecuted *atomic.Pointer[Block]) error {
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
// said function was originally called.  If no execution results are found in
// the [saetypes.ExecutionResults], they are instead inferred from the
// block itself, and the block is marked as synchronous.
//
// This function does NOT restore the block's settlement state, even if the
// block is synchronous. The caller MUST mark the block as settled if and when
// appropriate. Because this function breaks this invariant, any consumer
// SHOULD consider using [RestoreSettledBlock] instead, if possible.
//
// Any error returned corrupts the block's in-memory state.
func (b *Block) RestoreExecutionArtefacts(hooks hook.Points, db ethdb.Database, xdb saetypes.ExecutionResults, chainConfig *params.ChainConfig) error {
	e, err := loadExecutionResults(xdb, b.NumberU64())
	if errors.Is(err, database.ErrNotFound) {
		e, err = b.synchronousExecutionResults(hooks)
		b.synchronous = true
	}
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

// synchronousExecutionResults derives the post-execution artefacts of a
// synchronous block. Unlike asynchronously executed blocks, synchronous blocks
// do not persist their execution results in the [saetypes.ExecutionResults]
// database, thus they are extracted from the header.
func (b *Block) synchronousExecutionResults(hooks hook.Points) (*executionResults, error) {
	// Target, excess, and config _after_ are a requirement of
	// [Block.MarkExecuted], as provided by [Block.WorstCaseGasTime].
	execTime, err := b.WorstCaseGasTime(hooks)
	if err != nil {
		return nil, err
	}

	ethB := b.EthBlock()
	e := &executionResults{
		byGas:         *execTime.Clone(),
		receiptRoot:   ethB.ReceiptHash(),
		stateRootPost: ethB.Root(),
		// receipts are populated in [Block.restoreExecutionArtefacts], which
		// calls this method, because this logic is shared.
	}
	e.baseFee.SetUint64(b.headerBaseFee())
	return e, nil
}

// WorstCaseGasTime reconstructs the worst-case gas time that the block
// committed to, from its base fee and the gas config after the block.
func (b *Block) WorstCaseGasTime(hooks hook.Points) (*gastime.Time, error) {
	hdr := b.Header()
	target, cfg := hooks.GasConfigAfter(hdr)
	return gastime.New(
		hooks.BlockTime(hdr),
		target,
		gas.Price(b.headerBaseFee()),
		cfg,
	)
}

// headerBaseFee returns the block's base fee, which MAY be nil (a pre-SAE
// header). The base fee is capped at [math.MaxUint64] but any reasonable
// implementation has a base fee much less than [math.MaxUint64].
func (b *Block) headerBaseFee() uint64 {
	switch bf := b.EthBlock().BaseFee(); {
	case bf == nil:
		return 0
	case bf.IsUint64():
		return bf.Uint64()
	default:
		return math.MaxUint64
	}
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

// PostExecutionStateRoot returns the post-execution state root of a block,
// without requiring a full [Block].
func PostExecutionStateRoot(xdb saetypes.ExecutionResults, blockNum uint64) (common.Hash, error) {
	return persistedExecutionArtefact(xdb, blockNum, (*executionResults).postExecutionStateRoot)
}

// ExecutionBaseFee returns the base fee after execution of the block without
// requiring a full [Block]. It returns the base fee when the block was executed
// (as against the worst-case prediction).
func ExecutionBaseFee(xdb saetypes.ExecutionResults, blockNum uint64) (*uint256.Int, error) {
	return persistedExecutionArtefact(xdb, blockNum, (*executionResults).cloneBaseFee)
}

// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package executor

import (
	"errors"
	"fmt"
	"time"

	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/chains/atomic"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/platformvm/block"
	"github.com/ava-labs/avalanchego/vms/platformvm/config"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
	"github.com/ava-labs/avalanchego/vms/platformvm/status"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs/executor"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs/fee"

	commonfees "github.com/ava-labs/avalanchego/vms/components/fees"
)

var (
	_ block.Visitor = (*verifier)(nil)

	ErrConflictingBlockTxs = errors.New("block contains conflicting transactions")

	errApricotBlockIssuedAfterFork           = errors.New("apricot block issued after fork")
	errBanffStandardBlockWithoutChanges      = errors.New("BanffStandardBlock performs no state changes")
	errIncorrectBlockHeight                  = errors.New("incorrect block height")
	errChildBlockEarlierThanParent           = errors.New("proposed timestamp before current chain time")
	errOptionBlockTimestampNotMatchingParent = errors.New("option block proposed timestamp not matching parent block one")
)

// verifier handles the logic for verifying a block.
type verifier struct {
	*backend
	txExecutorBackend *executor.Backend
}

func (v *verifier) BanffAbortBlock(b *block.BanffAbortBlock) error {
	if err := v.banffOptionBlock(b); err != nil {
		return err
	}
	return v.abortBlock(b)
}

func (v *verifier) BanffCommitBlock(b *block.BanffCommitBlock) error {
	if err := v.banffOptionBlock(b); err != nil {
		return err
	}
	return v.commitBlock(b)
}

func (v *verifier) BanffProposalBlock(b *block.BanffProposalBlock) error {
	if err := v.banffNonOptionBlock(b); err != nil {
		return err
	}

	parentID := b.Parent()
	onDecisionState, err := state.NewDiff(parentID, v.backend)
	if err != nil {
		return err
	}

	// retrieve parent block time before moving time forward
	parentBlkTime := onDecisionState.GetTimestamp()

	// Advance the time to [nextChainTime].
	nextChainTime := b.Timestamp()
	if _, err := executor.AdvanceTimeTo(v.txExecutorBackend, onDecisionState, nextChainTime); err != nil {
		return err
	}

	inputs, feesMan, atomicRequests, onAcceptFunc, standardFees, err := v.processStandardTxs(
		b.Transactions,
		onDecisionState,
		b.Parent(),
		parentBlkTime,
		b.Timestamp(),
	)
	if err != nil {
		return err
	}

	onCommitState, err := state.NewDiffOn(onDecisionState)
	if err != nil {
		return err
	}

	onAbortState, err := state.NewDiffOn(onDecisionState)
	if err != nil {
		return err
	}

	proposalFees, err := v.proposalBlock(
		&b.ApricotProposalBlock,
		onDecisionState,
		onCommitState,
		onAbortState,
		feesMan,
		inputs,
		atomicRequests,
		onAcceptFunc,
	)
	if err != nil {
		return err
	}

	v.ctx.Log.Info(
		"BLOCK COMPLEXITY",
		zap.Stringer("blkID", b.ID()),
		zap.Uint64("blkHeight", b.Height()),
		zap.Uint64("total block fees", standardFees+proposalFees),
		zap.Any("feeRates", feesMan.GetFeeRates()),
		zap.Any("consumedUnits", feesMan.GetCumulatedComplexity()),
	)
	return nil
}

func (v *verifier) BanffStandardBlock(b *block.BanffStandardBlock) error {
	if err := v.banffNonOptionBlock(b); err != nil {
		return err
	}

	parentID := b.Parent()
	onAcceptState, err := state.NewDiff(parentID, v.backend)
	if err != nil {
		return err
	}

	// retrieve parent block time before moving time forward
	parentBlkTime := onAcceptState.GetTimestamp()

	// Advance the time to [b.Timestamp()].
	changed, err := executor.AdvanceTimeTo(
		v.txExecutorBackend,
		onAcceptState,
		b.Timestamp(),
	)
	if err != nil {
		return err
	}

	// If this block doesn't perform any changes, then it should never have been
	// issued.
	if !changed && len(b.Transactions) == 0 {
		return errBanffStandardBlockWithoutChanges
	}

	return v.standardBlock(&b.ApricotStandardBlock, parentBlkTime, b.Timestamp(), onAcceptState)
}

func (v *verifier) ApricotAbortBlock(b *block.ApricotAbortBlock) error {
	if err := v.apricotCommonBlock(b); err != nil {
		return err
	}
	return v.abortBlock(b)
}

func (v *verifier) ApricotCommitBlock(b *block.ApricotCommitBlock) error {
	if err := v.apricotCommonBlock(b); err != nil {
		return err
	}
	return v.commitBlock(b)
}

func (v *verifier) ApricotProposalBlock(b *block.ApricotProposalBlock) error {
	if err := v.apricotCommonBlock(b); err != nil {
		return err
	}

	parentID := b.Parent()
	onCommitState, err := state.NewDiff(parentID, v.backend)
	if err != nil {
		return err
	}
	onAbortState, err := state.NewDiff(parentID, v.backend)
	if err != nil {
		return err
	}

	feeRates, err := onCommitState.GetFeeRates()
	if err != nil {
		return err
	}

	// I CANNOT UPDATE THE feeRates for proposal blocks since they don't have timestamp pre Banff
	// So I just keep the fee Rates I have
	// My goal is to give an idea of how the system behave under stress and proposal blocks
	// does not seem to cause much load
	feeMan := commonfees.NewManager(feeRates)
	proposalFees, err := v.proposalBlock(b, nil, onCommitState, onAbortState, feeMan, nil, nil, nil)
	if err != nil {
		return err
	}

	v.ctx.Log.Info(
		"BLOCK COMPLEXITY",
		zap.Stringer("blkID", b.ID()),
		zap.Uint64("blkHeight", b.Height()),
		zap.Uint64("total block fees", proposalFees),
		zap.Any("feeRates", feeMan.GetFeeRates()),
		zap.Any("consumedUnits", feeMan.GetCumulatedComplexity()),
	)
	return nil
}

func (v *verifier) ApricotStandardBlock(b *block.ApricotStandardBlock) error {
	if err := v.apricotCommonBlock(b); err != nil {
		return err
	}

	parentID := b.Parent()
	onAcceptState, err := state.NewDiff(parentID, v)
	if err != nil {
		return err
	}

	return v.standardBlock(b, time.Time{}, time.Time{}, onAcceptState)
}

func (v *verifier) ApricotAtomicBlock(b *block.ApricotAtomicBlock) error {
	// We call [commonBlock] here rather than [apricotCommonBlock] because below
	// this check we perform the more strict check that ApricotPhase5 isn't
	// activated.
	if err := v.commonBlock(b); err != nil {
		return err
	}

	parentID := b.Parent()
	currentTimestamp := v.getTimestamp(parentID)
	cfg := v.txExecutorBackend.Config
	if cfg.IsApricotPhase5Activated(currentTimestamp) {
		return fmt.Errorf(
			"the chain timestamp (%d) is after the apricot phase 5 time (%d), hence atomic transactions should go through the standard block",
			currentTimestamp.Unix(),
			cfg.ApricotPhase5Time.Unix(),
		)
	}

	feeMan := commonfees.NewManager(commonfees.Empty)
	atomicExecutor := executor.AtomicTxExecutor{
		Backend:       v.txExecutorBackend,
		BlkFeeManager: feeMan,
		ParentID:      parentID,
		StateVersions: v,
		Tx:            b.Tx,
	}

	if err := b.Tx.Unsigned.Visit(&atomicExecutor); err != nil {
		txID := b.Tx.ID()
		v.MarkDropped(txID, err) // cache tx as dropped
		return fmt.Errorf("tx %s failed semantic verification: %w", txID, err)
	}

	atomicExecutor.OnAccept.AddTx(b.Tx, status.Committed)

	if err := v.verifyUniqueInputs(parentID, atomicExecutor.Inputs); err != nil {
		return err
	}

	v.Mempool.Remove(b.Tx)

	blkID := b.ID()
	v.blkIDToState[blkID] = &blockState{
		statelessBlock: b,

		onAcceptState: atomicExecutor.OnAccept,

		inputs:          atomicExecutor.Inputs,
		timestamp:       atomicExecutor.OnAccept.GetTimestamp(),
		blockComplexity: feeMan.GetCumulatedComplexity(),
		atomicRequests:  atomicExecutor.AtomicRequests,
	}
	return nil
}

func (v *verifier) banffOptionBlock(b block.BanffBlock) error {
	if err := v.commonBlock(b); err != nil {
		return err
	}

	// Banff option blocks must be uniquely generated from the
	// BanffProposalBlock. This means that the timestamp must be
	// standardized to a specific value. Therefore, we require the timestamp to
	// be equal to the parents timestamp.
	parentID := b.Parent()
	parentBlkTime := v.getTimestamp(parentID)
	blkTime := b.Timestamp()
	if !blkTime.Equal(parentBlkTime) {
		return fmt.Errorf(
			"%w parent block timestamp (%s) option block timestamp (%s)",
			errOptionBlockTimestampNotMatchingParent,
			parentBlkTime,
			blkTime,
		)
	}
	return nil
}

func (v *verifier) banffNonOptionBlock(b block.BanffBlock) error {
	if err := v.commonBlock(b); err != nil {
		return err
	}

	parentID := b.Parent()
	parentState, ok := v.GetState(parentID)
	if !ok {
		return fmt.Errorf("%w: %s", state.ErrMissingParentState, parentID)
	}

	newChainTime := b.Timestamp()
	parentChainTime := parentState.GetTimestamp()
	if newChainTime.Before(parentChainTime) {
		return fmt.Errorf(
			"%w: proposed timestamp (%s), chain time (%s)",
			errChildBlockEarlierThanParent,
			newChainTime,
			parentChainTime,
		)
	}

	nextStakerChangeTime, err := state.GetNextStakerChangeTime(parentState)
	if err != nil {
		return fmt.Errorf("could not verify block timestamp: %w", err)
	}

	now := v.txExecutorBackend.Clk.Time()
	return executor.VerifyNewChainTime(
		newChainTime,
		nextStakerChangeTime,
		now,
	)
}

func (v *verifier) apricotCommonBlock(b block.Block) error {
	// We can use the parent timestamp here, because we are guaranteed that the
	// parent was verified. Apricot blocks only update the timestamp with
	// AdvanceTimeTxs. This means that this block's timestamp will be equal to
	// the parent block's timestamp; unless this is a CommitBlock. In order for
	// the timestamp of the CommitBlock to be after the Banff activation,
	// the parent ApricotProposalBlock must include an AdvanceTimeTx with a
	// timestamp after the Banff timestamp. This is verified not to occur
	// during the verification of the ProposalBlock.
	parentID := b.Parent()
	timestamp := v.getTimestamp(parentID)
	if v.txExecutorBackend.Config.IsBanffActivated(timestamp) {
		return fmt.Errorf("%w: timestamp = %s", errApricotBlockIssuedAfterFork, timestamp)
	}
	return v.commonBlock(b)
}

func (v *verifier) commonBlock(b block.Block) error {
	parentID := b.Parent()
	parent, err := v.GetBlock(parentID)
	if err != nil {
		return err
	}

	expectedHeight := parent.Height() + 1
	height := b.Height()
	if expectedHeight != height {
		return fmt.Errorf(
			"%w expected %d, but found %d",
			errIncorrectBlockHeight,
			expectedHeight,
			height,
		)
	}
	return nil
}

// abortBlock populates the state of this block if [nil] is returned
func (v *verifier) abortBlock(b block.Block) error {
	parentID := b.Parent()
	onAbortState, ok := v.getOnAbortState(parentID)
	if !ok {
		return fmt.Errorf("%w: %s", state.ErrMissingParentState, parentID)
	}

	blkID := b.ID()
	v.blkIDToState[blkID] = &blockState{
		statelessBlock: b,
		onAcceptState:  onAbortState,
		timestamp:      onAbortState.GetTimestamp(),

		// blockComplexity not set. We'll assign same complexity
		// as proposal blocks upon acceptance
	}
	return nil
}

// commitBlock populates the state of this block if [nil] is returned
func (v *verifier) commitBlock(b block.Block) error {
	parentID := b.Parent()
	onCommitState, ok := v.getOnCommitState(parentID)
	if !ok {
		return fmt.Errorf("%w: %s", state.ErrMissingParentState, parentID)
	}

	blkID := b.ID()
	v.blkIDToState[blkID] = &blockState{
		statelessBlock: b,
		onAcceptState:  onCommitState,
		timestamp:      onCommitState.GetTimestamp(),

		// blockComplexity not set. We'll assign same complexity
		// as proposal blocks upon acceptance
	}
	return nil
}

// proposalBlock populates the state of this block if [nil] is returned
func (v *verifier) proposalBlock(
	b *block.ApricotProposalBlock,
	onDecisionState state.Diff,
	onCommitState state.Diff,
	onAbortState state.Diff,
	feesMan *commonfees.Manager,
	inputs set.Set[ids.ID],
	atomicRequests map[ids.ID]*atomic.Requests,
	onAcceptFunc func(),
) (
	uint64, // cumulated block fees
	error,
) {
	txExecutor := executor.ProposalTxExecutor{
		OnCommitState: onCommitState,
		OnAbortState:  onAbortState,
		Backend:       v.txExecutorBackend,
		BlkFeeManager: feesMan,
		Tx:            b.Tx,
	}

	if err := b.Tx.Unsigned.Visit(&txExecutor); err != nil {
		txID := b.Tx.ID()
		v.MarkDropped(txID, err) // cache tx as dropped
		return txExecutor.BaseFee, err
	}

	onCommitState.AddTx(b.Tx, status.Committed)
	onAbortState.AddTx(b.Tx, status.Aborted)

	v.Mempool.Remove(b.Tx)

	blkID := b.ID()
	v.blkIDToState[blkID] = &blockState{
		proposalBlockState: proposalBlockState{
			onDecisionState: onDecisionState,
			onCommitState:   onCommitState,
			onAbortState:    onAbortState,
		},

		statelessBlock: b,

		onAcceptFunc: onAcceptFunc,

		inputs: inputs,
		// It is safe to use [b.onAbortState] here because the timestamp will
		// never be modified by an Apricot Abort block and the timestamp will
		// always be the same as the Banff Proposal Block.
		timestamp:       onAbortState.GetTimestamp(),
		blockComplexity: feesMan.GetCumulatedComplexity(),
		atomicRequests:  atomicRequests,
	}
	return txExecutor.BaseFee, nil
}

// standardBlock populates the state of this block if [nil] is returned
func (v *verifier) standardBlock(
	b *block.ApricotStandardBlock,
	parentBlkTime time.Time,
	blkTimestamp time.Time,
	onAcceptState state.Diff,
) error {
	inputs, feeMan, atomicRequests, onAcceptFunc, totalBlockFees, err := v.processStandardTxs(
		b.Transactions,
		onAcceptState,
		b.Parent(),
		parentBlkTime,
		blkTimestamp,
	)
	if err != nil {
		return err
	}

	v.ctx.Log.Info(
		"BLOCK COMPLEXITY",
		zap.Stringer("blkID", b.ID()),
		zap.Uint64("blkHeight", b.Height()),
		zap.Uint64("total block fees", totalBlockFees),
		zap.Any("feeRates", feeMan.GetFeeRates()),
		zap.Any("consumedUnits", feeMan.GetCumulatedComplexity()),
	)

	v.Mempool.Remove(b.Transactions...)

	blkID := b.ID()
	v.blkIDToState[blkID] = &blockState{
		statelessBlock: b,

		onAcceptState: onAcceptState,
		onAcceptFunc:  onAcceptFunc,

		timestamp:       onAcceptState.GetTimestamp(),
		blockComplexity: feeMan.GetCumulatedComplexity(),
		inputs:          inputs,
		atomicRequests:  atomicRequests,
	}
	return nil
}

func (v *verifier) processStandardTxs(
	txs []*txs.Tx,
	state state.Diff,
	parentID ids.ID,
	parentBlkTime, blkTimestamp time.Time,
) (
	set.Set[ids.ID],
	*commonfees.Manager,
	map[ids.ID]*atomic.Requests,
	func(),
	uint64, // total block fees
	error,
) {
	// feeRates, err := state.GetFeeRates()
	// if err != nil {
	// 	return nil, nil, nil, nil, 0, err
	// }

	var (
		isEActive = v.txExecutorBackend.Config.IsEActivated(parentBlkTime)
		feesCfg   = config.GetDynamicFeesConfig(isEActive)

		onAcceptFunc   func()
		inputs         set.Set[ids.ID]
		funcs          = make([]func(), 0, len(txs))
		atomicRequests = make(map[ids.ID]*atomic.Requests)
		totalBlockFees = uint64(0)
	)

	// feeMan := commonfees.NewManager(feeRates)
	// if isEActive { // FOR TESTING, METER AND UPDATE FEES EVEN PRE-E-FORK
	feeMan, err := fee.UpdatedFeeManager(state, v.txExecutorBackend.Config, parentBlkTime, blkTimestamp)
	if err != nil {
		return nil, nil, nil, nil, 0, err
	}
	// }

	for _, tx := range txs {
		txExecutor := executor.StandardTxExecutor{
			Backend:            v.txExecutorBackend,
			BlkFeeManager:      feeMan,
			BlockMaxComplexity: feesCfg.BlockMaxComplexity,
			State:              state,
			Tx:                 tx,
		}
		if err := tx.Unsigned.Visit(&txExecutor); err != nil {
			txID := tx.ID()
			v.MarkDropped(txID, err) // cache tx as dropped
			return nil, nil, nil, nil, 0, err
		}
		totalBlockFees += txExecutor.BaseFee

		// ensure it doesn't overlap with current input batch
		if inputs.Overlaps(txExecutor.Inputs) {
			return nil, nil, nil, nil, totalBlockFees, ErrConflictingBlockTxs
		}
		// Add UTXOs to batch
		inputs.Union(txExecutor.Inputs)

		state.AddTx(tx, status.Committed)
		if txExecutor.OnAccept != nil {
			funcs = append(funcs, txExecutor.OnAccept)
		}

		for chainID, txRequests := range txExecutor.AtomicRequests {
			// Add/merge in the atomic requests represented by [tx]
			chainRequests, exists := atomicRequests[chainID]
			if !exists {
				atomicRequests[chainID] = txRequests
				continue
			}

			chainRequests.PutRequests = append(chainRequests.PutRequests, txRequests.PutRequests...)
			chainRequests.RemoveRequests = append(chainRequests.RemoveRequests, txRequests.RemoveRequests...)
		}
	}

	if err := v.verifyUniqueInputs(parentID, inputs); err != nil {
		return nil, nil, nil, nil, totalBlockFees, err
	}

	// if isEActive { // FOR TESTING, METER AND UPDATE FEES EVEN PRE-E-FORK
	state.SetFeeRates(feeMan.GetFeeRates())
	state.SetLastBlockComplexity(feeMan.GetCumulatedComplexity())
	// }

	if numFuncs := len(funcs); numFuncs == 1 {
		onAcceptFunc = funcs[0]
	} else if numFuncs > 1 {
		onAcceptFunc = func() {
			for _, f := range funcs {
				f()
			}
		}
	}

	return inputs, feeMan, atomicRequests, onAcceptFunc, totalBlockFees, nil
}

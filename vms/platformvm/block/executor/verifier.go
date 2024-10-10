// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package executor

import (
	"errors"
	"fmt"

	"github.com/ava-labs/avalanchego/chains/atomic"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/math"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/components/gas"
	"github.com/ava-labs/avalanchego/vms/platformvm/block"
	"github.com/ava-labs/avalanchego/vms/platformvm/config"
	"github.com/ava-labs/avalanchego/vms/platformvm/metrics"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
	"github.com/ava-labs/avalanchego/vms/platformvm/status"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs/executor"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs/fee"

	validatorfee "github.com/ava-labs/avalanchego/vms/platformvm/validators/fee"
)

var (
	_ block.Visitor = (*verifier)(nil)

	ErrConflictingBlockTxs = errors.New("block contains conflicting transactions")

	errApricotBlockIssuedAfterFork           = errors.New("apricot block issued after fork")
	errBanffStandardBlockWithoutChanges      = errors.New("BanffStandardBlock performs no state changes")
	errIncorrectBlockHeight                  = errors.New("incorrect block height")
	errOptionBlockTimestampNotMatchingParent = errors.New("option block proposed timestamp not matching parent block one")
)

// verifier handles the logic for verifying a block.
type verifier struct {
	*backend
	txExecutorBackend *executor.Backend
	pChainHeight      uint64
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

	// Advance the time to [nextChainTime].
	nextChainTime := b.Timestamp()
	if _, err := executor.AdvanceTimeTo(v.txExecutorBackend, onDecisionState, nextChainTime); err != nil {
		return err
	}

	feeCalculator := state.PickFeeCalculator(v.txExecutorBackend.Config, onDecisionState)
	inputs, atomicRequests, onAcceptFunc, gasConsumed, err := v.processStandardTxs(
		b.Transactions,
		feeCalculator,
		onDecisionState,
		b.Parent(),
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

	return v.proposalBlock(
		b,
		b.Tx,
		onDecisionState,
		gasConsumed,
		onCommitState,
		onAbortState,
		feeCalculator,
		inputs,
		atomicRequests,
		onAcceptFunc,
	)
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

	feeCalculator := state.PickFeeCalculator(v.txExecutorBackend.Config, onAcceptState)
	return v.standardBlock(
		b,
		b.Transactions,
		feeCalculator,
		onAcceptState,
	)
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

	var (
		timestamp     = onCommitState.GetTimestamp() // Equal to parent timestamp
		feeCalculator = state.NewStaticFeeCalculator(v.txExecutorBackend.Config, timestamp)
	)
	return v.proposalBlock(
		b,
		b.Tx,
		nil,
		0,
		onCommitState,
		onAbortState,
		feeCalculator,
		nil,
		nil,
		nil,
	)
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

	var (
		timestamp     = onAcceptState.GetTimestamp() // Equal to parent timestamp
		feeCalculator = state.NewStaticFeeCalculator(v.txExecutorBackend.Config, timestamp)
	)
	return v.standardBlock(
		b,
		b.Transactions,
		feeCalculator,
		onAcceptState,
	)
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
	if cfg.UpgradeConfig.IsApricotPhase5Activated(currentTimestamp) {
		return fmt.Errorf(
			"the chain timestamp (%d) is after the apricot phase 5 time (%d), hence atomic transactions should go through the standard block",
			currentTimestamp.Unix(),
			cfg.UpgradeConfig.ApricotPhase5Time.Unix(),
		)
	}

	feeCalculator := state.NewStaticFeeCalculator(v.txExecutorBackend.Config, currentTimestamp)
	atomicExecutor := executor.AtomicTxExecutor{
		Backend:       v.txExecutorBackend,
		FeeCalculator: feeCalculator,
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
		atomicRequests:  atomicExecutor.AtomicRequests,
		verifiedHeights: set.Of(v.pChainHeight),
		metrics: calculateBlockMetrics(
			v.txExecutorBackend.Config,
			b,
			atomicExecutor.OnAccept,
			0,
		),
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
	now := v.txExecutorBackend.Clk.Time()
	return executor.VerifyNewChainTime(
		v.txExecutorBackend.Config.ValidatorFeeConfig,
		newChainTime,
		now,
		parentState,
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
	if v.txExecutorBackend.Config.UpgradeConfig.IsBanffActivated(timestamp) {
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
		statelessBlock:  b,
		onAcceptState:   onAbortState,
		timestamp:       onAbortState.GetTimestamp(),
		verifiedHeights: set.Of(v.pChainHeight),
		metrics: calculateBlockMetrics(
			v.txExecutorBackend.Config,
			b,
			onAbortState,
			0,
		),
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
		statelessBlock:  b,
		onAcceptState:   onCommitState,
		timestamp:       onCommitState.GetTimestamp(),
		verifiedHeights: set.Of(v.pChainHeight),
		metrics: calculateBlockMetrics(
			v.txExecutorBackend.Config,
			b,
			onCommitState,
			0,
		),
	}
	return nil
}

// proposalBlock populates the state of this block if [nil] is returned
func (v *verifier) proposalBlock(
	b block.Block,
	tx *txs.Tx,
	onDecisionState state.Diff,
	gasConsumed gas.Gas,
	onCommitState state.Diff,
	onAbortState state.Diff,
	feeCalculator fee.Calculator,
	inputs set.Set[ids.ID],
	atomicRequests map[ids.ID]*atomic.Requests,
	onAcceptFunc func(),
) error {
	txExecutor := executor.ProposalTxExecutor{
		OnCommitState: onCommitState,
		OnAbortState:  onAbortState,
		Backend:       v.txExecutorBackend,
		FeeCalculator: feeCalculator,
		Tx:            tx,
	}

	if err := tx.Unsigned.Visit(&txExecutor); err != nil {
		txID := tx.ID()
		v.MarkDropped(txID, err) // cache tx as dropped
		return err
	}

	onCommitState.AddTx(tx, status.Committed)
	onAbortState.AddTx(tx, status.Aborted)

	v.Mempool.Remove(tx)

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
		atomicRequests:  atomicRequests,
		verifiedHeights: set.Of(v.pChainHeight),
		metrics: calculateBlockMetrics(
			v.txExecutorBackend.Config,
			b,
			onCommitState,
			gasConsumed,
		),
	}
	return nil
}

// standardBlock populates the state of this block if [nil] is returned
func (v *verifier) standardBlock(
	b block.Block,
	txs []*txs.Tx,
	feeCalculator fee.Calculator,
	onAcceptState state.Diff,
) error {
	inputs, atomicRequests, onAcceptFunc, gasConsumed, err := v.processStandardTxs(
		txs,
		feeCalculator,
		onAcceptState,
		b.Parent(),
	)
	if err != nil {
		return err
	}

	v.Mempool.Remove(txs...)

	blkID := b.ID()
	v.blkIDToState[blkID] = &blockState{
		statelessBlock: b,

		onAcceptState: onAcceptState,
		onAcceptFunc:  onAcceptFunc,

		timestamp:       onAcceptState.GetTimestamp(),
		inputs:          inputs,
		atomicRequests:  atomicRequests,
		verifiedHeights: set.Of(v.pChainHeight),
		metrics: calculateBlockMetrics(
			v.txExecutorBackend.Config,
			b,
			onAcceptState,
			gasConsumed,
		),
	}
	return nil
}

func (v *verifier) processStandardTxs(txs []*txs.Tx, feeCalculator fee.Calculator, diff state.Diff, parentID ids.ID) (
	set.Set[ids.ID],
	map[ids.ID]*atomic.Requests,
	func(),
	gas.Gas,
	error,
) {
	// Complexity is limited first to avoid processing too large of a block.
	var (
		timestamp   = diff.GetTimestamp()
		isEtna      = v.txExecutorBackend.Config.UpgradeConfig.IsEtnaActivated(timestamp)
		gasConsumed gas.Gas
	)
	if isEtna {
		var blockComplexity gas.Dimensions
		for _, tx := range txs {
			txComplexity, err := fee.TxComplexity(tx.Unsigned)
			if err != nil {
				txID := tx.ID()
				v.MarkDropped(txID, err)
				return nil, nil, nil, 0, err
			}

			blockComplexity, err = blockComplexity.Add(&txComplexity)
			if err != nil {
				return nil, nil, nil, 0, err
			}
		}

		var err error
		gasConsumed, err = blockComplexity.ToGas(v.txExecutorBackend.Config.DynamicFeeConfig.Weights)
		if err != nil {
			return nil, nil, nil, 0, err
		}

		// If this block exceeds the available capacity, ConsumeGas will return
		// an error.
		feeState := diff.GetFeeState()
		feeState, err = feeState.ConsumeGas(gasConsumed)
		if err != nil {
			return nil, nil, nil, 0, err
		}

		// Updating the fee state prior to executing the transactions is fine
		// because the fee calculator was already created.
		diff.SetFeeState(feeState)
	}

	var (
		onAcceptFunc   func()
		inputs         set.Set[ids.ID]
		funcs          = make([]func(), 0, len(txs))
		atomicRequests = make(map[ids.ID]*atomic.Requests)
	)
	for _, tx := range txs {
		txExecutor := executor.StandardTxExecutor{
			Backend:       v.txExecutorBackend,
			State:         diff,
			FeeCalculator: feeCalculator,
			Tx:            tx,
		}
		if err := tx.Unsigned.Visit(&txExecutor); err != nil {
			txID := tx.ID()
			v.MarkDropped(txID, err) // cache tx as dropped
			return nil, nil, nil, 0, err
		}
		// ensure it doesn't overlap with current input batch
		if inputs.Overlaps(txExecutor.Inputs) {
			return nil, nil, nil, 0, ErrConflictingBlockTxs
		}
		// Add UTXOs to batch
		inputs.Union(txExecutor.Inputs)

		diff.AddTx(tx, status.Committed)
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

	// After processing all the transactions, deactivate any SoVs that might not
	// have sufficient fee to pay for the next second.
	//
	// This ensures that SoVs are not undercharged for the next second.
	if isEtna {
		var (
			validatorFeeState = validatorfee.State{
				Current: gas.Gas(diff.NumActiveSubnetOnlyValidators()),
				Excess:  diff.GetSoVExcess(),
			}
			accruedFees   = diff.GetAccruedFees()
			potentialCost = validatorFeeState.CostOf(
				v.txExecutorBackend.Config.ValidatorFeeConfig,
				1, // 1 second
			)
		)
		potentialAccruedFees, err := math.Add(accruedFees, potentialCost)
		if err != nil {
			return nil, nil, nil, 0, err
		}

		// Invariant: Proposal transactions do not impact SoV state.
		sovIterator, err := diff.GetActiveSubnetOnlyValidatorsIterator()
		if err != nil {
			return nil, nil, nil, 0, err
		}

		var sovsToDeactivate []state.SubnetOnlyValidator
		for sovIterator.Next() {
			sov := sovIterator.Value()
			// If the validator has exactly the right amount of fee for the next
			// second we should not remove them here.
			if sov.EndAccumulatedFee >= potentialAccruedFees {
				break
			}

			sovsToDeactivate = append(sovsToDeactivate, sov)
		}

		// The iterator must be released prior to attempting to write to the
		// diff.
		sovIterator.Release()

		for _, sov := range sovsToDeactivate {
			sov.EndAccumulatedFee = 0
			if err := diff.PutSubnetOnlyValidator(sov); err != nil {
				return nil, nil, nil, 0, err
			}
		}
	}

	if err := v.verifyUniqueInputs(parentID, inputs); err != nil {
		return nil, nil, nil, 0, err
	}

	if numFuncs := len(funcs); numFuncs == 1 {
		onAcceptFunc = funcs[0]
	} else if numFuncs > 1 {
		onAcceptFunc = func() {
			for _, f := range funcs {
				f()
			}
		}
	}

	return inputs, atomicRequests, onAcceptFunc, gasConsumed, nil
}

func calculateBlockMetrics(
	config *config.Config,
	blk block.Block,
	s state.Chain,
	gasConsumed gas.Gas,
) metrics.Block {
	var (
		gasState        = s.GetFeeState()
		validatorExcess = s.GetSoVExcess()
	)
	return metrics.Block{
		Block: blk,

		GasConsumed: gasConsumed,
		GasState:    gasState,
		GasPrice: gas.CalculatePrice(
			config.DynamicFeeConfig.MinPrice,
			gasState.Excess,
			config.DynamicFeeConfig.ExcessConversionConstant,
		),

		ActiveSoVs:      s.NumActiveSubnetOnlyValidators(),
		ValidatorExcess: validatorExcess,
		ValidatorPrice: gas.CalculatePrice(
			config.ValidatorFeeConfig.MinPrice,
			validatorExcess,
			config.ValidatorFeeConfig.ExcessConversionConstant,
		),
	}
}

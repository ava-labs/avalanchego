// Copyright (C) 2025-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package blocks

import (
	"fmt"
	"maps"
	"slices"

	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/state"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/trie"
	"github.com/holiman/uint256"
	"go.uber.org/zap"

	"github.com/ava-labs/strevm/gastime"
	"github.com/ava-labs/strevm/hook"
)

// WorstCaseBounds define the limits of certain values, predicted by the block
// builder, that a [Block] will encounter when eventually executed.
type WorstCaseBounds struct {
	MaxBaseFee *uint256.Int
	// LatestEndTime is the worst-case [gastime.Time] after this block's gas has been
	// consumed and the target updated. Its [gastime.Time.BaseFee] is an upper
	// bound on the next block's base fee because the next block's
	// [gastime.Time.BeforeBlock] can only reduce the excess.
	LatestEndTime *gastime.Time
	// Invariant: keys of individual maps MUST be identical to those of the
	// respective [hook.Op.Burn] map. For transaction-derived Ops, there is
	// always 1 entry.
	MinOpBurnerBalances []map[common.Address]*uint256.Int
}

// SetWorstCaseBounds sets the bounds, which MUST be done before execution.
func (b *Block) SetWorstCaseBounds(lim *WorstCaseBounds) {
	b.bounds = lim
}

// WorstCaseBounds returns the argument passed to [Block.SetWorstCaseBounds].
func (b *Block) WorstCaseBounds() *WorstCaseBounds {
	return b.bounds
}

// CheckBaseFeeBound logs at ERROR if the `actual` base fee is greater than the
// predicted upper bound passed to [Block.SetWorstCaseBounds].
//
// Such a violation, while potentially critical, might not result in failed
// execution so no error is returned and execution MUST continue optimistically.
// Any such log in development will cause tests to fail.
func (b *Block) CheckBaseFeeBound(actual *uint256.Int) {
	if b.bounds == nil {
		return
	}

	switch actual.Cmp(b.bounds.MaxBaseFee) {
	case 1:
		b.log.Error("Actual base fee > predicted worst case",
			zap.Stringer("actual", actual),
			zap.Stringer("predicted", b.bounds.MaxBaseFee),
		)

	case 0: // Coverage visualisation
		_ = 0
	case -1:
		_ = 0
	}
}

// CheckSenderBalanceBound logs at ERROR if the balance of the `tx` sender is
// less than the predicted lower bound passed to [Block.SetWorstCaseBounds].
// [state.StateDB.SetTxContext] MUST have already been called.
//
// Such a violation, while potentially critical, might not result in failed
// execution so no error is returned and execution MUST continue optimistically.
// Any such log in development will cause tests to fail.
func (b *Block) CheckSenderBalanceBound(stateDB *state.StateDB, signer types.Signer, tx *types.Transaction) {
	if b.bounds == nil {
		return
	}

	log := b.log.With(
		zap.Int("tx_index", stateDB.TxIndex()),
		zap.Stringer("tx_hash", tx.Hash()),
	)
	sender, err := types.Sender(signer, tx)
	if err != nil {
		log.Warn("Unable to recover sender for confirming worst-case balance",
			zap.Error(err),
		)
		return
	}
	b.checkBalanceBounds(log, stateDB, stateDB.TxIndex(), sender)
}

// CheckOpBurnerBalanceBounds is equivalent to [Block.CheckSenderBalanceBound],
// performed for every address in [hook.Op.Burn] instead of only for a single
// transaction sender.
//
// For the purposes of calculating the Op's index in the block, a
// [types.Transaction] is also considered to be an Op.
func (b *Block) CheckOpBurnerBalanceBounds(stateDB *state.StateDB, opIndexInBlock int, op hook.Op) {
	if b.bounds == nil {
		return
	}

	log := b.log.With(
		zap.Int("op_index_in_block", opIndexInBlock),
		zap.Stringer("op_id", op.ID),
	)
	b.checkBalanceBounds(log, stateDB, opIndexInBlock, slices.Collect(maps.Keys(op.Burn))...)
}

func (b *Block) checkBalanceBounds(log logging.Logger, stateDB *state.StateDB, opIndexInBlock int, accounts ...common.Address) {
	minBals := b.bounds.MinOpBurnerBalances[opIndexInBlock]
	if len(minBals) != len(accounts) {
		log.Warn("Incorrect number of worst-case op-burner balances",
			zap.Int("min_balance_bounds", len(minBals)),
			zap.Int("accounts_to_check", len(accounts)),
		)
	}

	for _, addr := range accounts {
		low, ok := minBals[addr]
		if !ok {
			log.Warn("Op burner (transaction sender) not in worst-case op-burner balances",
				zap.Stringer("burner", addr),
				zap.Stringers("op_burners", slices.Collect(maps.Keys(minBals))),
			)
			continue
		}
		switch actual := stateDB.GetBalance(addr); actual.Cmp(low) {
		case -1:
			log.Error("Actual balance < predicted worst case",
				zap.Stringer("burner_or_sender", addr),
				zap.Stringer("actual", actual),
				zap.Stringer("predicted", low),
			)

		case 0: // Coverage visualisation
			_ = 0
		case 1:
			_ = 0
		}
	}
}

// A LifeCycleStage defines the progression of a block from acceptance through
// to settlement.
type LifeCycleStage int

// Valid [LifeCycleStage] values. Blocks proceed in increasing stage numbers,
// but specific values MUST NOT be relied upon to be stable.
const (
	NotExecuted LifeCycleStage = iota
	Executed
	Settled

	Accepted = NotExecuted
)

func (b *Block) brokenInvariantErr(msg string) error {
	return fmt.Errorf("block %d: %s", b.Height(), msg)
}

// CheckInvariants checks internal invariants against expected stage, typically
// only used during database recovery.
func (b *Block) CheckInvariants(expect LifeCycleStage) error {
	switch e := b.execution.Load(); e {
	case nil: // not executed
		if expect >= Executed {
			return b.brokenInvariantErr("expected to be executed")
		}
	default: // executed
		if expect < Executed {
			return b.brokenInvariantErr("unexpectedly executed")
		}
		if e.receiptRoot != types.DeriveSha(e.receipts, trie.NewStackTrie(nil)) {
			return b.brokenInvariantErr("receipts don't match root")
		}
	}

	switch a := b.ancestry.Load(); a {
	case nil: // settled
		if expect < Settled {
			return b.brokenInvariantErr("unexpectedly settled")
		}
	default: // not settled
		if expect >= Settled {
			return b.brokenInvariantErr("expected to be settled")
		}
		if b.SettledStateRoot() != b.LastSettled().PostExecutionStateRoot() {
			return b.brokenInvariantErr("state root does not match last-settled post execution")
		}
	}

	return nil
}

// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package worstcase provides the worst-case balance and nonce tracking needed
// to safely include transactions that are guaranteed to be valid during
// execution.
package worstcase

import (
	"errors"
	"fmt"
	"math"
	"math/big"
	"slices"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core"
	"github.com/ava-labs/libevm/core/state"
	"github.com/ava-labs/libevm/core/txpool"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/params"
	"github.com/holiman/uint256"

	"github.com/ava-labs/avalanchego/vms/components/gas"
	"github.com/ava-labs/avalanchego/vms/saevm/blocks"
	"github.com/ava-labs/avalanchego/vms/saevm/gastime"
	"github.com/ava-labs/avalanchego/vms/saevm/hook"
	"github.com/ava-labs/avalanchego/vms/saevm/saedb"

	saeparams "github.com/ava-labs/avalanchego/vms/saevm/params"
)

// State tracks the worst-case gas price and account state as operations are
// executed.
//
// Usage of [State] must follow the pattern:
//  1. [State.StartBlock] for each block to be included.
//  2. [State.GasLimit] and [State.BaseFee] to query the block's parameters.
//  3. [State.ApplyTx] or [State.Apply] for each [types.Transaction] or
//     [hook.Op] to include in the block, respectively.
//  4. [State.GasUsed] to query the total gas used in the block.
//  5. [State.FinishBlock] to finalize the block's gas time.
//  6. Repeat from step 1 for the next block.
type State struct {
	hooks  hook.Points
	config *params.ChainConfig

	db    *state.StateDB
	clock *gastime.Time
	// expectedParentHash is used to sanity check that blocks are provided in
	// order. The [types.Header] in the `curr` field is modified to reflect
	// worst-case bounds (which will almost certainly differ from actual values
	// when replaying historical blocks) so its hash can't be used.
	expectedParentHash common.Hash

	qSize, blockSize, maxBlockSize gas.Gas

	baseFee             *uint256.Int
	curr                *types.Header
	signer              types.Signer
	minOpBurnerBalances []map[common.Address]*uint256.Int
}

var errSettledBlockNotExecuted = errors.New("block marked for settling has not finished execution yet")

// NewState constructs a new worst-case state on top of the settled block.
func NewState(
	hooks hook.Points,
	config *params.ChainConfig,
	settled *blocks.Block,
	opener saedb.StateDBOpener,
) (*State, error) {
	if !settled.Executed() {
		return nil, errSettledBlockNotExecuted
	}

	db, err := opener.StateDB(settled.PostExecutionStateRoot())
	if err != nil {
		return nil, err
	}
	return &State{
		hooks:              hooks,
		config:             config,
		db:                 db,
		clock:              settled.ExecutedByGasTime(),
		expectedParentHash: settled.Hash(),
	}, nil
}

const (
	maxGasSecondsPerBlock = saeparams.TauSeconds * saeparams.Lambda
)

var (
	errNonConsecutiveBlocks = errors.New("non-consecutive blocks")
	// ErrQueueFull is returned by [State.StartBlock] if the queue is not able
	// to accept new blocks. This can be rectified by building a block at a
	// later time such that additional blocks are settled and the queue is
	// sufficiently drained.
	ErrQueueFull = errors.New("queue exceeds gas threshold for new block")
)

// StartBlock updates the worst-case state to the beginning of the provided
// block.
//
// It is not necessary for [types.Header.GasLimit] nor [types.Header.BaseFee] to
// be set. However, all other fields should be populated and
// [types.Header.ParentHash] must match the previous block's hash.
//
// If the queue is too full to accept another block, [ErrQueueFull] is returned.
func (s *State) StartBlock(h *types.Header) error {
	if h.ParentHash != s.expectedParentHash {
		return fmt.Errorf("%w: expected parent hash of %s but was %s",
			errNonConsecutiveBlocks,
			s.expectedParentHash,
			h.ParentHash,
		)
	}

	s.clock.BeforeBlock(s.hooks.BlockTime(h))
	s.blockSize = 0

	s.maxBlockSize = safeMaxBlockSize(s.clock)
	if maxOpenQSize := saeparams.MaxFullBlocksInOpenQueue * s.maxBlockSize; s.qSize > maxOpenQSize {
		return fmt.Errorf("%w: current size %d exceeds maximum size for accepting new blocks %d", ErrQueueFull, s.qSize, maxOpenQSize)
	}

	s.baseFee = s.clock.BaseFee()
	clear(s.minOpBurnerBalances) // [State.FinishBlock] returns a clone so we can reuse the alloc here
	s.minOpBurnerBalances = s.minOpBurnerBalances[:0]

	// expectedParentHash is updated prior to modifying the GasLimit and BaseFee
	// to ensure that historical block hashes are not modified.
	s.expectedParentHash = h.Hash()
	s.curr = types.CopyHeader(h)
	s.curr.GasLimit = uint64(s.maxBlockSize)
	s.curr.BaseFee = s.baseFee.ToBig()

	// We MUST use the block's timestamp, not the execution clock's, otherwise
	// we might enable an upgrade too early.
	s.signer = types.MakeSigner(s.config, h.Number, h.Time)
	return nil
}

// safeMaxBlockSize returns the maximum block size for the clock's rate,
// possibly capping it so a full closed queue still fits in [gas.Gas]. At the
// time of writing, the cap is ~6e17, so capping is exceedingly unlikely.
func safeMaxBlockSize(clock *gastime.Time) gas.Gas {
	const (
		maxGasSecondsInClosedQueue         = saeparams.MaxFullBlocksInClosedQueue * maxGasSecondsPerBlock
		maxGasInClosedQueue        gas.Gas = math.MaxUint64
		maxSafeRate                gas.Gas = maxGasInClosedQueue / maxGasSecondsInClosedQueue
	)
	return min(clock.Rate(), maxSafeRate) * maxGasSecondsPerBlock
}

// GasLimit returns the available gas limit for the current block.
func (s *State) GasLimit() uint64 {
	return uint64(s.maxBlockSize)
}

// BaseFee returns the worst-case base fee for the current block.
func (s *State) BaseFee() *uint256.Int {
	return s.baseFee
}

var errCostOverflow = errors.New("Cost() overflows uint256")

// ApplyTx validates the transaction both intrinsically and in the context of
// worst-case gas assumptions of all previous operations. This provides an upper
// bound on the total cost of the transaction such that a nil error returned by
// ApplyTx guarantees that the sender of the transaction will have sufficient
// balance to cover its costs if consensus accepts the same operation set
// (and order) as was applied.
//
// If the transaction can not be applied, an error is returned and the state is
// not modified.
//
// TODO: Consider exporting txToOp and expecting users to call Apply directly.
func (s *State) ApplyTx(tx *types.Transaction) error {
	opts := &txpool.ValidationOptions{
		Config: s.config,
		Accept: 0 |
			1<<types.LegacyTxType |
			1<<types.AccessListTxType |
			1<<types.DynamicFeeTxType,
		// No byte-size limit needed as gas validation (intrinsic gas ≤
		// tx gas ≤ block gas limit) already enforces an implicit size
		// limit (2MB) on transactions.
		MaxSize: math.MaxUint,
		MinTip:  big.NewInt(0),
	}
	if err := txpool.ValidateTransaction(tx, s.curr, s.signer, opts); err != nil {
		return fmt.Errorf("validating transaction: %w", err)
	}

	from, err := types.Sender(s.signer, tx)
	if err != nil {
		return fmt.Errorf("determining sender: %w", err)
	}

	// While EOA enforcement is not possible to guarantee in worst-case
	// execution, we can prevent most cases here.
	//
	// TODO: We must still handle non-EOA issuance later during actual
	// execution.
	if codeHash := s.db.GetCodeHash(from); codeHash != (common.Hash{}) && codeHash != types.EmptyCodeHash {
		return fmt.Errorf("%w: address %v, codehash: %s", core.ErrSenderNoEOA, from.Hex(), codeHash)
	}

	if err := s.hooks.CanExecuteTransaction(from, tx.To(), s.db); err != nil {
		return fmt.Errorf("transaction blocked by CanExecuteTransaction hook: %w", err)
	}

	op, err := txToOp(from, tx, s.baseFee)
	if err != nil {
		return fmt.Errorf("converting transaction to operation: %w", err)
	}
	return s.Apply(op)
}

func bigToUint256(v *big.Int) (_ uint256.Int, overflow bool) {
	var x uint256.Int
	overflow = x.SetFromBig(v)
	return x, overflow
}

// mulAdd returns a*b + c and reports whether overflow occurred.
func mulAdd(a uint64, b, c *uint256.Int) (_ uint256.Int, overflow bool) {
	var x uint256.Int
	x.SetUint64(a)
	if _, overflow := x.MulOverflow(&x, b); overflow {
		return uint256.Int{}, true
	}
	_, overflow = x.AddOverflow(&x, c)
	return x, overflow
}

func txToOp(from common.Address, tx *types.Transaction, baseFee *uint256.Int) (hook.Op, error) {
	type Op = hook.Op // for convenience when returning zero value

	gasFeeCap, overflow := bigToUint256(tx.GasFeeCap())
	if overflow {
		return Op{}, core.ErrFeeCapVeryHigh
	}
	value, overflow := bigToUint256(tx.Value())
	if overflow {
		return Op{}, core.ErrInsufficientFundsForTransfer
	}
	minBalance, overflow := mulAdd(tx.Gas(), &gasFeeCap, &value)
	if overflow {
		return Op{}, errCostOverflow
	}

	// effectiveGasPrice = min(gasFeeCap, baseFee + gasTipCap)
	gasTipCap, overflow := bigToUint256(tx.GasTipCap())
	if overflow {
		return Op{}, core.ErrTipVeryHigh
	}
	var effectiveGasPrice uint256.Int
	if _, overflow := effectiveGasPrice.AddOverflow(baseFee, &gasTipCap); overflow || gasFeeCap.Lt(&effectiveGasPrice) {
		effectiveGasPrice.Set(&gasFeeCap)
	}

	amount, overflow := mulAdd(tx.Gas(), &effectiveGasPrice, &value)
	if overflow {
		return Op{}, errCostOverflow
	}
	return Op{
		Gas:       gas.Gas(tx.Gas()),
		GasFeeCap: gasFeeCap,
		Burn: map[common.Address]hook.AccountDebit{
			from: {
				Nonce:      tx.Nonce(),
				Amount:     amount,
				MinBalance: minBalance,
			},
		},
		// Mint MUST NOT be populated here because this transaction may revert.
	}, nil
}

// Apply attempts to apply the operation to this state.
//
// If the operation can not be applied, an error is returned and the state is
// not modified.
//
// Operations are invalid if any of the following are true:
//
//   - The operation consumes more gas than the block has available.
//   - The operation specifies too low of a gas price.
//   - The operation is from an account with an incorrect or invalid nonce.
//   - The operation is from an account with an insufficient balance.
func (s *State) Apply(o hook.Op) error {
	if o.Gas > s.maxBlockSize-s.blockSize {
		return core.ErrGasLimitReached
	}
	if o.GasFeeCap.Lt(s.baseFee) {
		return core.ErrFeeCapTooLow
	}

	burnerBalances := make(map[common.Address]*uint256.Int, len(o.Burn))
	for from, ad := range o.Burn {
		switch nonce, next := ad.Nonce, s.db.GetNonce(from); {
		case nonce < next:
			return fmt.Errorf("%w: %d < %d", core.ErrNonceTooLow, nonce, next)
		case nonce > next:
			return fmt.Errorf("%w: %d > %d", core.ErrNonceTooHigh, nonce, next)
		case next == math.MaxUint64:
			return core.ErrNonceMax
		}
		// MUST be before `o.ApplyTo()` to mirror [saexec.Executor] check
		burnerBalances[from] = s.db.GetBalance(from)
	}

	if err := o.ApplyTo(s.db); err != nil {
		return err
	}
	s.minOpBurnerBalances = append(s.minOpBurnerBalances, burnerBalances)
	s.blockSize += o.Gas
	return nil
}

// GasUsed returns the gas used for the current block.
func (s *State) GasUsed() uint64 {
	return uint64(s.blockSize)
}

// FinishBlock advances the [gastime.Time] in preparation for the next block.
//
// The returned bounds assume that every non-nil error from [State.ApplyTx]
// resulted in said transaction being included, which is reflected in the
// indexing of tx-sender balances.
func (s *State) FinishBlock() (*blocks.WorstCaseBounds, error) {
	target, gasCfg := s.hooks.GasConfigAfter(s.curr)
	if err := s.clock.AfterBlock(s.blockSize, target, gasCfg); err != nil {
		return nil, fmt.Errorf("finishing block gas time update: %w", err)
	}
	s.qSize += s.blockSize
	return &blocks.WorstCaseBounds{
		MaxBaseFee:          s.baseFee,
		LatestEndTime:       s.clock.Clone(),
		MinOpBurnerBalances: slices.Clone(s.minOpBurnerBalances),
	}, nil
}

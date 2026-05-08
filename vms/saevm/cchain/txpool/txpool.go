// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package txpool implements an in-memory pool of cross-chain transactions
// awaiting inclusion in a block.
package txpool

import (
	"context"
	"errors"
	"fmt"
	"iter"
	"slices"
	"sync"

	"github.com/ava-labs/libevm/core"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/event"
	"github.com/ava-labs/libevm/libevm"
	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/graft/coreth/params"
	"github.com/ava-labs/avalanchego/graft/coreth/plugin/evm/customtypes"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/utils/heap"
	"github.com/ava-labs/avalanchego/utils/lock"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/utils/setmap"
	"github.com/ava-labs/avalanchego/vms/saevm/blocks"
	"github.com/ava-labs/avalanchego/vms/saevm/cchain/tx"
	"github.com/ava-labs/avalanchego/vms/saevm/hook"
)

// Backend that the [Txpool] depends on for current chain state.
type Backend interface {
	SubscribeChainHeadEvent(ch chan<- core.ChainHeadEvent) event.Subscription
	LastExecutedState() (libevm.StateReader, error)
}

// Txpool is an in-memory pool of cross-chain transactions awaiting inclusion
// in a block.
//
// Transactions are admitted only after passing verification against the most
// recently executed state.
//
// Transactions are removed after they are included in an executed block or are
// replaced by a higher paying transaction.
type Txpool struct {
	*Pending

	ctx     *snow.Context
	sub     event.Subscription
	maxSize int

	// stateLock is ordered before [Pending.lock]. Acquiring stateLock with
	// [Pending.lock] held will deadlock.
	stateLock sync.RWMutex
	state     libevm.StateReader
}

// New constructs a [Txpool] that enables adding transactions to [Pending].
//
// maxSize is the maximum number of transactions the pool will hold; once
// reached, [Txpool.Add] evicts the lowest-fee transaction in favor of a
// strictly higher-fee incoming transaction.
//
// [Txpool.Close] MUST be called during shutdown to release allocated resources.
func New(
	ctx *snow.Context,
	chainConfig *params.ChainConfig,
	pending *Pending,
	chain Backend,
	maxSize int,
) (*Txpool, error) {
	if maxSize <= 0 {
		return nil, fmt.Errorf("maxSize must be > 0: %d", maxSize)
	}

	// executed is unbuffered to guarantee that the pool never holds a reference
	// to state older than the last-settled state. SAE does not guarantee that
	// such a state exists on disk anymore.
	executed := make(chan core.ChainHeadEvent)
	sub := chain.SubscribeChainHeadEvent(executed)

	state, err := chain.LastExecutedState()
	if err != nil {
		sub.Unsubscribe()
		return nil, fmt.Errorf("getting last executed state: %w", err)
	}

	// state must be populated after [Backend.SubscribeChainHeadEvent] is called
	// to ensure we do not miss an update.
	p := &Txpool{
		Pending: pending,
		ctx:     ctx,
		sub:     sub,
		maxSize: maxSize,
		state:   state,
	}

	go func() {
		defer sub.Unsubscribe()
		for {
			select {
			case e := <-executed:
				b := e.Block
				inputs, err := inputUTXOs(b, chainConfig)
				if err != nil {
					ctx.Log.Error("unable to get inputs from block",
						zap.Stringer("blockHash", b.Hash()),
						zap.Uint64("blockNumber", b.NumberU64()),
						zap.Error(err),
					)
					continue
				}

				newState, err := chain.LastExecutedState()
				if err != nil {
					ctx.Log.Error("unable to get latest executed state",
						zap.Error(err),
					)
					continue
				}

				p.stateLock.Lock()
				p.lock.Lock()

				p.removeConflicts(inputs)
				p.state = newState

				p.lock.Unlock()
				p.stateLock.Unlock()
			case err := <-sub.Err():
				if err != nil {
					ctx.Log.Error("pool subscription failed",
						zap.Error(err),
					)
				}
				return
			}
		}
	}()
	return p, nil
}

var (
	// ErrAlreadyKnown is returned by [Txpool.Add] when the transaction is
	// already in the pool.
	ErrAlreadyKnown = errors.New("transaction already in pool")

	errSanityCheck       = errors.New("sanity check")
	errVerifyCredentials = errors.New("credential verification")
	errVerifyState       = errors.New("state verification")
	errInsufficientFee   = errors.New("insufficient fee")
)

// Add validates tx and inserts it into the pool.
//
// If tx conflicts with a transaction already in the pool, the lower-fee
// transaction is evicted. If the pool is at capacity, the lowest-fee
// transaction is evicted in favor of a higher-fee incoming transaction.
//
// Returns [ErrAlreadyKnown] if tx is already in the pool.
func (p *Txpool) Add(tx *tx.Tx) error {
	if err := tx.SanityCheck(p.ctx); err != nil {
		return fmt.Errorf("%w: %w", errSanityCheck, err)
	}

	t, err := newTxData(tx, p.ctx.AVAXAssetID)
	if err != nil {
		return err
	}

	// TODO:(StephenButtolph): Should we enforce a maximum gas amount here?

	// We must verify the tx against a state that is at least as high as the
	// last block processed by the pool subscription.
	//
	// Verifying against an older state risks admitting a tx that would never
	// be evicted.
	p.stateLock.RLock()
	defer p.stateLock.RUnlock()

	if err := tx.VerifyCredentials(p.ctx.SharedMemory); err != nil {
		return fmt.Errorf("%w: %w", errVerifyCredentials, err)
	}
	if err := verifyOp(p.state, t.op); err != nil {
		return fmt.Errorf("%w: %w", errVerifyState, err)
	}

	p.lock.Lock()
	defer p.lock.Unlock()

	if _, ok := p.txs.Get(t.id); ok {
		return ErrAlreadyKnown
	}

	for input := range t.inputs {
		if conflictID, ok := p.utxos.GetKey(input); ok {
			conflict, _ := p.txs.Get(conflictID)
			if t.op.GasFeeCap.Cmp(&conflict.op.GasFeeCap) <= 0 {
				return errInsufficientFee
			}
		}
	}
	p.removeConflicts(t.inputs)

	if p.txs.Len() >= p.maxSize {
		_, cheap, _ := p.txs.Peek()
		if t.op.GasFeeCap.Cmp(&cheap.op.GasFeeCap) <= 0 {
			return errInsufficientFee
		}
		p.removeConflicts(cheap.inputs)
	}

	p.utxos.Put(t.id, t.inputs)
	p.txs.Push(t.id, t)
	p.cond.Broadcast()
	return nil
}

func (p *Txpool) removeConflicts(utxos set.Set[ids.ID]) {
	for _, removed := range p.utxos.DeleteOverlapping(utxos) {
		p.txs.Remove(removed.Key)
	}
}

// Close releases all allocated resources.
func (p *Txpool) Close() {
	p.sub.Unsubscribe()
}

// inputUTXOs returns the union of all UTXO IDs consumed by transactions in b,
// covering both EVM-native account+nonce inputs and cross-chain inputs.
func inputUTXOs(b *types.Block, c *params.ChainConfig) (set.Set[ids.ID], error) {
	var (
		signer = blocks.Signer(b, c)
		inputs set.Set[ids.ID]
	)
	for i, t := range b.Transactions() {
		sender, err := signer.Sender(t)
		if err != nil {
			return nil, fmt.Errorf("getting sender of tx %s (%d): %w", t.Hash(), i, err)
		}
		inputs.Add(tx.AccountInputID(sender, t.Nonce()))
	}

	txs, err := tx.ParseSlice(customtypes.BlockExtData(b))
	if err != nil {
		return nil, fmt.Errorf("parsing txs: %w", err)
	}
	for _, tx := range txs {
		inputs.Union(tx.InputIDs())
	}
	return inputs, nil
}

var (
	errNonceMismatch     = errors.New("nonce mismatch")
	errInsufficientFunds = errors.New("insufficient funds")
)

// verifyOp verifies that op's debits are valid against state.
func verifyOp(state libevm.StateReader, op hook.Op) error {
	for address, debit := range op.Burn {
		if nonce := state.GetNonce(address); nonce != debit.Nonce {
			return fmt.Errorf("%w: address %s has nonce %d but needs %d", errNonceMismatch, address, nonce, debit.Nonce)
		}
		if balance := state.GetBalance(address); balance.Lt(&debit.MinBalance) {
			return fmt.Errorf("%w: address %s has balance %s but needs %s", errInsufficientFunds, address, balance.String(), debit.MinBalance.String())
		}
	}
	return nil
}

// Pending stores transactions that are eligible for inclusion in a future
// block, indexed for fast conflict lookup and ordered by gas price.
type Pending struct {
	lock sync.RWMutex
	cond *lock.Cond

	// txs is the collection of transactions available to be included into a
	// block, ordered as a min-heap by gas price.
	txs heap.Map[ids.ID, *txData]
	// utxos maps a txID to the set of utxoIDs it consumes.
	utxos *setmap.SetMap[ids.ID, ids.ID]
}

// NewPending constructs an empty set of [Pending] transactions.
func NewPending() *Pending {
	p := &Pending{
		txs: heap.NewMap[ids.ID, *txData](func(a, b *txData) bool {
			return a.op.GasFeeCap.Lt(&b.op.GasFeeCap) // txs is a min-heap
		}),
		utxos: setmap.New[ids.ID, ids.ID](),
	}
	p.cond = lock.NewCond(p.lock.RLocker())
	return p
}

// Iter returns an iterator over the pool's transactions in decreasing gas
// price order.
func (p *Pending) Iter() iter.Seq[*tx.Tx] {
	p.lock.RLock()
	// TODO:(StephenButtolph): Iteration shouldn't copy the pool.
	values := heap.MapValues(p.txs)
	p.lock.RUnlock()

	slices.SortFunc(values, func(a, b *txData) int {
		return -a.op.GasFeeCap.Cmp(&b.op.GasFeeCap)
	})

	return func(yield func(*tx.Tx) bool) {
		for _, t := range values {
			if !yield(t.tx) {
				return
			}
		}
	}
}

// Len returns the number of transactions currently in the pool.
func (p *Pending) Len() int {
	p.lock.RLock()
	defer p.lock.RUnlock()

	return p.txs.Len()
}

// Has reports whether txID is in the pool.
func (p *Pending) Has(txID ids.ID) bool {
	p.lock.RLock()
	defer p.lock.RUnlock()

	_, ok := p.txs.Get(txID)
	return ok
}

// AwaitTxs blocks until at least one transaction is in the pool or ctx is
// cancelled.
func (p *Pending) AwaitTxs(ctx context.Context) error {
	p.lock.RLock()
	defer p.lock.RUnlock()

	for p.txs.Len() == 0 {
		if err := p.cond.Wait(ctx); err != nil {
			return err
		}
	}

	return nil
}

// txData contains the values from [tx.Tx] that the pool uses for ordering and
// conflict detection.
type txData struct {
	id     ids.ID
	tx     *tx.Tx
	inputs set.Set[ids.ID]
	op     hook.Op
}

var errAsOp = errors.New("as op")

func newTxData(tx *tx.Tx, avaxAssetID ids.ID) (*txData, error) {
	op, err := tx.AsOp(avaxAssetID)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", errAsOp, err)
	}
	return &txData{
		id:     op.ID,
		tx:     tx,
		inputs: tx.InputIDs(),
		op:     op,
	}, nil
}

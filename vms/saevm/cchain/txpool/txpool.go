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
	"sync"

	"github.com/ava-labs/libevm/core"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/event"
	"github.com/ava-labs/libevm/libevm"
	"github.com/google/btree"
	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/graft/coreth/params"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/utils/lock"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/utils/setmap"
	"github.com/ava-labs/avalanchego/vms/saevm/blocks"
	"github.com/ava-labs/avalanchego/vms/saevm/cchain/dynamic"
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

	snowCtx *snow.Context
	sub     event.Subscription
	maxSize int
	wg      sync.WaitGroup

	// executionLock is ordered before [Pending.lock] and [Txpool.stateLock].
	// Acquiring executionLock with either other lock held will deadlock.
	executionLock sync.RWMutex
	stateLock     sync.Mutex
	state         libevm.StateReader
}

// New constructs a [Txpool] that wraps the provided [Pending].
//
// maxSize is the maximum number of transactions the pool will hold; once
// reached, [Txpool.Add] evicts the lowest-fee transaction in favor of a
// strictly higher-fee incoming transaction.
//
// [Txpool.Close] MUST be called during shutdown to release allocated resources.
func New(
	snowCtx *snow.Context,
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
		snowCtx: snowCtx,
		sub:     sub,
		maxSize: maxSize,
		state:   state,
	}
	p.wg.Go(func() {
		p.updateState(chainConfig, chain, executed)
	})
	return p, nil
}

func (p *Txpool) updateState(
	chainConfig *params.ChainConfig,
	chain Backend,
	executed <-chan core.ChainHeadEvent,
) {
	sub := p.sub
	defer sub.Unsubscribe()
	for {
		select {
		case e := <-executed:
			var (
				b   = e.Block
				log = p.snowCtx.Log.With(
					zap.Stringer("blockHash", b.Hash()),
					zap.Uint64("blockNumber", b.NumberU64()),
				)
			)

			inputs, err := inputUTXOs(b, chainConfig)
			if err != nil {
				log.Error("unable to get inputs from block",
					zap.Error(err),
				)
				continue
			}

			newState, err := chain.LastExecutedState()
			if err != nil {
				log.Error("unable to get latest executed state",
					zap.Error(err),
				)
				continue
			}

			p.executionLock.Lock()
			p.lock.Lock()

			p.removeConflicts(inputs)
			p.state = newState

			p.lock.Unlock()
			p.executionLock.Unlock()

			log.Debug("updated to new state")
		case err := <-sub.Err():
			if err != nil {
				p.snowCtx.Log.Error("pool subscription failed",
					zap.Error(err),
				)
			}
			return
		}
	}
}

var (
	// ErrAlreadyKnown is returned by [Txpool.Add] when the transaction is
	// already in the pool.
	ErrAlreadyKnown = errors.New("transaction already in pool")

	errSanityCheck       = errors.New("sanity check")
	errExcessGas         = errors.New("gas exceeds minimum gas target")
	errVerifyCredentials = errors.New("credential verification")
	errVerifyState       = errors.New("state verification")
	errInsufficientFee   = errors.New("insufficient fee")
)

// Each tx byte must cost at least one gas.
const _ uint = tx.GasPerByte - 1

// Add validates tx and inserts it into the pool.
//
// If tx conflicts with a transaction already in the pool, the lower-fee
// transaction is evicted. If the pool is at capacity, the lowest-fee
// transaction is evicted in favor of a higher-fee incoming transaction.
//
// Returns [ErrAlreadyKnown] if tx is already in the pool.
func (p *Txpool) Add(tx *tx.Tx) error {
	if err := tx.SanityCheck(p.snowCtx); err != nil {
		return fmt.Errorf("%w: %w", errSanityCheck, err)
	}

	t, err := newTxData(tx, p.snowCtx.AVAXAssetID)
	if err != nil {
		return err
	}

	// Cap admitted-tx gas at MinTarget, the floor of the dynamic target, so
	// every admitted tx stays includable. An unincludable tx never pays its
	// fee, so an attacker could pin it with a free, arbitrarily high GasFeeCap
	// and fill the pool.
	//
	// Since each tx byte costs at least one gas, this also caps tx size at
	// MinTarget bytes, bounding the pool's memory.
	if t.op.Gas > dynamic.MinTarget {
		return fmt.Errorf("%w: %d > %d", errExcessGas, t.op.Gas, dynamic.MinTarget)
	}

	// TODO(JonathanOppenheimer): Consider raising the gas per byte of
	// cross-chain txs so that byte-heavy txs pay their fair share.

	// We must verify the tx against a state that is at least as high as the
	// last block processed by the pool subscription.
	//
	// Verifying against an older state risks admitting a tx that would never
	// be evicted.
	p.executionLock.RLock()
	defer p.executionLock.RUnlock()

	if err := tx.VerifyCredentials(p.snowCtx.SharedMemory); err != nil {
		return fmt.Errorf("%w: %w", errVerifyCredentials, err)
	}
	if err := p.verifyOp(t.op); err != nil {
		return fmt.Errorf("%w: %w", errVerifyState, err)
	}

	p.lock.Lock()
	defer p.lock.Unlock()

	if _, ok := p.byID[t.id]; ok {
		return ErrAlreadyKnown
	}

	for input := range t.inputs {
		if conflictID, ok := p.utxos.GetKey(input); ok {
			conflict := p.byID[conflictID]
			if t.op.GasFeeCap.Cmp(&conflict.op.GasFeeCap) <= 0 {
				return errInsufficientFee
			}
		}
	}
	p.removeConflicts(t.inputs)

	if len(p.byID) >= p.maxSize {
		// maxSize > 0 and the pool is full, so the tree is non-empty.
		cheapest, _ := p.byPrice.Max()
		if t.op.GasFeeCap.Cmp(&cheapest.op.GasFeeCap) <= 0 {
			return errInsufficientFee
		}
		p.removeConflicts(cheapest.inputs)
	}

	p.add(t)
	return nil
}

// Close releases all allocated resources.
func (p *Txpool) Close() {
	p.sub.Unsubscribe()
	p.wg.Wait()
}

func (p *Txpool) verifyOp(op hook.Op) error {
	// [libevm.StateReader] is not thread-safe, we must lock it even for
	// read-only operations.
	p.stateLock.Lock()
	defer p.stateLock.Unlock()

	return verifyOp(p.state, op)
}

// inputUTXOs returns the union of all UTXO IDs consumed by transactions in b,
// covering both EVM-native account+nonce inputs and cross-chain inputs.
func inputUTXOs(b *types.Block, c *params.ChainConfig) (set.Set[ids.ID], error) {
	var (
		ethTxs = b.Transactions()
		signer = blocks.Signer(b, c)
		inputs = set.NewSet[ids.ID](len(ethTxs))
	)
	for i, t := range ethTxs {
		sender, err := signer.Sender(t)
		if err != nil {
			return nil, fmt.Errorf("getting sender of tx %s (%d): %w", t.Hash(), i, err)
		}
		inputs.Add(tx.AccountInputID(sender, t.Nonce()))
	}

	avaxTxs, err := tx.FromBlock(c, b)
	if err != nil {
		return nil, fmt.Errorf("parsing txs: %w", err)
	}
	for _, t := range avaxTxs {
		inputs.Union(t.InputIDs())
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

// priceTreeDegree is the degree of [Pending.byPrice]. Every node but the root
// holds between degree-1 and 2*degree-1 transactions, so the tree stays a few
// levels deep for the pool sizes we expect while keeping the per-node copy
// that copy-on-write mutations pay after [Pending.Iter] small.
const priceTreeDegree = 16

// Pending stores transactions that are eligible for inclusion in a future
// block, indexed for fast conflict lookup and ordered by gas price.
type Pending struct {
	lock sync.RWMutex
	cond *lock.Cond

	// byID indexes every pooled transaction by its ID.
	byID map[ids.ID]*txData
	// byPrice orders every pooled transaction by [txData.lessByPrice]:
	// decreasing gas price, so that the first item is the most valuable to
	// include in a block and the last is the eviction candidate when the pool
	// is full.
	//
	// The tree is copy-on-write. [Pending.Iter] clones it in O(1) and walks
	// the clone without holding the pool's lock, so iteration neither copies
	// the pool nor blocks writers for its duration.
	byPrice *btree.BTreeG[*txData]
	// utxos maps a txID to the set of utxoIDs it consumes.
	utxos *setmap.SetMap[ids.ID, ids.ID]
}

// NewPending constructs an empty set of [Pending] transactions.
func NewPending() *Pending {
	p := &Pending{
		byID:    make(map[ids.ID]*txData),
		byPrice: btree.NewG(priceTreeDegree, (*txData).lessByPrice),
		utxos:   setmap.New[ids.ID, ids.ID](),
	}
	p.cond = lock.NewCond(p.lock.RLocker())
	return p
}

// Iter returns an iterator over the pool's transactions in decreasing gas
// price order. Transactions with equal gas prices are yielded in ascending ID
// order, so the order is the same regardless of insertion order.
//
// The iterator ranges over a snapshot of the pool taken when Iter is called:
// transactions added or removed afterwards are not observed. Taking the
// snapshot costs O(1) regardless of the pool's size, and the pool's lock is
// not held while iterating, so callers may perform slow work (such as
// verifying credentials) and call other [Pending] methods from within the
// loop.
func (p *Pending) Iter() iter.Seq[*tx.Tx] {
	// Clone is O(1) but it replaces the tree's copy-on-write context and
	// MUST NOT run concurrently with another Clone, so it requires the write
	// lock even though the set of transactions is unchanged. Subsequent
	// mutations of byPrice copy only the nodes they touch, leaving the
	// snapshot intact.
	p.lock.Lock()
	snapshot := p.byPrice.Clone()
	p.lock.Unlock()

	return func(yield func(*tx.Tx) bool) {
		snapshot.Ascend(func(t *txData) bool {
			return yield(t.tx)
		})
	}
}

// Len returns the number of transactions currently in the pool.
func (p *Pending) Len() int {
	p.lock.RLock()
	defer p.lock.RUnlock()

	return len(p.byID)
}

// Has reports whether txID is in the pool.
func (p *Pending) Has(txID ids.ID) bool {
	p.lock.RLock()
	defer p.lock.RUnlock()

	_, ok := p.byID[txID]
	return ok
}

// AwaitTxs blocks until at least one transaction is in the pool or ctx is
// cancelled.
func (p *Pending) AwaitTxs(ctx context.Context) error {
	p.lock.RLock()
	defer p.lock.RUnlock()

	for len(p.byID) == 0 {
		if err := p.cond.Wait(ctx); err != nil {
			return err
		}
	}

	return nil
}

func (p *Pending) removeConflicts(utxos set.Set[ids.ID]) {
	for _, removed := range p.utxos.DeleteOverlapping(utxos) {
		p.remove(removed.Key)
	}
}

// remove deletes the transaction with txID from byID and byPrice, if present.
// It assumes that the transaction's inputs have already been removed from
// utxos.
func (p *Pending) remove(txID ids.ID) {
	t, ok := p.byID[txID]
	if !ok {
		return
	}
	delete(p.byID, txID)
	p.byPrice.Delete(t)
}

// add inserts t into the pool. It assumes there are no existing conflicts.
func (p *Pending) add(t *txData) {
	p.utxos.Put(t.id, t.inputs)
	p.byID[t.id] = t
	p.byPrice.ReplaceOrInsert(t)
	p.cond.Broadcast()
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

// lessByPrice orders transactions by decreasing gas price, breaking ties by
// ascending ID. The tie-break makes the order total: every transaction has
// exactly one position in [Pending.byPrice], which the tree relies on to find
// it again on removal, and it makes the order in which block builders see
// transactions independent of the order in which they arrived.
func (t *txData) lessByPrice(o *txData) bool {
	if c := t.op.GasFeeCap.Cmp(&o.op.GasFeeCap); c != 0 {
		return c > 0
	}
	return t.id.Compare(o.id) < 0
}

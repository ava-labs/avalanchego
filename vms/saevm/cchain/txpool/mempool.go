// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txpool

import (
	"errors"
	"fmt"
	"sync"

	"github.com/ava-labs/libevm/core"
	"github.com/ava-labs/libevm/core/state"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/event"
	"github.com/ava-labs/libevm/log"
	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/graft/coreth/params"
	"github.com/ava-labs/avalanchego/graft/coreth/plugin/evm/customtypes"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/saevm/blocks"
	"github.com/ava-labs/avalanchego/vms/saevm/cchain/tx"
	"github.com/ava-labs/avalanchego/vms/saevm/hook"
	"github.com/ava-labs/avalanchego/vms/saevm/sae"
)

const maxSize = 4096

var (
	ErrAlreadyKnown    = errors.New("transaction already in mempool")
	errInsufficientFee = errors.New("insufficient fee")
)

// Mempool is a simple mempool for atomic transactions
type Mempool struct {
	*Txs

	ctx *snow.Context
	sub event.Subscription

	// stateLock is ordered before [Txs.lock]. Trying to grab stateLock with
	// [Txs.lock] held will result in a deadlock.
	stateLock sync.RWMutex
	state     *state.StateDB
}

func New(
	txs *Txs,
	ctx *snow.Context,
	vm *sae.VM,
) (*Mempool, error) {
	// c is unbuffered to guarantee that the mempool never holds a reference to
	// state older than the last-settled state. SAE does not guarantee that such
	// a state exists on disk anymore.
	//
	// TODO:(StephenButtolph): If this is identified as a performance issue, we
	// can likely make this channel buffered, as it seems highly unlikely for
	// this goroutine not to be able to keep up with the last executed state.
	c := make(chan core.ChainHeadEvent)
	sub := vm.SubscribeChainHeadEvent(c)

	state, err := vm.LastExecutedState()
	if err != nil {
		sub.Unsubscribe()
		return nil, fmt.Errorf("getting last executed state: %w", err)
	}

	// height must be populated after [rpc.GethBackends.SubscribeChainHeadEvent]
	// is called, to ensure we do not miss an update.
	m := &Mempool{
		Txs:   txs,
		ctx:   ctx,
		sub:   sub,
		state: state,
	}

	chainConfig := vm.GethRPCBackends().ChainConfig()
	go func() {
		defer sub.Unsubscribe()
		for {
			select {
			case e := <-c:
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

				state, err := vm.LastExecutedState()
				if err != nil {
					ctx.Log.Error("unable to get latest executed state",
						zap.Error(err),
					)
					continue
				}

				m.stateLock.Lock()
				m.lock.Lock()

				m.removeConflicts(inputs)
				m.state = state

				m.lock.Unlock()
				m.stateLock.Unlock()
			case err := <-sub.Err():
				if err != nil {
					ctx.Log.Error("mempool subscription failed",
						zap.Error(err),
					)
				}
				return
			}
		}
	}()
	return m, nil
}

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

	atomicTxs, err := tx.ParseSlice(customtypes.BlockExtData(b))
	if err != nil {
		log.Error("parsing atomic txs",
			zap.Error(err),
		)
		return nil, fmt.Errorf("parsing atomic txs: %w", err)
	}
	for _, tx := range atomicTxs {
		inputs.Union(tx.InputIDs())
	}
	return inputs, nil
}

func (m *Mempool) Add(rawTx *tx.Tx) error {
	if err := rawTx.SanityCheck(m.ctx); err != nil {
		return fmt.Errorf("tx failed sanity check: %w", err)
	}

	tx, err := NewTx(rawTx, m.ctx.AVAXAssetID)
	if err != nil {
		return err
	}

	// We must verify the tx against a state that is at least as high as the
	// last block processed by the mempool subscription.
	//
	// If we verify the tx against an older state, then we may allow a tx into
	// the mempool which would never be evicted.
	m.stateLock.RLock()
	defer m.stateLock.RUnlock()

	// [snow.Context.SharedMemory] is updated in AfterExecutingBlock, which will
	// be done before the event on the subscription, so we may verify against a
	// newer state, but never an older state.
	if err := rawTx.VerifyCredentials(m.ctx.SharedMemory); err != nil {
		return fmt.Errorf("tx failed credential verification: %w", err)
	}
	if err := verifyOp(m.state, tx.Op); err != nil {
		return fmt.Errorf("tx failed state verification: %w", err)
	}

	m.lock.Lock()
	defer m.lock.Unlock()

	if _, ok := m.txs.Get(tx.ID); ok {
		return ErrAlreadyKnown
	}

	for input := range tx.Inputs {
		if conflictID, ok := m.utxos.GetKey(input); ok {
			conflict, _ := m.txs.Get(conflictID)
			if tx.GasPrice.Cmp(&conflict.GasPrice) <= 0 {
				return errInsufficientFee
			}
		}
	}
	m.removeConflicts(tx.Inputs)

	if m.txs.Len() >= maxSize {
		_, cheap, _ := m.txs.Peek()
		if tx.GasPrice.Cmp(&cheap.GasPrice) <= 0 {
			return errInsufficientFee
		}
		m.removeConflicts(cheap.Inputs)
	}

	m.utxos.Put(tx.ID, tx.Inputs)
	m.txs.Push(tx.ID, tx)
	m.cond.Broadcast()
	return nil
}

var (
	errNonceMismatch     = errors.New("nonce mismatch")
	errInsufficientFunds = errors.New("insufficient funds")
)

// TODO(StephenButtolph): Is there a way to deduplicate this logic? It is very
// similar to the behavior that we perform during worstcase verification.
func verifyOp(state *state.StateDB, op hook.Op) error {
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

func (m *Mempool) Close() {
	m.sub.Unsubscribe()
}

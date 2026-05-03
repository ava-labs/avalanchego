// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txpool

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/ava-labs/libevm/core"
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
	"github.com/ava-labs/avalanchego/vms/saevm/sae/rpc"

	ethrpc "github.com/ava-labs/libevm/rpc"
)

const maxSize = 4096

var (
	ErrAlreadyKnown    = errors.New("transaction already in mempool")
	errInsufficientFee = errors.New("insufficient fee")
)

// Mempool is a simple mempool for atomic transactions
type Mempool struct {
	*Txs

	ctx      *snow.Context
	backends rpc.GethBackends
	sub      event.Subscription

	// heightLock is ordered before [Txs.lock]. Trying to grab heightLock with
	// [Txs.lock] held will result in a deadlock.
	heightLock sync.RWMutex
	height     uint64
}

func New(
	txs *Txs,
	ctx *snow.Context,
	backends rpc.GethBackends,
) *Mempool {
	c := make(chan core.ChainHeadEvent, 16)
	sub := backends.SubscribeChainHeadEvent(c)

	// height must be populated after [rpc.GethBackends.SubscribeChainHeadEvent]
	// is called, to ensure we do not miss an update.
	m := &Mempool{
		Txs:      txs,
		ctx:      ctx,
		backends: backends,
		sub:      sub,
		height:   backends.CurrentBlock().Number.Uint64(),
	}

	chainConfig := backends.ChainConfig()
	go func() {
		defer sub.Unsubscribe()
		for {
			select {
			case e := <-c:
				b := e.Block
				height := b.NumberU64()
				inputs, err := inputUTXOs(b, chainConfig)
				if err != nil {
					ctx.Log.Error("unable to get inputs from block",
						zap.Stringer("blockHash", b.Hash()),
						zap.Uint64("blockNumber", height),
						zap.Error(err),
					)
					continue
				}
				m.advanceHeight(height, inputs)
			case err := <-sub.Err():
				if err != nil {
					ctx.Log.Warn("mempool subscription failed",
						zap.Error(err),
					)
				}
				return
			}
		}
	}()
	return m
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
		inputs.Add(tx.NonceInputID(sender, t.Nonce()))
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

func (m *Mempool) advanceHeight(height uint64, inputs set.Set[ids.ID]) {
	m.heightLock.Lock()
	defer m.heightLock.Unlock()
	m.lock.Lock()
	defer m.lock.Unlock()

	m.removeConflicts(inputs)
	m.height = height
}

func (m *Mempool) Add(rawTx *tx.Tx) error {
	ctx := context.TODO()

	if err := rawTx.SanityCheck(m.ctx); err != nil {
		return fmt.Errorf("tx failed sanity check: %w", err)
	}

	tx, err := NewTx(rawTx, m.ctx.AVAXAssetID)
	if err != nil {
		return err
	}

	m.heightLock.RLock()
	defer m.heightLock.RUnlock()

	// We must verify the tx against a state that is at least as high as the
	// last block processed by the mempool subscription.
	//
	// If we verify the tx against an older state, then we may allow a tx into
	// the mempool which would never be evicted.
	{
		// [tx.Tx.VerifyCredentials] reads from [snow.Context.SharedMemory]
		// which is updated in hook.Points.AfterExecutingBlock, which will be
		// done before the event on the subscription, so we may verify against a
		// newer state, but never an older state.
		if err := rawTx.VerifyCredentials(m.ctx.SharedMemory); err != nil {
			return fmt.Errorf("tx failed credential verification: %w", err)
		}

		// TODO: Using the rpc backend is gross. We should make something easier
		// to use for this.
		// TODO: Is it okay for us to be opening so many state dbs?
		state, _, err := m.backends.StateAndHeaderByNumber(ctx, ethrpc.BlockNumber(m.height)) //#nosec G115 -- block height won't overflow
		if err != nil {
			return fmt.Errorf("problem getting latest state: %w", err)
		}
		if err := rawTx.VerifyState(m.ctx.AVAXAssetID, state); err != nil {
			return fmt.Errorf("tx failed state verification: %w", err)
		}
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

func (m *Mempool) Close() {
	m.sub.Unsubscribe()
}

// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package builder

import (
	"context"
	"sync"
	"time"

	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/database/versiondb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/utils/linked"
	"github.com/ava-labs/avalanchego/utils/lock"
	"github.com/ava-labs/avalanchego/vms/example/xsvm/chain"
	"github.com/ava-labs/avalanchego/vms/example/xsvm/execute"
	"github.com/ava-labs/avalanchego/vms/example/xsvm/tx"

	smblock "github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	xsblock "github.com/ava-labs/avalanchego/vms/example/xsvm/block"
)

const MaxTxsPerBlock = 10

var _ Builder = (*builder)(nil)

type Builder interface {
	SetPreference(preferred ids.ID)
	AddTx(ctx context.Context, tx *tx.Tx) error
	WaitForEvent(ctx context.Context) (common.Message, error)
	BuildBlock(ctx context.Context, blockContext *smblock.Context) (chain.Block, error)
}

type builder struct {
	chainContext *snow.Context
	chain        chain.Chain

	preference ids.ID
	// pendingTxsCond is awoken once there is at least one pending transaction.
	pendingTxsCond *lock.Cond
	pendingTxs     *linked.Hashmap[ids.ID, *tx.Tx]
}

func New(chainContext *snow.Context, chain chain.Chain) Builder {
	return &builder{
		chainContext:   chainContext,
		chain:          chain,
		preference:     chain.LastAccepted(),
		pendingTxsCond: lock.NewCond(&sync.Mutex{}),
		pendingTxs:     linked.NewHashmap[ids.ID, *tx.Tx](),
	}
}

func (b *builder) SetPreference(preferred ids.ID) {
	b.preference = preferred
}

func (b *builder) AddTx(_ context.Context, newTx *tx.Tx) error {
	b.chainContext.Log.Info("adding tx to builder")
	// TODO: verify [tx] against the currently preferred state
	txID, err := newTx.ID()
	if err != nil {
		return err
	}

	b.pendingTxsCond.L.Lock()
	defer b.pendingTxsCond.L.Unlock()

	b.pendingTxs.Put(txID, newTx)
	b.pendingTxsCond.Broadcast()
	return nil
}

func (b *builder) WaitForEvent(ctx context.Context) (common.Message, error) {
	b.pendingTxsCond.L.Lock()
	defer b.pendingTxsCond.L.Unlock()

	for b.pendingTxs.Len() == 0 {
		b.chainContext.Log.Info("waiting for tx to be added to builder")
		if err := b.pendingTxsCond.Wait(ctx); err != nil {
			return 0, err
		}
	}

	b.chainContext.Log.Info("returning pending txs")

	return common.PendingTxs, nil
}

func (b *builder) BuildBlock(ctx context.Context, blockContext *smblock.Context) (chain.Block, error) {
	preferredBlk, err := b.chain.GetBlock(b.preference)
	if err != nil {
		return nil, err
	}

	preferredState, err := preferredBlk.State()
	if err != nil {
		return nil, err
	}

	parentTimestamp := preferredBlk.Timestamp()
	timestamp := time.Now().Truncate(time.Second)
	if timestamp.Before(parentTimestamp) {
		timestamp = parentTimestamp
	}

	wipBlock := xsblock.Stateless{
		ParentID:  b.preference,
		Timestamp: timestamp.Unix(),
		Height:    preferredBlk.Height() + 1,
	}

	b.pendingTxsCond.L.Lock()
	defer b.pendingTxsCond.L.Unlock()

	currentState := versiondb.New(preferredState)
	for len(wipBlock.Txs) < MaxTxsPerBlock {
		txID, currentTx, exists := b.pendingTxs.Oldest()
		if !exists {
			break
		}
		b.pendingTxs.Delete(txID)

		sender, err := currentTx.SenderID()
		if err != nil {
			// This tx was invalid, drop it and continue block building
			continue
		}

		txState := versiondb.New(currentState)
		txExecutor := execute.Tx{
			Context:      ctx,
			ChainContext: b.chainContext,
			Database:     txState,
			BlockContext: blockContext,
			TxID:         txID,
			Sender:       sender,
			// TODO: populate fees
		}
		if err := currentTx.Unsigned.Visit(&txExecutor); err != nil {
			// This tx was invalid, drop it and continue block building
			continue
		}
		if err := txState.Commit(); err != nil {
			return nil, err
		}

		wipBlock.Txs = append(wipBlock.Txs, currentTx)
	}

	b.chainContext.Log.Info("built block", zap.Int("num txs", len(wipBlock.Txs)))

	return b.chain.NewBlock(&wipBlock)
}

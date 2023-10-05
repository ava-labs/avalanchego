// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package builder

import (
	"context"
	"time"

	"github.com/ava-labs/avalanchego/database/versiondb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/utils/linkedhashmap"
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
	BuildBlock(ctx context.Context, blockContext *smblock.Context) (chain.Block, error)
}

type builder struct {
	chainContext *snow.Context
	engineChan   chan<- common.Message
	chain        chain.Chain

	pendingTxs linkedhashmap.LinkedHashmap[ids.ID, *tx.Tx]
	preference ids.ID
}

func New(chainContext *snow.Context, engineChan chan<- common.Message, chain chain.Chain) Builder {
	return &builder{
		chainContext: chainContext,
		engineChan:   engineChan,
		chain:        chain,

		pendingTxs: linkedhashmap.New[ids.ID, *tx.Tx](),
		preference: chain.LastAccepted(),
	}
}

func (b *builder) SetPreference(preferred ids.ID) {
	b.preference = preferred
}

func (b *builder) AddTx(_ context.Context, newTx *tx.Tx) error {
	// TODO: verify [tx] against the currently preferred state
	txID, err := newTx.ID()
	if err != nil {
		return err
	}
	b.pendingTxs.Put(txID, newTx)
	select {
	case b.engineChan <- common.PendingTxs:
	default:
	}
	return nil
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

	defer func() {
		if b.pendingTxs.Len() == 0 {
			return
		}
		select {
		case b.engineChan <- common.PendingTxs:
		default:
		}
	}()

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
	return b.chain.NewBlock(&wipBlock)
}

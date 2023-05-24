// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package network

import (
	"context"
	"sync"

	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/cache"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/vms/avm/blocks/executor"
	"github.com/ava-labs/avalanchego/vms/avm/txs"
	"github.com/ava-labs/avalanchego/vms/avm/txs/mempool"
	"github.com/ava-labs/avalanchego/vms/components/message"
)

// We allow [recentTxsCacheSize] to be fairly large because we only store hashes
// in the cache, not entire transactions.
const recentTxsCacheSize = 512

var _ Network = (*network)(nil)

type Network interface {
	common.AppHandler

	// IssueTx verifies the transaction at the currently preferred state, adds
	// it to the mempool, and gossips it to the network.
	//
	// Invariant: Assumes the context lock is held.
	IssueTx(context.Context, *txs.Tx) error
}

type network struct {
	// We embed a noop handler for all unhandled messages
	common.AppHandler

	ctx       *snow.Context
	parser    txs.Parser
	manager   executor.Manager
	mempool   mempool.Mempool
	appSender common.AppSender

	// gossip related attributes
	recentTxsLock sync.Mutex
	recentTxs     *cache.LRU[ids.ID, struct{}]
}

func New(
	ctx *snow.Context,
	parser txs.Parser,
	manager executor.Manager,
	mempool mempool.Mempool,
	appSender common.AppSender,
) Network {
	return &network{
		AppHandler: common.NewNoOpAppHandler(ctx.Log),

		ctx:       ctx,
		parser:    parser,
		manager:   manager,
		mempool:   mempool,
		appSender: appSender,

		recentTxs: &cache.LRU[ids.ID, struct{}]{
			Size: recentTxsCacheSize,
		},
	}
}

func (n *network) AppGossip(ctx context.Context, nodeID ids.NodeID, msgBytes []byte) error {
	n.ctx.Log.Debug("called AppGossip message handler",
		zap.Stringer("nodeID", nodeID),
		zap.Int("messageLen", len(msgBytes)),
	)

	msgIntf, err := message.Parse(msgBytes)
	if err != nil {
		n.ctx.Log.Debug("dropping AppGossip message",
			zap.String("reason", "failed to parse message"),
		)
		return nil
	}

	msg, ok := msgIntf.(*message.Tx)
	if !ok {
		n.ctx.Log.Debug("dropping unexpected message",
			zap.Stringer("nodeID", nodeID),
		)
		return nil
	}

	tx, err := n.parser.ParseTx(msg.Tx)
	if err != nil {
		n.ctx.Log.Verbo("received invalid tx",
			zap.Stringer("nodeID", nodeID),
			zap.Binary("tx", msg.Tx),
			zap.Error(err),
		)
		return nil
	}

	// We need to grab the context lock here to avoid racy behavior with
	// transaction verification + mempool modifications.
	n.ctx.Lock.Lock()
	err = n.issueTx(tx)
	n.ctx.Lock.Unlock()
	if err == nil {
		txID := tx.ID()
		n.gossipTx(ctx, txID, msgBytes)
	}
	return nil
}

func (n *network) IssueTx(ctx context.Context, tx *txs.Tx) error {
	if err := n.issueTx(tx); err != nil {
		return err
	}

	txBytes := tx.Bytes()
	msg := &message.Tx{
		Tx: txBytes,
	}
	msgBytes, err := message.Build(msg)
	if err != nil {
		return err
	}

	txID := tx.ID()
	n.gossipTx(ctx, txID, msgBytes)
	return nil
}

// returns nil if the tx is in the mempool
func (n *network) issueTx(tx *txs.Tx) error {
	txID := tx.ID()
	if n.mempool.Has(txID) {
		// The tx is already in the mempool
		return nil
	}

	if reason := n.mempool.GetDropReason(txID); reason != nil {
		// If the tx is being dropped - just ignore it
		//
		// TODO: Should we allow re-verification of the transaction even if it
		// failed previously?
		return reason
	}

	// Verify the tx at the currently preferred state
	if err := n.manager.VerifyTx(tx); err != nil {
		n.ctx.Log.Debug("tx failed verification",
			zap.Stringer("txID", txID),
			zap.Error(err),
		)

		n.mempool.MarkDropped(txID, err)
		return err
	}

	if err := n.mempool.Add(tx); err != nil {
		n.ctx.Log.Debug("tx failed to be added to the mempool",
			zap.Stringer("txID", txID),
			zap.Error(err),
		)

		n.mempool.MarkDropped(txID, err)
		return err
	}

	n.mempool.RequestBuildBlock()
	return nil
}

func (n *network) gossipTx(ctx context.Context, txID ids.ID, msgBytes []byte) {
	// This lock is just to ensure there isn't racy behavior between checking if
	// the tx was gossiped and marking the tx as gossiped.
	n.recentTxsLock.Lock()
	_, has := n.recentTxs.Get(txID)
	n.recentTxs.Put(txID, struct{}{})
	n.recentTxsLock.Unlock()

	// Don't gossip a transaction if it has been recently gossiped.
	if has {
		return
	}

	n.ctx.Log.Debug("gossiping tx",
		zap.Stringer("txID", txID),
	)

	if err := n.appSender.SendAppGossip(ctx, msgBytes); err != nil {
		n.ctx.Log.Error("failed to gossip tx",
			zap.Stringer("txID", txID),
			zap.Error(err),
		)
	}
}

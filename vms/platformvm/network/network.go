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
	"github.com/ava-labs/avalanchego/vms/components/message"
	"github.com/ava-labs/avalanchego/vms/platformvm/block/executor"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs/mempool"
)

// We allow [recentCacheSize] to be fairly large because we only store hashes
// in the cache, not entire transactions.
const recentCacheSize = 512

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

	ctx                       *snow.Context
	manager                   executor.Manager
	mempool                   mempool.Mempool
	partialSyncPrimaryNetwork bool
	appSender                 common.AppSender

	// gossip related attributes
	recentTxsLock sync.Mutex
	recentTxs     *cache.LRU[ids.ID, struct{}]
}

func New(
	ctx *snow.Context,
	manager executor.Manager,
	mempool mempool.Mempool,
	partialSyncPrimaryNetwork bool,
	appSender common.AppSender,
) Network {
	return &network{
		AppHandler: common.NewNoOpAppHandler(ctx.Log),

		ctx:                       ctx,
		manager:                   manager,
		mempool:                   mempool,
		partialSyncPrimaryNetwork: partialSyncPrimaryNetwork,
		appSender:                 appSender,
		recentTxs:                 &cache.LRU[ids.ID, struct{}]{Size: recentCacheSize},
	}
}

func (n *network) AppGossip(ctx context.Context, nodeID ids.NodeID, msgBytes []byte) error {
	n.ctx.Log.Debug("called AppGossip message handler",
		zap.Stringer("nodeID", nodeID),
		zap.Int("messageLen", len(msgBytes)),
	)

	if n.partialSyncPrimaryNetwork {
		n.ctx.Log.Debug("dropping AppGossip message",
			zap.String("reason", "primary network is not being fully synced"),
		)
		return nil
	}

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

	tx, err := txs.Parse(txs.Codec, msg.Tx)
	if err != nil {
		n.ctx.Log.Verbo("received invalid tx",
			zap.Stringer("nodeID", nodeID),
			zap.Binary("tx", msg.Tx),
			zap.Error(err),
		)
		return nil
	}
	txID := tx.ID()

	// We need to grab the context lock here to avoid racy behavior with
	// transaction verification + mempool modifications.
	//
	// Invariant: tx should not be referenced again without the context lock
	// held to avoid any data races.
	n.ctx.Lock.Lock()
	defer n.ctx.Lock.Unlock()

	if reason := n.mempool.GetDropReason(txID); reason != nil {
		// If the tx is being dropped - just ignore it
		return nil
	}
	if err := n.issueTx(tx); err == nil {
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

	// Verify the tx at the currently preferred state
	if err := n.manager.VerifyTx(tx); err != nil {
		n.ctx.Log.Debug("tx failed verification",
			zap.Stringer("txID", txID),
			zap.Error(err),
		)

		n.mempool.MarkDropped(txID, err)
		return err
	}

	// If we are partially syncing the Primary Network, we should not be
	// maintaining the transaction mempool locally.
	if n.partialSyncPrimaryNetwork {
		return nil
	}

	if err := n.mempool.Add(tx); err != nil {
		n.ctx.Log.Debug("tx failed to be added to the mempool",
			zap.Stringer("txID", txID),
			zap.Error(err),
		)

		n.mempool.MarkDropped(txID, err)
		return err
	}

	return nil
}

func (n *network) gossipTx(ctx context.Context, txID ids.ID, msgBytes []byte) {
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

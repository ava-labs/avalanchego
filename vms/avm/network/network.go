// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package network

import (
	"context"
	"fmt"
	"sync"

	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/cache"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/vms/avm/block/executor"
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

	// IssueTx attempts to add a tx to the mempool, after verifying it against
	// the preferred state. If the tx is added to the mempool, it will attempt
	// to push gossip the tx to random peers in the network.
	//
	// If the tx is already in the mempool, mempool.ErrDuplicateTx will be
	// returned.
	// If the tx is not added to the mempool, an error will be returned.
	//
	// Invariant: The context lock is not held
	IssueTx(context.Context, *txs.Tx) error

	// IssueVerifiedTx attempts to add a tx to the mempool. If the tx is added
	// to the mempool, it will attempt to push gossip the tx to random peers in
	// the network.
	//
	// If the tx is already in the mempool, mempool.ErrDuplicateTx will be
	// returned.
	// If the tx is not added to the mempool, an error will be returned.
	IssueVerifiedTx(context.Context, *txs.Tx) error
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

	if err := n.issueTx(tx); err == nil {
		txID := tx.ID()
		n.gossipTxMessage(ctx, txID, msgBytes)
	}
	return nil
}

func (n *network) IssueTx(ctx context.Context, tx *txs.Tx) error {
	if err := n.issueTx(tx); err != nil {
		return err
	}
	return n.gossipTx(ctx, tx)
}

func (n *network) IssueVerifiedTx(ctx context.Context, tx *txs.Tx) error {
	if err := n.issueVerifiedTx(tx); err != nil {
		return err
	}
	return n.gossipTx(ctx, tx)
}

func (n *network) issueTx(tx *txs.Tx) error {
	txID := tx.ID()
	if _, ok := n.mempool.Get(txID); ok {
		return fmt.Errorf("attempted to issue %w: %s ", mempool.ErrDuplicateTx, txID)
	}

	if reason := n.mempool.GetDropReason(txID); reason != nil {
		// If the tx is being dropped - just ignore it
		//
		// TODO: Should we allow re-verification of the transaction even if it
		// failed previously?
		return reason
	}

	// Verify the tx at the currently preferred state
	n.ctx.Lock.Lock()
	err := n.manager.VerifyTx(tx)
	n.ctx.Lock.Unlock()
	if err != nil {
		n.ctx.Log.Debug("tx failed verification",
			zap.Stringer("txID", txID),
			zap.Error(err),
		)

		n.mempool.MarkDropped(txID, err)
		return err
	}

	return n.issueVerifiedTx(tx)
}

func (n *network) issueVerifiedTx(tx *txs.Tx) error {
	if err := n.mempool.Add(tx); err != nil {
		txID := tx.ID()
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

func (n *network) gossipTx(ctx context.Context, tx *txs.Tx) error {
	txBytes := tx.Bytes()
	msg := &message.Tx{
		Tx: txBytes,
	}
	msgBytes, err := message.Build(msg)
	if err != nil {
		return err
	}

	txID := tx.ID()
	n.gossipTxMessage(ctx, txID, msgBytes)
	return nil
}

func (n *network) gossipTxMessage(ctx context.Context, txID ids.ID, msgBytes []byte) {
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

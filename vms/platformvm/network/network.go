// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package network

import (
	"context"
	"fmt"
	"time"

	"github.com/ava-labs/avalanchego/cache"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/vms/platformvm/message"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"go.uber.org/zap"
)

const (
	// We allow [recentCacheSize] to be fairly large because we only store hashes
	// in the cache, not entire transactions.
	recentCacheSize = 512
)

var _ Network = (*network)(nil)

type Gossipper interface {
	// GossipTx gossips the provided txs
	GossipTx(tx *txs.Tx) error
}

type Network interface {
	common.AppHandler

	Gossipper
}

type network struct {
	ctx *snow.Context

	// gossip related attributes
	appSender common.AppSender
	recentTxs *cache.LRU[ids.ID, struct{}]

	gossipHandler message.GossipHandler
}

func NewNetwork(
	ctx *snow.Context,
	appSender common.AppSender,
	gossipHandler message.GossipHandler,
) Network {
	return &network{
		ctx:           ctx,
		appSender:     appSender,
		recentTxs:     &cache.LRU[ids.ID, struct{}]{Size: recentCacheSize},
		gossipHandler: gossipHandler,
	}
}

func (*network) CrossChainAppRequestFailed(context.Context, ids.ID, uint32) error {
	// This VM currently only supports gossiping of txs, so there are no
	// requests.
	return nil
}

func (*network) CrossChainAppRequest(context.Context, ids.ID, uint32, time.Time, []byte) error {
	// This VM currently only supports gossiping of txs, so there are no
	// requests.
	return nil
}

func (*network) CrossChainAppResponse(context.Context, ids.ID, uint32, []byte) error {
	// This VM currently only supports gossiping of txs, so there are no
	// requests.
	return nil
}

func (*network) AppRequestFailed(context.Context, ids.NodeID, uint32) error {
	// This VM currently only supports gossiping of txs, so there are no
	// requests.
	return nil
}

func (*network) AppRequest(context.Context, ids.NodeID, uint32, time.Time, []byte) error {
	// This VM currently only supports gossiping of txs, so there are no
	// requests.
	return nil
}

func (*network) AppResponse(context.Context, ids.NodeID, uint32, []byte) error {
	// This VM currently only supports gossiping of txs, so there are no
	// requests.
	return nil
}

func (n *network) AppGossip(_ context.Context, nodeID ids.NodeID, msgBytes []byte) error {
	n.ctx.Log.Debug("called AppGossip message handler",
		zap.Stringer("nodeID", nodeID),
		zap.Int("messageLen", len(msgBytes)),
	)
	gossipMsg, err := message.Parse(msgBytes)
	if err != nil {
		n.ctx.Log.Debug("dropping AppGossip message",
			zap.String("reason", "failed to parse message"),
			zap.Error(err),
		)
		return nil
	}

	return gossipMsg.Handle(n.gossipHandler, nodeID)
}

func (n *network) GossipTx(tx *txs.Tx) error {
	txID := tx.ID()
	// Don't gossip a transaction if it has been recently gossiped.
	if _, has := n.recentTxs.Get(txID); has {
		return nil
	}
	n.recentTxs.Put(txID, struct{}{})

	n.ctx.Log.Debug("gossiping tx",
		zap.Stringer("txID", txID),
	)

	msg := &message.TxGossip{Tx: tx.Bytes()}
	msgBytes, err := message.Build(msg)
	if err != nil {
		return fmt.Errorf("GossipTx: failed to build Tx message: %w", err)
	}
	return n.appSender.SendAppGossip(context.TODO(), msgBytes)
}

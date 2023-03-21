// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package network

import (
	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/vms/components/message"
	"github.com/ava-labs/avalanchego/vms/platformvm/blocks/builder"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
)

var _ message.Handler = (*gossipHandler)(nil)

// gossipHandler handles incoming gossip messages
type gossipHandler struct {
	ctx        *snow.Context
	blkBuilder builder.Builder
}

func NewGossipHandler(ctx *snow.Context, blkBuilder builder.Builder) message.Handler {
	return &gossipHandler{
		ctx:        ctx,
		blkBuilder: blkBuilder,
	}
}

func (g *gossipHandler) HandleTxGossip(nodeID ids.NodeID, msg *message.TxGossip) error {
	g.ctx.Log.Debug("called HandleTx message handler",
		zap.Stringer("nodeID", nodeID),
	)

	tx, err := txs.Parse(txs.Codec, msg.Tx)
	if err != nil {
		g.ctx.Log.Verbo("received invalid tx",
			zap.Stringer("nodeID", nodeID),
			zap.Binary("tx", msg.Tx),
			zap.Error(err),
		)
		return nil
	}

	// We need to grab the context lock here to avoid racy behavior with
	// transaction verification + mempool modifications.
	g.ctx.Lock.Lock()
	defer g.ctx.Lock.Unlock()

	if reason := g.blkBuilder.GetDropReason(tx.ID()); reason != nil {
		// If the tx is being dropped - just ignore it
		return nil
	}

	if err := g.blkBuilder.AddUnverifiedTx(tx); err != nil {
		g.ctx.Log.Debug("tx failed verification",
			zap.Stringer("nodeID", nodeID),
			zap.Error(err),
		)
	}
	return nil
}

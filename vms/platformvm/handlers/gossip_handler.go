// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package handlers

import (
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/vms/platformvm/blocks/builder"
	"github.com/ava-labs/avalanchego/vms/platformvm/message"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"go.uber.org/zap"
)

// GossipHandler handles incoming gossip messages
type GossipHandler struct {
	ctx        *snow.Context
	blkBuilder builder.Builder
}

func NewGossipHandler(ctx *snow.Context, blkBuilder builder.Builder) *GossipHandler {
	return &GossipHandler{
		ctx:        ctx,
		blkBuilder: blkBuilder,
	}
}

func (n *GossipHandler) HandleTx(nodeID ids.NodeID, msg *message.TxGossip) error {
	n.ctx.Log.Debug("called HandleTx message handler",
		zap.Stringer("nodeID", nodeID),
	)

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
	n.ctx.Lock.Lock()
	defer n.ctx.Lock.Unlock()

	if _, dropped := n.blkBuilder.GetDropReason(txID); dropped {
		// If the tx is being dropped - just ignore it
		return nil
	}

	// add to mempool
	if err := n.blkBuilder.AddUnverifiedTx(tx); err != nil {
		n.ctx.Log.Debug("tx failed verification",
			zap.Stringer("nodeID", nodeID),
			zap.Error(err),
		)
	}
	return nil
}

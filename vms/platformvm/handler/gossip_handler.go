// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package handler

import (
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/vms/platformvm/blocks/builder"
	"github.com/ava-labs/avalanchego/vms/platformvm/message"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"go.uber.org/zap"
)

// gossipHandler handles incoming gossip messages
type gossipHandler struct {
	log        logging.Logger
	blkBuilder builder.Builder
}

func NewGossipHandler(log logging.Logger, blkBuilder builder.Builder) message.Handler {
	return &gossipHandler{
		log:        log,
		blkBuilder: blkBuilder,
	}
}

func (n *gossipHandler) HandleTx(nodeID ids.NodeID, msg *message.TxGossip) error {
	n.log.Debug("called HandleTx message handler",
		zap.Stringer("nodeID", nodeID),
	)

	tx, err := txs.Parse(txs.Codec, msg.Tx)
	if err != nil {
		n.log.Verbo("received invalid tx",
			zap.Stringer("nodeID", nodeID),
			zap.Binary("tx", msg.Tx),
			zap.Error(err),
		)
		return nil
	}

	if err := n.blkBuilder.AddUnverifiedTx(tx); err != nil {
		n.log.Debug("tx failed verification",
			zap.Stringer("nodeID", nodeID),
			zap.Error(err),
		)
	}
	return nil
}

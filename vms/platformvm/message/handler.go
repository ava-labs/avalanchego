// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package message

import (
	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/logging"
)

var _ GossipHandler = NoopGossipHandler{}

// GossipHandler handles incoming gossip messages
type GossipHandler interface {
	HandleTx(nodeID ids.NodeID, msg *TxGossip) error
}

type NoopGossipHandler struct {
	Log logging.Logger
}

func (h NoopGossipHandler) HandleTx(nodeID ids.NodeID, _ *TxGossip) error {
	h.Log.Debug("dropping unexpected Tx message",
		zap.Stringer("nodeID", nodeID),
	)
	return nil
}

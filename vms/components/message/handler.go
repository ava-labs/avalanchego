// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package message

import (
	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/logging"
)

var _ Handler = NoopHandler{}

// Handler handles incoming gossip messages
type Handler interface {
	HandleTxGossip(nodeID ids.NodeID, msg *TxGossip) error
}

type NoopHandler struct {
	Log logging.Logger
}

func (h NoopHandler) HandleTxGossip(nodeID ids.NodeID, _ *TxGossip) error {
	h.Log.Debug("dropping unexpected Tx message",
		zap.Stringer("nodeID", nodeID),
	)
	return nil
}

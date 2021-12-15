// (c) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package message

import (
	"github.com/ethereum/go-ethereum/log"

	"github.com/ava-labs/avalanchego/ids"
)

var _ Handler = NoopHandler{}

type Handler interface {
	HandleTxs(nodeID ids.ShortID, requestID uint32, msg *Txs) error
}

type NoopHandler struct{}

func (NoopHandler) HandleTxs(nodeID ids.ShortID, requestID uint32, _ *Txs) error {
	log.Debug("dropping unexpected EthTxs message", "peerID", nodeID, "requestID", requestID)
	return nil
}

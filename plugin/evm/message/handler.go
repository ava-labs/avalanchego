// (c) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package message

import (
	"github.com/ethereum/go-ethereum/log"

	"github.com/ava-labs/avalanchego/ids"
)

var _ Handler = NoopHandler{}

type Handler interface {
	HandleAtomicTx(nodeID ids.ShortID, requestID uint32, msg *AtomicTx) error
	HandleEthTxs(nodeID ids.ShortID, requestID uint32, msg *EthTxs) error
}

type NoopHandler struct{}

func (NoopHandler) HandleAtomicTx(nodeID ids.ShortID, requestID uint32, _ *AtomicTx) error {
	log.Debug("dropping unexpected AtomicTx message", "peerID", nodeID, "requestID", requestID)
	return nil
}

func (NoopHandler) HandleEthTxs(nodeID ids.ShortID, requestID uint32, _ *EthTxs) error {
	log.Debug("dropping unexpected EthTxs message", "peerID", nodeID, "requestID", requestID)
	return nil
}

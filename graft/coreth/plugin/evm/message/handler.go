// (c) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package message

import (
	"github.com/ethereum/go-ethereum/log"

	"github.com/ava-labs/avalanchego/ids"
)

var _ Handler = NoopHandler{}

type Handler interface {
	HandleAtomicTxNotify(nodeID ids.ShortID, requestID uint32, msg *AtomicTxNotify) error
	HandleAtomicTxRequest(nodeID ids.ShortID, requestID uint32, msg *AtomicTxRequest) error
	HandleAtomicTx(nodeID ids.ShortID, requestID uint32, msg *AtomicTx) error
	HandleEthTxsNotify(nodeID ids.ShortID, requestID uint32, msg *EthTxsNotify) error
	HandleEthTxsRequest(nodeID ids.ShortID, requestID uint32, msg *EthTxsRequest) error
	HandleEthTxs(nodeID ids.ShortID, requestID uint32, msg *EthTxs) error
}

type NoopHandler struct{}

func (NoopHandler) HandleAtomicTxNotify(nodeID ids.ShortID, requestID uint32, _ *AtomicTxNotify) error {
	log.Debug("dropping unexpected AtomicTxNotify message", "peerID", nodeID, "requestID", requestID)
	return nil
}

func (NoopHandler) HandleAtomicTxRequest(nodeID ids.ShortID, requestID uint32, _ *AtomicTxRequest) error {
	log.Debug("dropping unexpected AtomicTxRequest message", "peerID", nodeID, "requestID", requestID)
	return nil
}

func (NoopHandler) HandleAtomicTx(nodeID ids.ShortID, requestID uint32, _ *AtomicTx) error {
	log.Debug("dropping unexpected HandleAtomicTx message", "peerID", nodeID, "requestID", requestID)
	return nil
}

func (NoopHandler) HandleEthTxsNotify(nodeID ids.ShortID, requestID uint32, _ *EthTxsNotify) error {
	log.Debug("dropping unexpected EthTxsNotify message", "peerID", nodeID, "requestID", requestID)
	return nil
}

func (NoopHandler) HandleEthTxsRequest(nodeID ids.ShortID, requestID uint32, _ *EthTxsRequest) error {
	log.Debug("dropping unexpected EthTxsRequest message", "peerID", nodeID, "requestID", requestID)
	return nil
}

func (NoopHandler) HandleEthTxs(nodeID ids.ShortID, requestID uint32, _ *EthTxs) error {
	log.Debug("dropping unexpected EthTxs message", "peerID", nodeID, "requestID", requestID)
	return nil
}

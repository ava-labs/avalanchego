// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import (
	"fmt"
	"time"

	"github.com/ava-labs/avalanchego/ids"

	commonEng "github.com/ava-labs/avalanchego/snow/engine/common"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/rlp"

	"github.com/ava-labs/coreth/core/types"
	"github.com/ava-labs/coreth/plugin/evm/message"

	coreth "github.com/ava-labs/coreth/chain"
)

const (
	maxGossipEthDataSize int = 100 // TODO choose a sensible value
	maxGossipEthTxsSize  int = 10  // TODO choose a sensible value
)

type network struct {
	gossipActivationTime time.Time

	appSender commonEng.AppSender
	chain     *coreth.ETHChain
	mempool   *Mempool

	requestID uint32
	// TODO: prevent a different peer from responding and canceling a request of
	//       another peer
	// requestMaps allow checking that solicited content matched the requested one
	// They are populated upon sending an AppRequest and cleaned up upon AppResponse (also failed ones)
	requestsAtmContent map[uint32]ids.ID
	requestsEthContent map[uint32]map[common.Hash]struct{}

	requestHandler  message.Handler
	responseHandler message.Handler
	gossipHandler   message.Handler
}

func newNetwork(
	activationTime time.Time,
	appSender commonEng.AppSender,
	chain *coreth.ETHChain,
	mempool *Mempool,
	vm *VM,
) *network {
	net := &network{
		gossipActivationTime: activationTime,
		appSender:            appSender,
		chain:                chain,
		mempool:              mempool,
		requestsAtmContent:   make(map[uint32]ids.ID),
		requestsEthContent:   make(map[uint32]map[common.Hash]struct{}),
	}
	net.requestHandler = &RequestHandler{net: net}
	net.responseHandler = &ResponseHandler{
		vm:  vm,
		net: net,
	}
	net.gossipHandler = &GossipHandler{net: net}
	return net
}

func (n *network) AppRequestFailed(nodeID ids.ShortID, requestID uint32) error {
	log.Debug("AppRequestFailed called", "peerID", nodeID, "requestID", requestID)

	if time.Now().Before(n.gossipActivationTime) {
		log.Warn("AppRequestFailed called before activation time")
		return nil
	}

	delete(n.requestsAtmContent, requestID)
	delete(n.requestsEthContent, requestID)
	return nil
}

func (n *network) AppRequest(nodeID ids.ShortID, requestID uint32, msgBytes []byte) error {
	return n.handle(
		n.requestHandler,
		"Request",
		nodeID,
		requestID,
		msgBytes,
	)
}

func (n *network) AppResponse(nodeID ids.ShortID, requestID uint32, msgBytes []byte) error {
	return n.handle(
		n.responseHandler,
		"Response",
		nodeID,
		requestID,
		msgBytes,
	)
}

func (n *network) AppGossip(nodeID ids.ShortID, msgBytes []byte) error {
	return n.handle(
		n.gossipHandler,
		"Gossip",
		nodeID,
		0,
		msgBytes,
	)
}

func (n *network) handle(
	handler message.Handler,
	handlerName string,
	nodeID ids.ShortID,
	requestID uint32,
	msgBytes []byte,
) error {
	log.Debug(
		"App message handler called",
		"handler", handlerName,
		"peerID", nodeID,
		"len(msg)", len(msgBytes),
	)

	if time.Now().Before(n.gossipActivationTime) {
		log.Debug("App message called before activation time")
		return nil
	}

	msg, err := message.Parse(msgBytes)
	if err != nil {
		log.Debug("dropping App message due to failing to parse message")
		return nil
	}

	return msg.Handle(handler, nodeID, requestID)
}

type RequestHandler struct {
	message.NoopHandler

	net *network
}

func (h *RequestHandler) HandleAtomicTxNotify(nodeID ids.ShortID, requestID uint32, msg *message.AtomicTxNotify) error {
	log.Debug(
		"AppRequest called with AtomicTxNotify",
		"peerID", nodeID,
		"requestID", requestID,
		"txID", msg.TxID,
	)

	resTx, dropped, found := h.net.mempool.GetTx(msg.TxID)
	// TODO: why isn't this just `!found`?
	if !dropped && !found {
		// tx isn't in the mempool
		log.Trace(
			"AppRequest asked for tx that isn't in the mempool",
			"txID", msg.TxID,
		)
		return nil
	}

	reply := &message.AtomicTx{
		Tx: resTx.Bytes(),
	}
	replyBytes, err := message.Build(reply)
	if err != nil {
		log.Warn(
			"failed to build response AtomicTx message",
			"txID", msg.TxID,
			"err", err,
		)
		return nil
	}

	if err := h.net.appSender.SendAppResponse(nodeID, requestID, replyBytes); err != nil {
		return fmt.Errorf("failed to send AppResponse with: %s", err)
	}
	return nil
}

func (h *RequestHandler) HandleEthTxsNotify(nodeID ids.ShortID, requestID uint32, msg *message.EthTxsNotify) error {
	log.Debug(
		"AppRequest called with EthTxsNotify",
		"peerID", nodeID,
		"requestID", requestID,
		"len(txs)", len(msg.Txs),
	)

	pool := h.net.chain.GetTxPool()
	txs := make([]*types.Transaction, 0, len(msg.Txs))
	for _, txData := range msg.Txs {
		tx := pool.Get(txData.Hash)
		if tx != nil {
			txs = append(txs, tx)
		} else {
			log.Trace(
				"AppRequest asked for eth tx that isn't in the mempool",
				"hash", txData.Hash,
			)
		}
	}
	if len(txs) == 0 {
		return nil
	}

	txBytes, err := rlp.EncodeToBytes(txs)
	if err != nil {
		log.Warn(
			"failed to encode eth transactions",
			"len(txs)", len(txs),
			"err", err,
		)
		return nil
	}

	reply := &message.EthTxs{
		TxsBytes: txBytes,
	}
	replyBytes, err := message.Build(reply)
	if err != nil {
		log.Warn(
			"failed to build response EthTxs message",
			"len(txs)", len(txs),
			"err", err,
		)
		return nil
	}

	if err := h.net.appSender.SendAppResponse(nodeID, requestID, replyBytes); err != nil {
		return fmt.Errorf("failed to send AppResponse with: %s", err)
	}
	return nil
}

type ResponseHandler struct {
	message.NoopHandler

	vm  *VM
	net *network
}

func (h *ResponseHandler) HandleAtomicTx(nodeID ids.ShortID, requestID uint32, msg *message.AtomicTx) error {
	log.Debug(
		"AppResponse called with AtomicTx",
		"peerID", nodeID,
		"requestID", requestID,
		"len(tx)", len(msg.Tx),
	)

	// check that content matched with what previously requested
	expectedTxID, ok := h.net.requestsAtmContent[requestID]
	if !ok {
		log.Trace("AppResponse provided unrequested tx")
		return nil
	}
	delete(h.net.requestsAtmContent, requestID)

	tx := &Tx{}
	if _, err := Codec.Unmarshal(msg.Tx, tx); err != nil {
		log.Trace(
			"AppResponse provided invalid tx",
			"err", err,
		)
		return nil
	}
	unsignedBytes, err := Codec.Marshal(codecVersion, &tx.UnsignedAtomicTx)
	if err != nil {
		log.Warn(
			"AppResponse failed to marshal unsigned tx",
			"err", err,
		)
		return nil
	}
	tx.Initialize(unsignedBytes, msg.Tx)

	txID := tx.ID()
	if txID != expectedTxID {
		log.Trace(
			"AppResponse provided unrequested transaction ID",
			"expectedTxID", expectedTxID,
			"txID", txID,
		)
		return nil
	}

	if _, dropped, found := h.net.mempool.GetTx(txID); found || dropped {
		return nil
	}

	if err := h.vm.issueTx(tx, false /*=local*/); err != nil {
		log.Trace(
			"AppResponse provided invalid transaction",
			"peerID", nodeID,
			"requestID", requestID,
			"err", err,
		)
	}
	return nil
}

func (h *ResponseHandler) HandleEthTxs(nodeID ids.ShortID, requestID uint32, msg *message.EthTxs) error {
	log.Debug(
		"AppResponse called with EthTxs",
		"peerID", nodeID,
		"requestID", requestID,
		"len(txsBytes)", len(msg.TxsBytes),
	)

	// check that content matched with what previously requested
	expectedHashes, ok := h.net.requestsEthContent[requestID]
	if !ok {
		log.Trace("AppResponse provided unrequested txs")
		return nil
	}
	delete(h.net.requestsEthContent, requestID)

	txs := make([]*types.Transaction, 0)
	if err := rlp.DecodeBytes(msg.TxsBytes, &txs); err != nil {
		log.Trace(
			"AppResponse provided invalid txs",
			"err", err,
		)
		return nil
	}
	if len(txs) > maxGossipEthTxsSize {
		log.Trace(
			"AppResponse provided too many",
			"len(txs)", len(txs),
		)
		return nil
	}

	requestedTxs := make([]*types.Transaction, 0, len(expectedHashes))
	for _, tx := range txs {
		txHash := tx.Hash()
		if _, ok := expectedHashes[txHash]; !ok {
			log.Trace(
				"AppResponse received unexpected eth tx",
				"hash", txHash,
			)
			continue
		}
		requestedTxs = append(requestedTxs, tx)
	}
	if len(requestedTxs) == 0 {
		log.Trace("AppResponse received only unrequested txs")
		return nil
	}

	// submit to mempool expected transactions only
	errs := h.net.chain.GetTxPool().AddRemotes(requestedTxs)
	for _, err := range errs {
		if err != nil {
			log.Debug(
				"AppResponse failed to issue AddRemotes",
				"err", err,
			)
		}
	}
	return nil
}

type GossipHandler struct {
	message.NoopHandler

	net *network
}

func (h *GossipHandler) HandleAtomicTxNotify(nodeID ids.ShortID, _ uint32, msg *message.AtomicTxNotify) error {
	log.Debug(
		"AppGossip called with AtomicTxNotify",
		"peerID", nodeID,
		"txID", msg.TxID,
	)

	if _, _, found := h.net.mempool.GetTx(msg.TxID); found {
		// we already know the tx, no need to request it
		return nil
	}

	nodes := ids.ShortSet{nodeID: struct{}{}}
	h.net.requestID++
	if err := h.net.appSender.SendAppRequest(nodes, h.net.requestID, msg.Bytes()); err != nil {
		return fmt.Errorf("AppGossip: failed sending AppRequest with error %s", err)
	}

	// record content to check response later on
	h.net.requestsAtmContent[h.net.requestID] = msg.TxID
	return nil
}

func (h *GossipHandler) HandleEthTxsNotify(nodeID ids.ShortID, _ uint32, msg *message.EthTxsNotify) error {
	log.Debug(
		"AppGossip called with EthTxsNotify",
		"peerID", nodeID,
		"len(txs)", len(msg.Txs),
	)

	pool := h.net.chain.GetTxPool()
	dataToRequest := make([]message.EthTxNotify, 0)
	for _, txData := range msg.Txs {
		if pool.Has(txData.Hash) {
			continue
		}

		if err := pool.CheckNonceOrdering(txData.Sender, txData.Nonce); err != nil {
			log.Trace(
				"AppGossip eth tx's nonce is too old",
				"hash", txData.Hash,
			)
			continue
		}

		dataToRequest = append(dataToRequest, txData)
	}

	if len(dataToRequest) == 0 {
		return nil
	}

	reqMsg := message.EthTxsNotify{
		Txs: dataToRequest,
	}
	reqMsgBytes, err := message.Build(&reqMsg)
	if err != nil {
		log.Warn(
			"failed to build response EthTxsNotify message",
			"len(txs)", len(dataToRequest),
			"err", err,
		)
		return nil
	}

	nodes := ids.ShortSet{nodeID: struct{}{}}
	h.net.requestID++
	if err := h.net.appSender.SendAppRequest(nodes, h.net.requestID, reqMsgBytes); err != nil {
		return fmt.Errorf("AppGossip: failed sending AppRequest with error %s", err)
	}

	// record content to check response later on
	requestedTxIDs := make(map[common.Hash]struct{}, len(dataToRequest))
	for _, txData := range dataToRequest {
		requestedTxIDs[txData.Hash] = struct{}{}
	}
	h.net.requestsEthContent[h.net.requestID] = requestedTxIDs
	return nil
}

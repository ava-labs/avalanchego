// (c) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import (
	"fmt"
	"time"

	"github.com/ava-labs/avalanchego/cache"
	"github.com/ava-labs/avalanchego/ids"

	commonEng "github.com/ava-labs/avalanchego/snow/engine/common"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/rlp"

	"github.com/ava-labs/coreth/core"
	"github.com/ava-labs/coreth/core/types"
	"github.com/ava-labs/coreth/plugin/evm/message"

	coreth "github.com/ava-labs/coreth/chain"
)

const (
	recentCacheSize = 100
)

type network struct {
	gossipActivationTime time.Time

	appSender commonEng.AppSender
	chain     *coreth.ETHChain
	mempool   *Mempool
	signer    types.Signer

	requestID uint32
	// requestMaps allow checking that provided content matches the requested
	// content. They are populated upon sending an AppRequest and cleaned up
	// upon receipt of the corresponding AppResponse or AppRequestFailed.
	requestsAtmContent map[uint32]ids.ID
	requestsEthContent map[uint32]map[common.Hash]struct{}

	requestHandler  message.Handler
	responseHandler message.Handler
	gossipHandler   message.Handler

	recentAtomicTxs *cache.LRU
	recentEthTxs    *cache.LRU
}

func (vm *VM) NewNetwork(
	activationTime time.Time,
	appSender commonEng.AppSender,
	chain *coreth.ETHChain,
	mempool *Mempool,
	signer types.Signer,
) *network {
	net := &network{
		gossipActivationTime: activationTime,
		appSender:            appSender,
		chain:                chain,
		mempool:              mempool,
		signer:               signer,
		requestsAtmContent:   make(map[uint32]ids.ID),
		requestsEthContent:   make(map[uint32]map[common.Hash]struct{}),
		recentAtomicTxs:      &cache.LRU{Size: recentCacheSize},
		recentEthTxs:         &cache.LRU{Size: recentCacheSize},
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

func (n *network) GossipAtomicTx(tx *Tx) error {
	txID := tx.ID()
	if time.Now().Before(n.gossipActivationTime) {
		log.Debug(
			"not gossiping atomic tx before the gossiping activation time",
			"txID", txID,
		)
		return nil
	}

	if _, has := n.recentAtomicTxs.Get(txID); has {
		log.Debug(
			"not gossiping recently gossiped atomic tx",
			"txID", txID,
		)
		return nil
	}
	n.recentAtomicTxs.Put(txID, nil)

	msg := message.AtomicTxNotify{
		TxID: txID,
	}
	msgBytes, err := message.Build(&msg)
	if err != nil {
		return err
	}

	log.Debug(
		"gossiping atomic tx",
		"txID", txID,
	)
	return n.appSender.SendAppGossip(msgBytes)
}

func (n *network) GossipEthTxs(txs []*types.Transaction) error {
	if time.Now().Before(n.gossipActivationTime) {
		log.Debug(
			"not gossiping eth txs before the gossiping activation time",
			"len(txs)", len(txs),
		)
		return nil
	}

	pool := n.chain.GetTxPool()
	txsData := make([]message.EthTxNotify, 0)

	// Recover signatures before `core` package processing
	if cache := n.chain.SenderCacher(); cache != nil {
		cache.Recover(n.signer, txs)
	}

	for _, tx := range txs {
		txHash := tx.Hash()
		if _, has := n.recentEthTxs.Get(txHash); has {
			log.Debug(
				"not gossiping recently gossiped eth tx",
				"hash", txHash,
			)
			continue
		}
		n.recentEthTxs.Put(txHash, nil)

		txStatus := pool.Status([]common.Hash{txHash})[0]
		if txStatus != core.TxStatusPending {
			continue
		}

		sender, err := types.Sender(n.signer, tx)
		if err != nil {
			log.Debug(
				"couldn't retrieve sender for eth tx",
				"err", err,
			)
			continue
		}

		txsData = append(txsData, message.EthTxNotify{
			Hash:   txHash,
			Sender: sender,
			Nonce:  tx.Nonce(),
		})
	}

	for start, end := 0, message.MaxEthTxsLen; start < len(txsData); start, end = end, end+message.MaxEthTxsLen {
		if end > len(txsData) {
			end = len(txsData)
		}

		data := txsData[start:end]
		msg := message.EthTxsNotify{
			Txs: data,
		}
		msgBytes, err := message.Build(&msg)
		if err != nil {
			return err
		}

		log.Debug(
			"gossiping eth txs",
			"len(txs)", len(data),
		)
		if err := n.appSender.SendAppGossip(msgBytes); err != nil {
			return err
		}
	}
	return nil
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
		"requestID", requestID,
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
	if dropped || !found {
		// tx is either invalid or isn't in the mempool
		log.Trace(
			"AppRequest asked for tx that is either invalid or not in the mempool",
			"peerID", nodeID,
			"requestID", requestID,
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
			"peerID", nodeID,
			"requestID", requestID,
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
		txHashes := make([]common.Hash, len(txs))
		for i, tx := range txs {
			txHashes[i] = tx.Hash()
		}
		log.Warn(
			"failed to encode eth transactions",
			"hashes", txHashes,
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

	// check that the received transaction matches the requested transaction
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

	// check that the received transactions matches the requested transactions
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
			"peerID", nodeID,
			"requestID", requestID,
			"err", err,
		)
		return nil
	}
	if len(txs) > message.MaxEthTxsLen {
		log.Trace(
			"AppResponse provided too many txs",
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
	dataToRequest := make([]message.EthTxNotify, 0, len(msg.Txs))
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

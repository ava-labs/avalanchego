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
	net.gossipHandler = &GossipHandler{
		vm:  vm,
		net: net,
	}
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

	var msg message.AtomicTxNotify
	if _, has := n.recentAtomicTxs.Get(txID); !has {
		msg.Tx = tx.Bytes()
		n.recentAtomicTxs.Put(txID, nil)
	} else {
		msg.TxID = txID
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

func (n *network) sendEthTxsNotify(fullTxs []*types.Transaction, partialTxs []message.EthTxNotify) error {
	if len(fullTxs) == 0 || len(partialTxs) == 0 {
		return nil
	}

	var msg message.EthTxsNotify
	if len(fullTxs) > 0 {
		txBytes, err := rlp.EncodeToBytes(fullTxs)
		if err != nil {
			log.Warn(
				"failed to encode eth transactions",
				"len(fullTxs)", len(fullTxs),
				"err", err,
			)
			return nil
		}
		msg.TxsBytes = txBytes
	}
	if len(partialTxs) > 0 {
		msg.Txs = partialTxs
	}
	msgBytes, err := message.Build(&msg)
	if err != nil {
		return err
	}
	log.Debug(
		"gossiping eth txs",
		"len(txs)", len(msg.Txs),
		"len(txsBytes)", len(msg.TxsBytes),
		"len(fullTxs)", len(fullTxs),
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
	fullTxs := make([]*types.Transaction, 0)
	partialTxs := make([]message.EthTxNotify, 0)

	// TODO: import SenderCacher correctly instead of using a global var
	// Recover signatures before `core` package processing
	if cache := n.chain.SenderCacher(); cache != nil {
		cache.Recover(n.signer, txs)
	}

	for _, tx := range txs {
		txHash := tx.Hash()
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

		if _, has := n.recentEthTxs.Get(txHash); !has {
			fullTxs = append(fullTxs, tx)
			n.recentEthTxs.Put(txHash, nil)
		} else {
			partialTxs = append(partialTxs, message.EthTxNotify{
				Hash:   txHash,
				Sender: sender,
				Nonce:  tx.Nonce(),
			})
		}
	}

	// Attempt to gossip [fullTxs] first
	msgFullTxs := make([]*types.Transaction, 0)
	msgFullTxsSize := common.StorageSize(0)
	for _, tx := range fullTxs {
		size := tx.Size()
		if msgFullTxsSize+size > message.IdealEthMsgSize {
			if err := n.sendEthTxsNotify(msgFullTxs, nil); err != nil {
				return err
			}
			msgFullTxs = msgFullTxs[:0]
			msgFullTxsSize = 0
		}
		msgFullTxs = append(msgFullTxs, tx)
		msgFullTxsSize += size
	}

	// Next, gossip [partialTxs] (optionally including leftover [msgFullTxs])
	msgPartialTxs := make([]message.EthTxNotify, 0)
	for _, tx := range partialTxs {
		if len(msgPartialTxs) > message.MaxEthTxsLen {
			if err := n.sendEthTxsNotify(msgFullTxs, msgPartialTxs); err != nil {
				return err
			}
			msgFullTxs = nil
			msgPartialTxs = msgPartialTxs[:0]
		}
		msgPartialTxs = append(msgPartialTxs, tx)
	}

	// Send any remaining [msgFullTxs] and/or [msgPartialTxs]
	return n.sendEthTxsNotify(msgFullTxs, msgPartialTxs)
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

func (h *RequestHandler) HandleAtomicTxRequest(nodeID ids.ShortID, requestID uint32, msg *message.AtomicTxRequest) error {
	log.Debug(
		"AppRequest called with AtomicTxRequest",
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

func (h *RequestHandler) HandleEthTxsRequest(nodeID ids.ShortID, requestID uint32, msg *message.EthTxsRequest) error {
	log.Debug(
		"AppRequest called with EthTxsRequest",
		"peerID", nodeID,
		"requestID", requestID,
		"len(txs)", len(msg.Txs),
	)

	pool := h.net.chain.GetTxPool()
	txs := make([]*types.Transaction, 0, len(msg.Txs))
	txsSize := common.StorageSize(0)
	for _, txData := range msg.Txs {
		tx := pool.Get(txData.Hash)
		if tx != nil {
			size := tx.Size()
			if txsSize+size > message.IdealEthMsgSize {
				// If the size of the response is larger than [IdealEthMsgSize], then
				// we stop appending txs to the response.
				log.Trace(
					"AppRequest reached IdealEthMsgSize",
					"peerID", nodeID,
					"requestID", requestID,
					"len(txs)", len(msg.Txs),
				)
				break
			}
			txs = append(txs, tx)
			txsSize += size
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

	vm  *VM
	net *network
}

func (h *GossipHandler) HandleAtomicTxNotify(nodeID ids.ShortID, _ uint32, msg *message.AtomicTxNotify) error {
	log.Debug(
		"AppGossip called with AtomicTxNotify",
		"peerID", nodeID,
		"txID", msg.TxID,
	)

	// In the case that the gossip message contains a transaction,
	// attempt to parse it and add it as a remote.
	tx := Tx{}
	if _, err := Codec.Unmarshal(msg.Tx, &tx); err != nil {
		log.Trace(
			"AppGossip provided invalid tx",
			"err", err,
		)
		return nil
	}
	unsignedBytes, err := Codec.Marshal(codecVersion, &tx.UnsignedAtomicTx)
	if err != nil {
		log.Warn(
			"AppGossup failed to marshal unsigned tx",
			"err", err,
		)
		return nil
	}
	tx.Initialize(unsignedBytes, msg.Tx)

	txID := tx.ID()
	if _, dropped, found := h.net.mempool.GetTx(txID); found || dropped {
		return nil
	}

	if err := h.vm.issueTx(&tx, false /*=local*/); err != nil {
		log.Trace(
			"AppGossip provided invalid transaction",
			"peerID", nodeID,
			"err", err,
		)
	}

	// If only a transaction was provided, exit early without making
	// any request.
	if msg.TxID == (ids.ID{}) {
		return nil
	}

	if _, _, found := h.net.mempool.GetTx(msg.TxID); found {
		// we already know the tx, no need to request it
		return nil
	}

	reqMsg := message.AtomicTxRequest{
		TxID: msg.TxID,
	}

	reqMsgBytes, err := message.Build(&reqMsg)
	if err != nil {
		log.Warn(
			"failed to build response AtomicTxNotify message",
			"txID", msg.TxID,
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
	h.net.requestsAtmContent[h.net.requestID] = msg.TxID
	return nil
}

func (h *GossipHandler) HandleEthTxsNotify(nodeID ids.ShortID, _ uint32, msg *message.EthTxsNotify) error {
	log.Debug(
		"AppGossip called with EthTxsNotify",
		"peerID", nodeID,
		"len(txs)", len(msg.Txs),
		"len(txsBytes)", len(msg.TxsBytes),
	)

	// In the case that the gossip message contains transactions,
	// attempt to parse it and add it as a remote. The maximum size of this
	// encoded object is enforced by the codec.
	if len(msg.TxsBytes) > 0 {
		txs := make([]*types.Transaction, 0)
		if err := rlp.DecodeBytes(msg.TxsBytes, &txs); err != nil {
			log.Trace(
				"AppGossip provided invalid txs",
				"peerID", nodeID,
				"err", err,
			)
			return nil
		}
		errs := h.net.chain.GetTxPool().AddRemotes(txs)
		for _, err := range errs {
			if err != nil {
				log.Debug(
					"AppGossip failed to issue AddRemotes",
					"err", err,
				)
			}
		}
	}

	// Truncate transactions to request if recieve more than gossiped.
	if len(msg.Txs) > message.MaxEthTxsLen {
		log.Trace(
			"AppGossip provided > MaxEthTxsLen",
			"len(msg.Txs)", len(msg.Txs),
			"peerID", nodeID,
		)
		msg.Txs = msg.Txs[:message.MaxEthTxsLen]
	}

	// If message contains any transaction hashes, determine which hashes are
	// interesting and request them.
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

	reqMsg := message.EthTxsRequest{
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

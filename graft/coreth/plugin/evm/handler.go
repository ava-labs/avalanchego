// (c) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import (
	"github.com/ava-labs/avalanchego/ids"

	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/rlp"

	"github.com/ava-labs/coreth/core/txpool"
	"github.com/ava-labs/coreth/core/types"
	"github.com/ava-labs/coreth/plugin/evm/message"
)

// GossipHandler handles incoming gossip messages
type GossipHandler struct {
	vm            *VM
	atomicMempool *Mempool
	txPool        *txpool.TxPool
	stats         GossipStats
}

func NewGossipHandler(vm *VM, stats GossipStats) *GossipHandler {
	return &GossipHandler{
		vm:            vm,
		atomicMempool: vm.mempool,
		txPool:        vm.txPool,
		stats:         stats,
	}
}

func (h *GossipHandler) HandleAtomicTx(nodeID ids.NodeID, msg message.AtomicTxGossip) error {
	log.Trace(
		"AppGossip called with AtomicTxGossip",
		"peerID", nodeID,
	)

	if len(msg.Tx) == 0 {
		log.Trace(
			"AppGossip received empty AtomicTxGossip Message",
			"peerID", nodeID,
		)
		return nil
	}

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
		log.Trace(
			"AppGossip failed to marshal unsigned tx",
			"err", err,
		)
		return nil
	}
	tx.Initialize(unsignedBytes, msg.Tx)

	txID := tx.ID()
	h.stats.IncAtomicGossipReceived()
	if _, dropped, found := h.atomicMempool.GetTx(txID); found {
		h.stats.IncAtomicGossipReceivedKnown()
		return nil
	} else if dropped {
		h.stats.IncAtomicGossipReceivedDropped()
		return nil
	}

	h.stats.IncAtomicGossipReceivedNew()

	h.vm.ctx.Lock.RLock()
	defer h.vm.ctx.Lock.RUnlock()

	if err := h.vm.mempool.AddTx(&tx); err != nil {
		log.Trace(
			"AppGossip provided invalid transaction",
			"peerID", nodeID,
			"err", err,
		)
		h.stats.IncAtomicGossipReceivedError()
	}

	return nil
}

func (h *GossipHandler) HandleEthTxs(nodeID ids.NodeID, msg message.EthTxsGossip) error {
	log.Trace(
		"AppGossip called with EthTxsGossip",
		"peerID", nodeID,
		"size(txs)", len(msg.Txs),
	)

	if len(msg.Txs) == 0 {
		log.Trace(
			"AppGossip received empty EthTxsGossip Message",
			"peerID", nodeID,
		)
		return nil
	}

	// The maximum size of this encoded object is enforced by the codec.
	txs := make([]*types.Transaction, 0)
	if err := rlp.DecodeBytes(msg.Txs, &txs); err != nil {
		log.Trace(
			"AppGossip provided invalid txs",
			"peerID", nodeID,
			"err", err,
		)
		return nil
	}
	h.stats.IncEthTxsGossipReceived()
	errs := h.txPool.AddRemotes(txs)
	for i, err := range errs {
		if err != nil {
			log.Trace(
				"AppGossip failed to add to mempool",
				"err", err,
				"tx", txs[i].Hash(),
			)
			if err == txpool.ErrAlreadyKnown {
				h.stats.IncEthTxsGossipReceivedKnown()
			} else {
				h.stats.IncEthTxsGossipReceivedError()
			}
			continue
		}
		h.stats.IncEthTxsGossipReceivedNew()
	}
	return nil
}

// (c) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import (
	"container/heap"
	"context"
	"math/big"
	"sync"
	"time"

	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/network/p2p/gossip"

	"github.com/ava-labs/coreth/peer"

	"github.com/ava-labs/avalanchego/cache"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/utils/wrappers"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/rlp"

	"github.com/ava-labs/coreth/core"
	"github.com/ava-labs/coreth/core/state"
	"github.com/ava-labs/coreth/core/txpool"
	"github.com/ava-labs/coreth/core/types"
	"github.com/ava-labs/coreth/plugin/evm/message"
)

const (
	// We allow [recentCacheSize] to be fairly large because we only store hashes
	// in the cache, not entire transactions.
	recentCacheSize = 512

	// [ethTxsGossipInterval] is how often we attempt to gossip newly seen
	// transactions to other nodes.
	ethTxsGossipInterval = 500 * time.Millisecond

	// [minGossipBatchInterval] is the minimum amount of time that must pass
	// before our last gossip to peers.
	minGossipBatchInterval = 50 * time.Millisecond
)

// Gossiper handles outgoing gossip of transactions
type Gossiper interface {
	// GossipAtomicTxs sends AppGossip message containing the given [txs]
	GossipAtomicTxs(txs []*Tx) error
	// GossipEthTxs sends AppGossip message containing the given [txs]
	GossipEthTxs(txs []*types.Transaction) error
}

// pushGossiper is used to gossip transactions to the network
type pushGossiper struct {
	ctx    *snow.Context
	config Config

	client           peer.NetworkClient
	blockchain       *core.BlockChain
	txPool           *txpool.TxPool
	atomicMempool    *Mempool
	ethTxGossiper    gossip.Accumulator[*GossipEthTx]
	atomicTxGossiper gossip.Accumulator[*GossipAtomicTx]

	// We attempt to batch transactions we need to gossip to avoid runaway
	// amplification of mempol chatter.
	ethTxsToGossipChan chan []*types.Transaction
	ethTxsToGossip     map[common.Hash]*types.Transaction
	lastGossiped       time.Time
	shutdownChan       chan struct{}
	shutdownWg         *sync.WaitGroup

	// [recentAtomicTxs] and [recentEthTxs] prevent us from over-gossiping the
	// same transaction in a short period of time.
	recentAtomicTxs *cache.LRU[ids.ID, interface{}]
	recentEthTxs    *cache.LRU[common.Hash, interface{}]

	codec codec.Manager
	stats GossipSentStats
}

// createGossiper constructs and returns a pushGossiper or noopGossiper
// based on whether vm.chainConfig.ApricotPhase4BlockTimestamp is set
func (vm *VM) createGossiper(
	stats GossipStats,
	ethTxGossiper gossip.Accumulator[*GossipEthTx],
	atomicTxGossiper gossip.Accumulator[*GossipAtomicTx],
) Gossiper {
	net := &pushGossiper{
		ctx:                vm.ctx,
		config:             vm.config,
		client:             vm.client,
		blockchain:         vm.blockChain,
		txPool:             vm.txPool,
		atomicMempool:      vm.mempool,
		ethTxsToGossipChan: make(chan []*types.Transaction),
		ethTxsToGossip:     make(map[common.Hash]*types.Transaction),
		shutdownChan:       vm.shutdownChan,
		shutdownWg:         &vm.shutdownWg,
		recentAtomicTxs:    &cache.LRU[ids.ID, interface{}]{Size: recentCacheSize},
		recentEthTxs:       &cache.LRU[common.Hash, interface{}]{Size: recentCacheSize},
		codec:              vm.networkCodec,
		stats:              stats,
		ethTxGossiper:      ethTxGossiper,
		atomicTxGossiper:   atomicTxGossiper,
	}

	net.awaitEthTxGossip()
	return net
}

// queueExecutableTxs attempts to select up to [maxTxs] from the tx pool for
// regossiping.
//
// We assume that [txs] contains an array of nonce-ordered transactions for a given
// account. This array of transactions can have gaps and start at a nonce lower
// than the current state of an account.
func (n *pushGossiper) queueExecutableTxs(
	state *state.StateDB,
	baseFee *big.Int,
	txs map[common.Address]types.Transactions,
	maxTxs int,
) types.Transactions {
	// Setup heap for transactions
	heads := make(types.TxByPriceAndTime, 0, len(txs))
	for addr, accountTxs := range txs {
		// Short-circuit here to avoid performing an unnecessary state lookup
		if len(accountTxs) == 0 {
			continue
		}

		// Ensure any transactions regossiped are immediately executable
		var (
			currentNonce = state.GetNonce(addr)
			tx           *types.Transaction
		)
		for _, accountTx := range accountTxs {
			// The tx pool may be out of sync with current state, so we iterate
			// through the account transactions until we get to one that is
			// executable.
			if accountTx.Nonce() == currentNonce {
				tx = accountTx
				break
			}
			// There may be gaps in the tx pool and we could jump past the nonce we'd
			// like to execute.
			if accountTx.Nonce() > currentNonce {
				break
			}
		}
		if tx == nil {
			continue
		}

		// Don't try to regossip a transaction too frequently
		if time.Since(tx.FirstSeen()) < n.config.RegossipFrequency.Duration {
			continue
		}

		// Ensure the fee the transaction pays is valid at tip
		wrapped, err := types.NewTxWithMinerFee(tx, baseFee)
		if err != nil {
			log.Debug(
				"not queuing tx for regossip",
				"tx", tx.Hash(),
				"err", err,
			)
			continue
		}

		heads = append(heads, wrapped)
	}
	heap.Init(&heads)

	// Add up to [maxTxs] transactions to be gossiped
	queued := make([]*types.Transaction, 0, maxTxs)
	for len(heads) > 0 && len(queued) < maxTxs {
		tx := heads[0].Tx
		queued = append(queued, tx)
		heap.Pop(&heads)
	}

	return queued
}

// queueRegossipTxs finds the best transactions in the mempool and adds up to
// [TxRegossipMaxSize] of them to [ethTxsToGossip].
func (n *pushGossiper) queueRegossipTxs() types.Transactions {
	// Fetch all pending transactions
	pending := n.txPool.Pending(true)

	// Split the pending transactions into locals and remotes
	localTxs := make(map[common.Address]types.Transactions)
	remoteTxs := pending
	for _, account := range n.txPool.Locals() {
		if txs := remoteTxs[account]; len(txs) > 0 {
			delete(remoteTxs, account)
			localTxs[account] = txs
		}
	}

	// Add best transactions to be gossiped (preferring local txs)
	tip := n.blockchain.CurrentBlock()
	state, err := n.blockchain.StateAt(tip.Root)
	if err != nil || state == nil {
		log.Debug(
			"could not get state at tip",
			"tip", tip.Hash(),
			"err", err,
		)
		return nil
	}
	localQueued := n.queueExecutableTxs(state, tip.BaseFee, localTxs, n.config.RegossipMaxTxs)
	localCount := len(localQueued)
	n.stats.IncEthTxsRegossipQueuedLocal(localCount)
	if localCount >= n.config.RegossipMaxTxs {
		n.stats.IncEthTxsRegossipQueued()
		return localQueued
	}
	remoteQueued := n.queueExecutableTxs(state, tip.BaseFee, remoteTxs, n.config.RegossipMaxTxs-localCount)
	n.stats.IncEthTxsRegossipQueuedRemote(len(remoteQueued))
	if localCount+len(remoteQueued) > 0 {
		// only increment the regossip stat when there are any txs queued
		n.stats.IncEthTxsRegossipQueued()
	}
	return append(localQueued, remoteQueued...)
}

// awaitEthTxGossip periodically gossips transactions that have been queued for
// gossip at least once every [ethTxsGossipInterval].
func (n *pushGossiper) awaitEthTxGossip() {
	n.shutdownWg.Add(1)
	go n.ctx.Log.RecoverAndPanic(func() {
		var (
			gossipTicker   = time.NewTicker(ethTxsGossipInterval)
			regossipTicker = time.NewTicker(n.config.RegossipFrequency.Duration)
		)
		defer func() {
			gossipTicker.Stop()
			regossipTicker.Stop()
			n.shutdownWg.Done()
		}()

		for {
			select {
			case <-gossipTicker.C:
				if attempted, err := n.gossipEthTxs(false); err != nil {
					log.Warn(
						"failed to send eth transactions",
						"len(txs)", attempted,
						"err", err,
					)
				}
				if err := n.ethTxGossiper.Gossip(context.TODO()); err != nil {
					log.Warn(
						"failed to send eth transactions",
						"err", err,
					)
				}
			case <-regossipTicker.C:
				for _, tx := range n.queueRegossipTxs() {
					n.ethTxsToGossip[tx.Hash()] = tx
				}
				if attempted, err := n.gossipEthTxs(true); err != nil {
					log.Warn(
						"failed to send eth transactions",
						"len(txs)", attempted,
						"err", err,
					)
				}
			case txs := <-n.ethTxsToGossipChan:
				for _, tx := range txs {
					n.ethTxsToGossip[tx.Hash()] = tx
				}
				if attempted, err := n.gossipEthTxs(false); err != nil {
					log.Warn(
						"failed to send eth transactions",
						"len(txs)", attempted,
						"err", err,
					)
				}

				gossipTxs := make([]*GossipEthTx, 0, len(txs))
				for _, tx := range txs {
					gossipTxs = append(gossipTxs, &GossipEthTx{Tx: tx})
				}

				n.ethTxGossiper.Add(gossipTxs...)
				if err := n.ethTxGossiper.Gossip(context.TODO()); err != nil {
					log.Warn(
						"failed to send eth transactions",
						"len(txs)", len(txs),
						"err", err,
					)
				}

			case <-n.shutdownChan:
				return
			}
		}
	})
}

func (n *pushGossiper) GossipAtomicTxs(txs []*Tx) error {
	errs := wrappers.Errs{}
	for _, tx := range txs {
		errs.Add(n.gossipAtomicTx(tx))
	}
	return errs.Err
}

func (n *pushGossiper) gossipAtomicTx(tx *Tx) error {
	txID := tx.ID()
	// Don't gossip transaction if it has been recently gossiped.
	if _, has := n.recentAtomicTxs.Get(txID); has {
		return nil
	}
	// If the transaction is not pending according to the mempool
	// then there is no need to gossip it further.
	if _, pending := n.atomicMempool.GetPendingTx(txID); !pending {
		return nil
	}
	n.recentAtomicTxs.Put(txID, nil)

	msg := message.AtomicTxGossip{
		Tx: tx.SignedBytes(),
	}
	msgBytes, err := message.BuildGossipMessage(n.codec, msg)
	if err != nil {
		return err
	}

	log.Trace(
		"gossiping atomic tx",
		"txID", txID,
	)
	n.stats.IncAtomicGossipSent()
	n.atomicTxGossiper.Add(&GossipAtomicTx{Tx: tx})
	if err := n.atomicTxGossiper.Gossip(context.TODO()); err != nil {
		return err
	}

	return n.client.Gossip(msgBytes)
}

func (n *pushGossiper) sendEthTxs(txs []*types.Transaction) error {
	if len(txs) == 0 {
		return nil
	}

	txBytes, err := rlp.EncodeToBytes(txs)
	if err != nil {
		return err
	}
	msg := message.EthTxsGossip{
		Txs: txBytes,
	}
	msgBytes, err := message.BuildGossipMessage(n.codec, msg)
	if err != nil {
		return err
	}

	log.Trace(
		"gossiping eth txs",
		"len(txs)", len(txs),
		"size(txs)", len(msg.Txs),
	)
	n.stats.IncEthTxsGossipSent()
	return n.client.Gossip(msgBytes)
}

func (n *pushGossiper) gossipEthTxs(force bool) (int, error) {
	if (!force && time.Since(n.lastGossiped) < minGossipBatchInterval) || len(n.ethTxsToGossip) == 0 {
		return 0, nil
	}
	n.lastGossiped = time.Now()
	txs := make([]*types.Transaction, 0, len(n.ethTxsToGossip))
	for _, tx := range n.ethTxsToGossip {
		txs = append(txs, tx)
		delete(n.ethTxsToGossip, tx.Hash())
	}

	selectedTxs := make([]*types.Transaction, 0)
	for _, tx := range txs {
		txHash := tx.Hash()
		txStatus := n.txPool.Status([]common.Hash{txHash})[0]
		if txStatus != txpool.TxStatusPending {
			continue
		}

		if n.config.RemoteGossipOnlyEnabled && n.txPool.HasLocal(txHash) {
			continue
		}

		// We check [force] outside of the if statement to avoid an unnecessary
		// cache lookup.
		if !force {
			if _, has := n.recentEthTxs.Get(txHash); has {
				continue
			}
		}
		n.recentEthTxs.Put(txHash, nil)

		selectedTxs = append(selectedTxs, tx)
	}

	if len(selectedTxs) == 0 {
		return 0, nil
	}

	// Attempt to gossip [selectedTxs]
	msgTxs := make([]*types.Transaction, 0)
	msgTxsSize := uint64(0)
	for _, tx := range selectedTxs {
		size := tx.Size()
		if msgTxsSize+size > message.EthMsgSoftCapSize {
			if err := n.sendEthTxs(msgTxs); err != nil {
				return len(selectedTxs), err
			}
			msgTxs = msgTxs[:0]
			msgTxsSize = 0
		}
		msgTxs = append(msgTxs, tx)
		msgTxsSize += size
	}

	// Send any remaining [msgTxs]
	return len(selectedTxs), n.sendEthTxs(msgTxs)
}

// GossipEthTxs enqueues the provided [txs] for gossiping. At some point, the
// [pushGossiper] will attempt to gossip the provided txs to other nodes
// (usually right away if not under load).
//
// NOTE: We never return a non-nil error from this function but retain the
// option to do so in case it becomes useful.
func (n *pushGossiper) GossipEthTxs(txs []*types.Transaction) error {
	select {
	case n.ethTxsToGossipChan <- txs:
	case <-n.shutdownChan:
	}
	return nil
}

// GossipHandler handles incoming gossip messages
type GossipHandler struct {
	vm            *VM
	atomicMempool *Mempool
	txPool        *txpool.TxPool
	stats         GossipReceivedStats
}

func NewGossipHandler(vm *VM, stats GossipReceivedStats) *GossipHandler {
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

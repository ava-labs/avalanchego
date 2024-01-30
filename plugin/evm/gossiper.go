// (c) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import (
	"context"
	"math/big"
	"sync"
	"time"

	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/network/p2p/gossip"

	"github.com/ava-labs/subnet-evm/peer"

	"github.com/ava-labs/avalanchego/cache"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/rlp"

	"github.com/ava-labs/subnet-evm/core"
	"github.com/ava-labs/subnet-evm/core/state"
	"github.com/ava-labs/subnet-evm/core/txpool"
	"github.com/ava-labs/subnet-evm/core/types"
	"github.com/ava-labs/subnet-evm/plugin/evm/message"
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
	// GossipEthTxs sends AppGossip message containing the given [txs]
	GossipEthTxs(txs []*types.Transaction) error
}

// pushGossiper is used to gossip transactions to the network
type pushGossiper struct {
	ctx    *snow.Context
	config Config

	client        peer.NetworkClient
	blockchain    *core.BlockChain
	txPool        *txpool.TxPool
	ethTxGossiper gossip.Accumulator[*GossipEthTx]

	// We attempt to batch transactions we need to gossip to avoid runaway
	// amplification of mempol chatter.
	ethTxsToGossipChan chan []*types.Transaction
	ethTxsToGossip     map[common.Hash]*types.Transaction
	lastGossiped       time.Time
	shutdownChan       chan struct{}
	shutdownWg         *sync.WaitGroup

	// [recentEthTxs] prevent us from over-gossiping the
	// same transaction in a short period of time.
	recentEthTxs *cache.LRU[common.Hash, interface{}]

	codec  codec.Manager
	signer types.Signer
	stats  GossipSentStats
}

// createGossiper constructs and returns a pushGossiper or noopGossiper
// based on whether vm.chainConfig.SubnetEVMTimestamp is set
func (vm *VM) createGossiper(
	stats GossipStats,
	ethTxGossiper gossip.Accumulator[*GossipEthTx],
) Gossiper {
	net := &pushGossiper{
		ctx:                vm.ctx,
		config:             vm.config,
		client:             vm.client,
		blockchain:         vm.blockChain,
		txPool:             vm.txPool,
		ethTxsToGossipChan: make(chan []*types.Transaction),
		ethTxsToGossip:     make(map[common.Hash]*types.Transaction),
		shutdownChan:       vm.shutdownChan,
		shutdownWg:         &vm.shutdownWg,
		recentEthTxs:       &cache.LRU[common.Hash, interface{}]{Size: recentCacheSize},
		codec:              vm.networkCodec,
		signer:             types.LatestSigner(vm.blockChain.Config()),
		stats:              stats,
		ethTxGossiper:      ethTxGossiper,
	}

	net.awaitEthTxGossip()
	return net
}

// addrStatus used to track the metadata of addresses being queued for
// regossip.
type addrStatus struct {
	nonce    uint64
	txsAdded int
}

// queueExecutableTxs attempts to select up to [maxTxs] from the tx pool for
// regossiping (with at most [maxAcctTxs] per account).
//
// We assume that [txs] contains an array of nonce-ordered transactions for a given
// account. This array of transactions can have gaps and start at a nonce lower
// than the current state of an account.
func (n *pushGossiper) queueExecutableTxs(
	state *state.StateDB,
	baseFee *big.Int,
	txs map[common.Address]types.Transactions,
	regossipFrequency Duration,
	maxTxs int,
	maxAcctTxs int,
) types.Transactions {
	var (
		stxs     = types.NewTransactionsByPriceAndNonce(n.signer, txs, baseFee)
		statuses = make(map[common.Address]*addrStatus)
		queued   = make([]*types.Transaction, 0, maxTxs)
	)

	// Iterate over possible transactions until there are none left or we have
	// hit the regossip target.
	for len(queued) < maxTxs {
		next := stxs.Peek()
		if next == nil {
			break
		}

		sender, _ := types.Sender(n.signer, next)
		status, ok := statuses[sender]
		if !ok {
			status = &addrStatus{
				nonce: state.GetNonce(sender),
			}
			statuses[sender] = status
		}

		// The tx pool may be out of sync with current state, so we iterate
		// through the account transactions until we get to one that is
		// executable.
		switch {
		case next.Nonce() < status.nonce:
			stxs.Shift()
			continue
		case next.Nonce() > status.nonce, time.Since(next.FirstSeen()) < regossipFrequency.Duration,
			status.txsAdded >= maxAcctTxs:
			stxs.Pop()
			continue
		}
		queued = append(queued, next)
		status.nonce++
		status.txsAdded++
		stxs.Shift()
	}

	return queued
}

// queueRegossipTxs finds the best non-priority transactions in the mempool and adds up to
// [RegossipMaxTxs] of them to [txsToGossip].
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
	rgFrequency := n.config.RegossipFrequency
	rgMaxTxs := n.config.RegossipMaxTxs
	rgTxsPerAddr := n.config.RegossipTxsPerAddress
	localQueued := n.queueExecutableTxs(state, tip.BaseFee, localTxs, rgFrequency, rgMaxTxs, rgTxsPerAddr)
	localCount := len(localQueued)
	n.stats.IncEthTxsRegossipQueuedLocal(localCount)
	if localCount >= rgMaxTxs {
		n.stats.IncEthTxsRegossipQueued()
		return localQueued
	}
	remoteQueued := n.queueExecutableTxs(state, tip.BaseFee, remoteTxs, rgFrequency, rgMaxTxs-localCount, rgTxsPerAddr)
	n.stats.IncEthTxsRegossipQueuedRemote(len(remoteQueued))
	if localCount+len(remoteQueued) > 0 {
		// only increment the regossip stat when there are any txs queued
		n.stats.IncEthTxsRegossipQueued()
	}
	return append(localQueued, remoteQueued...)
}

// queuePriorityRegossipTxs finds the best priority transactions in the mempool and adds up to
// [PriorityRegossipMaxTxs] of them to [txsToGossip].
func (n *pushGossiper) queuePriorityRegossipTxs() types.Transactions {
	// Fetch all pending transactions from the priority addresses
	priorityTxs := n.txPool.PendingFrom(n.config.PriorityRegossipAddresses, true)

	// Add best transactions to be gossiped
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
	return n.queueExecutableTxs(
		state, tip.BaseFee, priorityTxs,
		n.config.PriorityRegossipFrequency,
		n.config.PriorityRegossipMaxTxs,
		n.config.PriorityRegossipTxsPerAddress,
	)
}

// awaitEthTxGossip periodically gossips transactions that have been queued for
// gossip at least once every [ethTxsGossipInterval].
func (n *pushGossiper) awaitEthTxGossip() {
	n.shutdownWg.Add(1)
	go n.ctx.Log.RecoverAndPanic(func() {
		var (
			gossipTicker           = time.NewTicker(ethTxsGossipInterval)
			regossipTicker         = time.NewTicker(n.config.RegossipFrequency.Duration)
			priorityRegossipTicker = time.NewTicker(n.config.PriorityRegossipFrequency.Duration)
		)
		defer func() {
			gossipTicker.Stop()
			regossipTicker.Stop()
			priorityRegossipTicker.Stop()
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
						"failed to regossip eth transactions",
						"len(txs)", attempted,
						"err", err,
					)
				}
			case <-priorityRegossipTicker.C:
				for _, tx := range n.queuePriorityRegossipTxs() {
					n.ethTxsToGossip[tx.Hash()] = tx
				}
				if attempted, err := n.gossipEthTxs(true); err != nil {
					log.Warn(
						"failed to regossip priority eth transactions",
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
	vm     *VM
	txPool *txpool.TxPool
	stats  GossipReceivedStats
}

func NewGossipHandler(vm *VM, stats GossipReceivedStats) *GossipHandler {
	return &GossipHandler{
		vm:     vm,
		txPool: vm.txPool,
		stats:  stats,
	}
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

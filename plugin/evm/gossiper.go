// (c) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import (
	"math/big"
	"sync"
	"time"

	"github.com/ava-labs/avalanchego/codec"

	"github.com/ava-labs/subnet-evm/peer"

	"github.com/ava-labs/avalanchego/cache"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/rlp"

	"github.com/ava-labs/subnet-evm/core"
	"github.com/ava-labs/subnet-evm/core/state"
	"github.com/ava-labs/subnet-evm/core/types"
	"github.com/ava-labs/subnet-evm/plugin/evm/message"
)

const (
	// We allow [recentCacheSize] to be fairly large because we only store hashes
	// in the cache, not entire transactions.
	recentCacheSize = 512

	// [txsGossipInterval] is how often we attempt to gossip newly seen
	// transactions to other nodes.
	txsGossipInterval = 500 * time.Millisecond
)

// Gossiper handles outgoing gossip of transactions
type Gossiper interface {
	// GossipTxs sends AppGossip message containing the given [txs]
	GossipTxs(txs []*types.Transaction) error
}

// pushGossiper is used to gossip transactions to the network
type pushGossiper struct {
	ctx                  *snow.Context
	gossipActivationTime time.Time
	config               Config

	client     peer.Client
	blockchain *core.BlockChain
	txPool     *core.TxPool

	// We attempt to batch transactions we need to gossip to avoid runaway
	// amplification of mempol chatter.
	txsToGossipChan chan []*types.Transaction
	txsToGossip     map[common.Hash]*types.Transaction
	lastGossiped    time.Time
	shutdownChan    chan struct{}
	shutdownWg      *sync.WaitGroup

	// [recentTxs] prevent us from over-gossiping the
	// same transaction in a short period of time.
	recentTxs *cache.LRU

	codec  codec.Manager
	signer types.Signer
}

// newPushGossiper constructs and returns a pushGossiper
// assumes vm.chainConfig.SubnetEVMTimestamp is set
func (vm *VM) newPushGossiper() Gossiper {
	net := &pushGossiper{
		ctx:                  vm.ctx,
		gossipActivationTime: time.Unix(vm.chainConfig.SubnetEVMTimestamp.Int64(), 0),
		config:               vm.config,
		client:               vm.client,
		blockchain:           vm.chain.BlockChain(),
		txPool:               vm.chain.GetTxPool(),
		txsToGossipChan:      make(chan []*types.Transaction),
		txsToGossip:          make(map[common.Hash]*types.Transaction),
		shutdownChan:         vm.shutdownChan,
		shutdownWg:           &vm.shutdownWg,
		recentTxs:            &cache.LRU{Size: recentCacheSize},
		codec:                vm.networkCodec,
		signer:               types.LatestSigner(vm.chain.BlockChain().Config()),
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
	state, err := n.blockchain.StateAt(tip.Root())
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
	localQueued := n.queueExecutableTxs(state, tip.BaseFee(), localTxs, rgFrequency, rgMaxTxs, rgTxsPerAddr)
	localCount := len(localQueued)
	if localCount >= rgMaxTxs {
		return localQueued
	}
	remoteQueued := n.queueExecutableTxs(state, tip.BaseFee(), remoteTxs, rgFrequency, rgMaxTxs-localCount, rgTxsPerAddr)
	return append(localQueued, remoteQueued...)
}

// queueRegossipTxs finds the best priority transactions in the mempool and adds up to
// [PriorityRegossipMaxTxs] of them to [txsToGossip].
func (n *pushGossiper) queuePriorityRegossipTxs() types.Transactions {
	// Fetch all pending transactions from the priority addresses
	priorityTxs := n.txPool.PendingFrom(n.config.PriorityRegossipAddresses, true)

	// Add best transactions to be gossiped
	tip := n.blockchain.CurrentBlock()
	state, err := n.blockchain.StateAt(tip.Root())
	if err != nil || state == nil {
		log.Debug(
			"could not get state at tip",
			"tip", tip.Hash(),
			"err", err,
		)
		return nil
	}
	return n.queueExecutableTxs(
		state, tip.BaseFee(), priorityTxs,
		n.config.PriorityRegossipFrequency,
		n.config.PriorityRegossipMaxTxs,
		n.config.PriorityRegossipTxsPerAddress,
	)
}

// awaitEthTxGossip periodically gossips transactions that have been queued for
// gossip at least once every [txsGossipInterval].
func (n *pushGossiper) awaitEthTxGossip() {
	n.shutdownWg.Add(1)
	go n.ctx.Log.RecoverAndPanic(func() {
		defer n.shutdownWg.Done()

		var (
			gossipTicker           = time.NewTicker(txsGossipInterval)
			regossipTicker         = time.NewTicker(n.config.RegossipFrequency.Duration)
			priorityRegossipTicker = time.NewTicker(n.config.PriorityRegossipFrequency.Duration)
		)

		for {
			select {
			case <-gossipTicker.C:
				if attempted, err := n.gossipTxs(false); err != nil {
					log.Warn(
						"failed to send eth transactions",
						"len(txs)", attempted,
						"err", err,
					)
				}
			case <-regossipTicker.C:
				for _, tx := range n.queueRegossipTxs() {
					n.txsToGossip[tx.Hash()] = tx
				}
				if attempted, err := n.gossipTxs(true); err != nil {
					log.Warn(
						"failed to regossip eth transactions",
						"len(txs)", attempted,
						"err", err,
					)
				}
			case <-priorityRegossipTicker.C:
				for _, tx := range n.queuePriorityRegossipTxs() {
					n.txsToGossip[tx.Hash()] = tx
				}
				if attempted, err := n.gossipTxs(true); err != nil {
					log.Warn(
						"failed to regossip priority eth transactions",
						"len(txs)", attempted,
						"err", err,
					)
				}
			case txs := <-n.txsToGossipChan:
				for _, tx := range txs {
					n.txsToGossip[tx.Hash()] = tx
				}
				if attempted, err := n.gossipTxs(false); err != nil {
					log.Warn(
						"failed to send eth transactions",
						"len(txs)", attempted,
						"err", err,
					)
				}
			case <-n.shutdownChan:
				return
			}
		}
	})
}

func (n *pushGossiper) sendTxs(txs []*types.Transaction) error {
	if len(txs) == 0 {
		return nil
	}

	txBytes, err := rlp.EncodeToBytes(txs)
	if err != nil {
		return err
	}
	msg := message.TxsGossip{
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
	return n.client.Gossip(msgBytes)
}

func (n *pushGossiper) gossipTxs(force bool) (int, error) {
	if (!force && time.Since(n.lastGossiped) < txsGossipInterval) || len(n.txsToGossip) == 0 {
		return 0, nil
	}
	n.lastGossiped = time.Now()
	txs := make([]*types.Transaction, 0, len(n.txsToGossip))
	for _, tx := range n.txsToGossip {
		txs = append(txs, tx)
		delete(n.txsToGossip, tx.Hash())
	}

	selectedTxs := make([]*types.Transaction, 0)
	for _, tx := range txs {
		txHash := tx.Hash()
		txStatus := n.txPool.Status([]common.Hash{txHash})[0]
		if txStatus != core.TxStatusPending {
			continue
		}

		if n.config.RemoteGossipOnlyEnabled && n.txPool.HasLocal(txHash) {
			continue
		}

		// We check [force] outside of the if statement to avoid an unnecessary
		// cache lookup.
		if !force {
			if _, has := n.recentTxs.Get(txHash); has {
				continue
			}
		}
		n.recentTxs.Put(txHash, nil)

		selectedTxs = append(selectedTxs, tx)
	}

	if len(selectedTxs) == 0 {
		return 0, nil
	}

	// Attempt to gossip [selectedTxs]
	msgTxs := make([]*types.Transaction, 0)
	msgTxsSize := common.StorageSize(0)
	for _, tx := range selectedTxs {
		size := tx.Size()
		if msgTxsSize+size > message.TxMsgSoftCapSize {
			if err := n.sendTxs(msgTxs); err != nil {
				return len(selectedTxs), err
			}
			msgTxs = msgTxs[:0]
			msgTxsSize = 0
		}
		msgTxs = append(msgTxs, tx)
		msgTxsSize += size
	}

	// Send any remaining [msgTxs]
	return len(selectedTxs), n.sendTxs(msgTxs)
}

// GossipTxs enqueues the provided [txs] for gossiping. At some point, the
// [pushGossiper] will attempt to gossip the provided txs to other nodes
// (usually right away if not under load).
//
// NOTE: We never return a non-nil error from this function but retain the
// option to do so in case it becomes useful.
func (n *pushGossiper) GossipTxs(txs []*types.Transaction) error {
	if time.Now().Before(n.gossipActivationTime) {
		log.Trace(
			"not gossiping eth txs before the gossiping activation time",
			"len(txs)", len(txs),
		)
		return nil
	}

	select {
	case n.txsToGossipChan <- txs:
	case <-n.shutdownChan:
	}
	return nil
}

// GossipHandler handles incoming gossip messages
type GossipHandler struct {
	vm     *VM
	txPool *core.TxPool
}

func NewGossipHandler(vm *VM) *GossipHandler {
	return &GossipHandler{
		vm:     vm,
		txPool: vm.chain.GetTxPool(),
	}
}

func (h *GossipHandler) HandleTxs(nodeID ids.NodeID, msg message.TxsGossip) error {
	log.Trace(
		"AppGossip called with TxsGossip",
		"peerID", nodeID,
		"size(txs)", len(msg.Txs),
	)

	if len(msg.Txs) == 0 {
		log.Trace(
			"AppGossip received empty TxsGossip Message",
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
	errs := h.txPool.AddRemotes(txs)
	for i, err := range errs {
		if err != nil {
			log.Trace(
				"AppGossip failed to add to mempool",
				"err", err,
				"tx", txs[i].Hash(),
			)
		}
	}
	return nil
}

// noopGossiper should be used when gossip communication is not supported
type noopGossiper struct{}

func (n *noopGossiper) GossipTxs([]*types.Transaction) error {
	return nil
}

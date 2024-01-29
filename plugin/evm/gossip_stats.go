// (c) 2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import "github.com/ava-labs/coreth/metrics"

var _ GossipStats = &gossipStats{}

// GossipStats contains methods for updating incoming and outgoing gossip stats.
type GossipStats interface {
	GossipReceivedStats
	GossipSentStats
}

// GossipReceivedStats groups functions for incoming gossip stats.
type GossipReceivedStats interface {
	IncAtomicGossipReceived()
	IncEthTxsGossipReceived()

	// new vs. known txs received
	IncAtomicGossipReceivedDropped()
	IncAtomicGossipReceivedError()
	IncAtomicGossipReceivedKnown()
	IncAtomicGossipReceivedNew()
	IncEthTxsGossipReceivedError()
	IncEthTxsGossipReceivedKnown()
	IncEthTxsGossipReceivedNew()
}

// GossipSentStats groups functions for outgoing gossip stats.
type GossipSentStats interface {
	IncAtomicGossipSent()
	IncEthTxsGossipSent()

	// regossip
	IncEthTxsRegossipQueued()
	IncEthTxsRegossipQueuedLocal(count int)
	IncEthTxsRegossipQueuedRemote(count int)
}

// gossipStats implements stats for incoming and outgoing gossip stats.
type gossipStats struct {
	// messages
	atomicGossipSent     metrics.Counter
	atomicGossipReceived metrics.Counter
	ethTxsGossipSent     metrics.Counter
	ethTxsGossipReceived metrics.Counter

	// regossip
	ethTxsRegossipQueued       metrics.Counter
	ethTxsRegossipQueuedLocal  metrics.Counter
	ethTxsRegossipQueuedRemote metrics.Counter

	// new vs. known txs received
	atomicGossipReceivedDropped metrics.Counter
	atomicGossipReceivedError   metrics.Counter
	atomicGossipReceivedKnown   metrics.Counter
	atomicGossipReceivedNew     metrics.Counter
	ethTxsGossipReceivedError   metrics.Counter
	ethTxsGossipReceivedKnown   metrics.Counter
	ethTxsGossipReceivedNew     metrics.Counter
}

func NewGossipStats() GossipStats {
	return &gossipStats{
		atomicGossipSent:     metrics.GetOrRegisterCounter("gossip_atomic_sent", nil),
		atomicGossipReceived: metrics.GetOrRegisterCounter("gossip_atomic_received", nil),
		ethTxsGossipSent:     metrics.GetOrRegisterCounter("gossip_eth_txs_sent", nil),
		ethTxsGossipReceived: metrics.GetOrRegisterCounter("gossip_eth_txs_received", nil),

		ethTxsRegossipQueued:       metrics.GetOrRegisterCounter("regossip_eth_txs_queued_attempts", nil),
		ethTxsRegossipQueuedLocal:  metrics.GetOrRegisterCounter("regossip_eth_txs_queued_local_tx_count", nil),
		ethTxsRegossipQueuedRemote: metrics.GetOrRegisterCounter("regossip_eth_txs_queued_remote_tx_count", nil),

		atomicGossipReceivedDropped: metrics.GetOrRegisterCounter("gossip_atomic_received_dropped", nil),
		atomicGossipReceivedError:   metrics.GetOrRegisterCounter("gossip_atomic_received_error", nil),
		atomicGossipReceivedKnown:   metrics.GetOrRegisterCounter("gossip_atomic_received_known", nil),
		atomicGossipReceivedNew:     metrics.GetOrRegisterCounter("gossip_atomic_received_new", nil),
		ethTxsGossipReceivedError:   metrics.GetOrRegisterCounter("gossip_eth_txs_received_error", nil),
		ethTxsGossipReceivedKnown:   metrics.GetOrRegisterCounter("gossip_eth_txs_received_known", nil),
		ethTxsGossipReceivedNew:     metrics.GetOrRegisterCounter("gossip_eth_txs_received_new", nil),
	}
}

// incoming messages
func (g *gossipStats) IncAtomicGossipReceived() { g.atomicGossipReceived.Inc(1) }
func (g *gossipStats) IncEthTxsGossipReceived() { g.ethTxsGossipReceived.Inc(1) }

// new vs. known txs received
func (g *gossipStats) IncAtomicGossipReceivedDropped() { g.atomicGossipReceivedDropped.Inc(1) }
func (g *gossipStats) IncAtomicGossipReceivedError()   { g.atomicGossipReceivedError.Inc(1) }
func (g *gossipStats) IncAtomicGossipReceivedKnown()   { g.atomicGossipReceivedKnown.Inc(1) }
func (g *gossipStats) IncAtomicGossipReceivedNew()     { g.atomicGossipReceivedNew.Inc(1) }
func (g *gossipStats) IncEthTxsGossipReceivedError()   { g.ethTxsGossipReceivedError.Inc(1) }
func (g *gossipStats) IncEthTxsGossipReceivedKnown()   { g.ethTxsGossipReceivedKnown.Inc(1) }
func (g *gossipStats) IncEthTxsGossipReceivedNew()     { g.ethTxsGossipReceivedNew.Inc(1) }

// outgoing messages
func (g *gossipStats) IncAtomicGossipSent() { g.atomicGossipSent.Inc(1) }
func (g *gossipStats) IncEthTxsGossipSent() { g.ethTxsGossipSent.Inc(1) }

// regossip
func (g *gossipStats) IncEthTxsRegossipQueued() { g.ethTxsRegossipQueued.Inc(1) }
func (g *gossipStats) IncEthTxsRegossipQueuedLocal(count int) {
	g.ethTxsRegossipQueuedLocal.Inc(int64(count))
}
func (g *gossipStats) IncEthTxsRegossipQueuedRemote(count int) {
	g.ethTxsRegossipQueuedRemote.Inc(int64(count))
}

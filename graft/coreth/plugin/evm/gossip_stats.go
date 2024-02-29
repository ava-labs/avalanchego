// (c) 2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import "github.com/ava-labs/coreth/metrics"

var _ GossipStats = &gossipStats{}

// GossipStats contains methods for updating incoming and outgoing gossip stats.
type GossipStats interface {
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

// gossipStats implements stats for incoming and outgoing gossip stats.
type gossipStats struct {
	// messages
	atomicGossipReceived metrics.Counter
	ethTxsGossipReceived metrics.Counter

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
		atomicGossipReceived: metrics.GetOrRegisterCounter("gossip_atomic_received", nil),
		ethTxsGossipReceived: metrics.GetOrRegisterCounter("gossip_eth_txs_received", nil),

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

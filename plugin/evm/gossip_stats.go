// (c) 2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import "github.com/ava-labs/subnet-evm/metrics"

var _ GossipStats = &gossipStats{}

// GossipStats contains methods for updating incoming and outgoing gossip stats.
type GossipStats interface {
	IncEthTxsGossipReceived()

	// new vs. known txs received
	IncEthTxsGossipReceivedError()
	IncEthTxsGossipReceivedKnown()
	IncEthTxsGossipReceivedNew()
}

// gossipStats implements stats for incoming and outgoing gossip stats.
type gossipStats struct {
	// messages
	ethTxsGossipReceived metrics.Counter

	// new vs. known txs received
	ethTxsGossipReceivedError metrics.Counter
	ethTxsGossipReceivedKnown metrics.Counter
	ethTxsGossipReceivedNew   metrics.Counter
}

func NewGossipStats() GossipStats {
	return &gossipStats{
		ethTxsGossipReceived:      metrics.GetOrRegisterCounter("gossip_eth_txs_received", nil),
		ethTxsGossipReceivedError: metrics.GetOrRegisterCounter("gossip_eth_txs_received_error", nil),
		ethTxsGossipReceivedKnown: metrics.GetOrRegisterCounter("gossip_eth_txs_received_known", nil),
		ethTxsGossipReceivedNew:   metrics.GetOrRegisterCounter("gossip_eth_txs_received_new", nil),
	}
}

// incoming messages
func (g *gossipStats) IncEthTxsGossipReceived() { g.ethTxsGossipReceived.Inc(1) }

// new vs. known txs received
func (g *gossipStats) IncEthTxsGossipReceivedError() { g.ethTxsGossipReceivedError.Inc(1) }
func (g *gossipStats) IncEthTxsGossipReceivedKnown() { g.ethTxsGossipReceivedKnown.Inc(1) }
func (g *gossipStats) IncEthTxsGossipReceivedNew()   { g.ethTxsGossipReceivedNew.Inc(1) }

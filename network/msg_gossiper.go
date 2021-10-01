package network

import (
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/network/message"
	"github.com/ava-labs/avalanchego/utils/constants"
)

// Assumes [n.stateLock] is not held.
func (n *network) Gossip(msgType constants.MsgType, msg message.OutboundMessage, subnetID ids.ID) bool {
	switch msgType {
	case constants.AppGossipMsg:
		return n.appGossip(msg, subnetID)
	case constants.GossipMsg:
		return n.gossipContainer(msg, subnetID, int(n.config.GossipOnAcceptSize))
	default:
		n.log.Error("Unhandled message type %v. Gossiping nothing.", msgType)
		return false
	}
}

func (n *network) appGossip(msg message.OutboundMessage, subnetID ids.ID) bool {
	now := n.clock.Time()
	n.stateLock.RLock()
	// Gossip the message to [n.config.AppGossipNonValidatorSize] random nodes
	// in the network.
	peersAll, err := n.peers.sample(subnetID, false, int(n.config.AppGossipNonValidatorSize))
	if err != nil {
		n.log.Debug("failed to sample %d peers for AppGossip: %s", n.config.AppGossipNonValidatorSize, err)
		n.stateLock.RUnlock()
		return false
	}

	// Gossip the message to [n.config.AppGossipValidatorSize] random validators
	// in the network. This does not gossip by stake - but uniformly to the
	// validator set.
	peersValidators, err := n.peers.sample(subnetID, true, int(n.config.AppGossipValidatorSize))
	n.stateLock.RUnlock()
	if err != nil {
		n.log.Debug("failed to sample %d validators for AppGossip: %s", n.config.AppGossipValidatorSize, err)
		return false
	}

	sentPeers := ids.ShortSet{}
	for _, peers := range [][]*peer{peersAll, peersValidators} {
		for _, peer := range peers {
			if sentPeers.Contains(peer.nodeID) {
				continue
			}
			sentPeers.Add(peer.nodeID)

			sent := peer.Send(msg, false)
			if !sent {
				n.log.Debug("failed to send AppGossip(%s)", peer.nodeID)
				n.metrics.appGossip.numFailed.Inc()
				n.sendFailRateCalculator.Observe(1, now)
				continue
			}

			n.metrics.appGossip.numSent.Inc()
			n.metrics.appGossip.sentBytes.Add(float64(len(msg.Bytes())))
			n.sendFailRateCalculator.Observe(0, now)
			if saved := msg.BytesSavedCompression(); saved != 0 {
				n.metrics.appGossip.savedSentBytes.Observe(float64(saved))
			}
		}
	}
	return true
}

// Assumes [n.stateLock] is not held.
func (n *network) gossipContainer(msg message.OutboundMessage, subnetID ids.ID, sampleSize int) bool {
	now := n.clock.Time()

	n.stateLock.RLock()
	peers, err := n.peers.sample(subnetID, false, sampleSize)
	n.stateLock.RUnlock()
	if err != nil {
		return false
	}

	res := false // gossip is successful if at least one node gets gossiped
	for _, peer := range peers {
		sent := peer.Send(msg, false)
		if !sent {
			n.sendFailRateCalculator.Observe(1, now)
			n.metrics.put.numFailed.Inc()
			continue
		}

		res = true
		n.metrics.put.numSent.Inc()
		n.metrics.put.sentBytes.Add(float64(len(msg.Bytes())))
		// assume that if [saved] == 0, [msg] wasn't compressed
		if saved := msg.BytesSavedCompression(); saved != 0 {
			n.metrics.put.savedSentBytes.Observe(float64(saved))
		}
		n.sendFailRateCalculator.Observe(0, now)
	}
	return res
}

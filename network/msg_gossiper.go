package network

import (
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/network/message"
	"github.com/ava-labs/avalanchego/utils/constants"
)

type msgGossiper struct {
	meterSuccess func(msg message.Message)
	meterFailure func(nodeID ids.ShortID, msg message.Message)
}

type msgsGossiper struct {
	n         *network
	gossipers map[constants.MsgType]msgGossiper
}

func newMsgsGossiper(netw *network) *msgsGossiper {
	res := &msgsGossiper{
		n:         netw,
		gossipers: make(map[constants.MsgType]msgGossiper),
	}

	// initialize msgs gossiper

	// constants.AppGossipMsg
	res.gossipers[constants.AppGossipMsg] = msgGossiper{
		meterSuccess: func(msg message.Message) {
			var (
				now    = res.n.clock.Time()
				msgLen = len(msg.Bytes())
			)
			res.n.metrics.appGossip.numSent.Inc()
			res.n.metrics.appGossip.sentBytes.Add(float64(msgLen))
			res.n.sendFailRateCalculator.Observe(0, now)
			if saved := msg.BytesSavedCompression(); saved != 0 {
				res.n.metrics.appGossip.savedSentBytes.Observe(float64(saved))
			}
		},
		meterFailure: func(nodeID ids.ShortID, msg message.Message) {
			now := res.n.clock.Time()
			res.n.metrics.appGossip.numFailed.Inc()
			res.n.sendFailRateCalculator.Observe(1, now)
		},
	}

	// constants.GossipMsg
	res.gossipers[constants.GossipMsg] = msgGossiper{
		meterSuccess: func(msg message.Message) {
			var (
				now    = res.n.clock.Time()
				msgLen = len(msg.Bytes())
			)

			res.n.metrics.put.numSent.Inc()
			res.n.metrics.put.sentBytes.Add(float64(msgLen))
			// assume that if [saved] == 0, [msg] wasn't compressed
			if saved := msg.BytesSavedCompression(); saved != 0 {
				res.n.metrics.put.savedSentBytes.Observe(float64(saved))
			}
			res.n.sendFailRateCalculator.Observe(0, now)
		},
		meterFailure: func(nodeID ids.ShortID, msg message.Message) {
			now := res.n.clock.Time()
			res.n.sendFailRateCalculator.Observe(1, now)
			res.n.metrics.put.numFailed.Inc()
		},
	}

	return res
}

// Assumes [n.stateLock] is not held.
func (mg *msgsGossiper) Gossip(msgType constants.MsgType, msg message.Message, subnetID ids.ID) bool {
	sampleSize := int(mg.n.config.AppGossipSize)
	return mg.gossip(msgType, msg, subnetID, sampleSize)
}

func (mg *msgsGossiper) gossip(msgType constants.MsgType, msg message.Message, subnetID ids.ID, sampleSize int) bool {
	msgGos, ok := mg.gossipers[msgType]
	if !ok {
		mg.n.log.Error("Unhandled message type %v. Gossiping nothing.", msgType)
		return false
	}

	mg.n.stateLock.RLock()
	peers, err := mg.n.peers.sample(subnetID, sampleSize)
	mg.n.stateLock.RUnlock()
	if err != nil {
		mg.n.log.Debug("failed to sample %d peers for Gossip: %s", sampleSize, err)
		return false
	}

	res := false // gossip succeeds if at least one Send is successful
	for _, peer := range peers {
		if sent := peer.Send(msg, false); !sent {
			msgGos.meterFailure(peer.nodeID, msg)
			continue
		}
		msgGos.meterSuccess(msg)
		res = true
	}
	return res
}

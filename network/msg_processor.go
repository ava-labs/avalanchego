package network

import (
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/network/message"
	"github.com/ava-labs/avalanchego/utils/constants"
)

type msgProcessor struct {
	canModifyMsg bool
	meterSuccess func(msg message.OutboundMessage)
	meterFailure func(nodeID ids.ShortID, msg message.OutboundMessage)
}

type msgsProcessor struct {
	n          *network
	processors map[constants.MsgType]msgProcessor
}

func newMsgsProcessor(netw *network) *msgsProcessor {
	res := &msgsProcessor{
		n:          netw,
		processors: make(map[constants.MsgType]msgProcessor),
	}

	// initialize msgs processors

	// constants.GetAcceptedFrontierMsg
	res.processors[constants.GetAcceptedFrontierMsg] = msgProcessor{
		canModifyMsg: false,
		meterSuccess: func(msg message.OutboundMessage) {
			var (
				now    = res.n.clock.Time()
				msgLen = len(msg.Bytes())
			)

			res.n.metrics.getAcceptedFrontier.numSent.Inc()
			res.n.sendFailRateCalculator.Observe(0, now)
			res.n.metrics.getAcceptedFrontier.sentBytes.Add(float64(msgLen))
			// assume that if [saved] == 0, [msg] wasn't compressed
			if saved := msg.BytesSavedCompression(); saved != 0 {
				res.n.metrics.getAcceptedFrontier.savedSentBytes.Observe(float64(saved))
			}
		},
		meterFailure: func(nodeID ids.ShortID, msg message.OutboundMessage) {
			now := res.n.clock.Time()
			res.n.metrics.getAcceptedFrontier.numFailed.Inc()
			res.n.sendFailRateCalculator.Observe(1, now)
		},
	}

	// constants.AcceptedFrontierMsg
	res.processors[constants.AcceptedFrontierMsg] = msgProcessor{
		canModifyMsg: true,
		meterSuccess: func(msg message.OutboundMessage) {
			var (
				now    = res.n.clock.Time()
				msgLen = len(msg.Bytes())
			)
			res.n.metrics.acceptedFrontier.numSent.Inc()
			res.n.sendFailRateCalculator.Observe(0, now)
			res.n.metrics.acceptedFrontier.sentBytes.Add(float64(msgLen))
			// assume that if [saved] == 0, [msg] wasn't compressed
			if saved := msg.BytesSavedCompression(); saved != 0 {
				res.n.metrics.acceptedFrontier.savedSentBytes.Observe(float64(saved))
			}
		},
		meterFailure: func(nodeID ids.ShortID, msg message.OutboundMessage) {
			now := res.n.clock.Time()
			res.n.metrics.acceptedFrontier.numFailed.Inc()
			res.n.sendFailRateCalculator.Observe(1, now)
		},
	}
	// constants.GetAcceptedMsg
	res.processors[constants.GetAcceptedMsg] = msgProcessor{
		canModifyMsg: false,
		meterSuccess: func(msg message.OutboundMessage) {
			var (
				now    = res.n.clock.Time()
				msgLen = len(msg.Bytes())
			)

			res.n.metrics.getAccepted.numSent.Inc()
			res.n.sendFailRateCalculator.Observe(0, now)
			res.n.metrics.getAccepted.sentBytes.Add(float64(msgLen))
			// assume that if [saved] == 0, [msg] wasn't compressed
			if saved := msg.BytesSavedCompression(); saved != 0 {
				res.n.metrics.getAccepted.savedSentBytes.Observe(float64(saved))
			}
		},
		meterFailure: func(vID ids.ShortID, msg message.OutboundMessage) {
			now := res.n.clock.Time()
			res.n.metrics.getAccepted.numFailed.Inc()
			res.n.sendFailRateCalculator.Observe(1, now)
		},
	}

	// constants.AcceptedMsg
	res.processors[constants.AcceptedMsg] = msgProcessor{
		canModifyMsg: true,
		meterSuccess: func(msg message.OutboundMessage) {
			var (
				now    = res.n.clock.Time()
				msgLen = len(msg.Bytes())
			)

			res.n.sendFailRateCalculator.Observe(0, now)
			res.n.metrics.accepted.numSent.Inc()
			res.n.metrics.accepted.sentBytes.Add(float64(msgLen))
			// assume that if [saved] == 0, [msg] wasn't compressed
			if saved := msg.BytesSavedCompression(); saved != 0 {
				res.n.metrics.accepted.savedSentBytes.Observe(float64(saved))
			}
		},
		meterFailure: func(nodeID ids.ShortID, msg message.OutboundMessage) {
			now := res.n.clock.Time()
			res.n.metrics.accepted.numFailed.Inc()
			res.n.sendFailRateCalculator.Observe(1, now)
		},
	}

	// constants.GetAncestorsMsg
	res.processors[constants.GetAncestorsMsg] = msgProcessor{
		canModifyMsg: true,
		meterSuccess: func(msg message.OutboundMessage) {
			var (
				now    = res.n.clock.Time()
				msgLen = len(msg.Bytes())
			)

			res.n.metrics.getAncestors.numSent.Inc()
			res.n.sendFailRateCalculator.Observe(0, now)
			res.n.metrics.getAncestors.sentBytes.Add(float64(msgLen))
			// assume that if [saved] == 0, [msg] wasn't compressed
			if saved := msg.BytesSavedCompression(); saved != 0 {
				res.n.metrics.getAncestors.savedSentBytes.Observe(float64(saved))
			}
		},
		meterFailure: func(nodeID ids.ShortID, msg message.OutboundMessage) {
			now := res.n.clock.Time()
			res.n.metrics.getAncestors.numFailed.Inc()
			res.n.sendFailRateCalculator.Observe(1, now)
		},
	}

	// constants.MultiPutMsg
	res.processors[constants.MultiPutMsg] = msgProcessor{
		canModifyMsg: true,
		meterSuccess: func(msg message.OutboundMessage) {
			var (
				now    = res.n.clock.Time()
				msgLen = len(msg.Bytes())
			)

			res.n.metrics.multiPut.numSent.Inc()
			res.n.sendFailRateCalculator.Observe(0, now)
			res.n.metrics.multiPut.sentBytes.Add(float64(msgLen))
			// assume that if [saved] == 0, [msg] wasn't compressed
			if saved := msg.BytesSavedCompression(); saved != 0 {
				res.n.metrics.multiPut.savedSentBytes.Observe(float64(saved))
			}
		},
		meterFailure: func(nodeID ids.ShortID, msg message.OutboundMessage) {
			now := res.n.clock.Time()
			res.n.metrics.multiPut.numFailed.Inc()
			res.n.sendFailRateCalculator.Observe(1, now)
		},
	}

	// constants.GetMsg
	res.processors[constants.GetMsg] = msgProcessor{
		canModifyMsg: true,
		meterSuccess: func(msg message.OutboundMessage) {
			var (
				now    = res.n.clock.Time()
				msgLen = len(msg.Bytes())
			)

			res.n.metrics.get.numSent.Inc()
			res.n.sendFailRateCalculator.Observe(0, now)
			res.n.metrics.get.sentBytes.Add(float64(msgLen))
			// assume that if [saved] == 0, [msg] wasn't compressed
			if saved := msg.BytesSavedCompression(); saved != 0 {
				res.n.metrics.get.savedSentBytes.Observe(float64(saved))
			}
		},
		meterFailure: func(nodeID ids.ShortID, msg message.OutboundMessage) {
			now := res.n.clock.Time()
			res.n.metrics.get.numFailed.Inc()
			res.n.sendFailRateCalculator.Observe(1, now)
		},
	}

	// constants.PutMsg
	res.processors[constants.PutMsg] = msgProcessor{
		canModifyMsg: true,
		meterSuccess: func(msg message.OutboundMessage) {
			var (
				now    = res.n.clock.Time()
				msgLen = len(msg.Bytes())
			)

			res.n.metrics.put.numSent.Inc()
			res.n.sendFailRateCalculator.Observe(0, now)
			res.n.metrics.put.sentBytes.Add(float64(msgLen))
			// assume that if [saved] == 0, [msg] wasn't compressed
			if saved := msg.BytesSavedCompression(); saved != 0 {
				res.n.metrics.put.savedSentBytes.Observe(float64(saved))
			}
		},
		meterFailure: func(nodeID ids.ShortID, msg message.OutboundMessage) {
			now := res.n.clock.Time()
			res.n.metrics.put.numFailed.Inc()
			res.n.sendFailRateCalculator.Observe(1, now)
		},
	}

	// constants.PushQueryMsg
	res.processors[constants.PushQueryMsg] = msgProcessor{
		canModifyMsg: false,
		meterSuccess: func(msg message.OutboundMessage) {
			var (
				now    = res.n.clock.Time()
				msgLen = len(msg.Bytes())
			)

			res.n.metrics.pushQuery.numSent.Inc()
			res.n.sendFailRateCalculator.Observe(0, now)
			res.n.metrics.pushQuery.sentBytes.Add(float64(msgLen))
			// assume that if [saved] == 0, [msg] wasn't compressed
			if saved := msg.BytesSavedCompression(); saved != 0 {
				res.n.metrics.pushQuery.savedSentBytes.Observe(float64(saved))
			}
		},
		meterFailure: func(vID ids.ShortID, msg message.OutboundMessage) {
			now := res.n.clock.Time()
			res.n.metrics.pushQuery.numFailed.Inc()
			res.n.sendFailRateCalculator.Observe(1, now)
		},
	}

	// constants.PullQueryMsg
	res.processors[constants.PullQueryMsg] = msgProcessor{
		canModifyMsg: false,
		meterSuccess: func(msg message.OutboundMessage) {
			var (
				now    = res.n.clock.Time()
				msgLen = len(msg.Bytes())
			)

			res.n.metrics.pullQuery.numSent.Inc()
			res.n.sendFailRateCalculator.Observe(0, now)
			res.n.metrics.pullQuery.sentBytes.Add(float64(msgLen))
			// assume that if [saved] == 0, [msg] wasn't compressed
			if saved := msg.BytesSavedCompression(); saved != 0 {
				res.n.metrics.pullQuery.savedSentBytes.Observe(float64(saved))
			}
		},
		meterFailure: func(vID ids.ShortID, msg message.OutboundMessage) {
			now := res.n.clock.Time()
			res.n.metrics.pullQuery.numFailed.Inc()
			res.n.sendFailRateCalculator.Observe(1, now)
		},
	}

	// constants.ChitsMsg
	res.processors[constants.ChitsMsg] = msgProcessor{
		canModifyMsg: true,
		meterSuccess: func(msg message.OutboundMessage) {
			var (
				now    = res.n.clock.Time()
				msgLen = len(msg.Bytes())
			)

			res.n.sendFailRateCalculator.Observe(0, now)
			res.n.metrics.chits.numSent.Inc()
			res.n.metrics.chits.sentBytes.Add(float64(msgLen))
			// assume that if [saved] == 0, [msg] wasn't compressed
			if saved := msg.BytesSavedCompression(); saved != 0 {
				res.n.metrics.chits.savedSentBytes.Observe(float64(saved))
			}
		},
		meterFailure: func(nodeID ids.ShortID, msg message.OutboundMessage) {
			now := res.n.clock.Time()
			res.n.metrics.chits.numFailed.Inc()
			res.n.sendFailRateCalculator.Observe(1, now)
		},
	}

	// constants.AppRequestMsg
	res.processors[constants.AppRequestMsg] = msgProcessor{
		canModifyMsg: false,
		meterSuccess: func(msg message.OutboundMessage) {
			var (
				now    = res.n.clock.Time()
				msgLen = len(msg.Bytes())
			)

			res.n.metrics.appRequest.numSent.Inc()
			res.n.sendFailRateCalculator.Observe(0, now)
			res.n.metrics.appRequest.sentBytes.Add(float64(msgLen))
			if saved := msg.BytesSavedCompression(); saved != 0 {
				res.n.metrics.appRequest.savedSentBytes.Observe(float64(saved))
			}
		},
		meterFailure: func(nodeID ids.ShortID, msg message.OutboundMessage) {
			now := res.n.clock.Time()
			res.n.metrics.appRequest.numFailed.Inc()
			res.n.sendFailRateCalculator.Observe(1, now)
		},
	}

	// constants.AppResponseMsg
	res.processors[constants.AppResponseMsg] = msgProcessor{
		canModifyMsg: true,
		meterSuccess: func(msg message.OutboundMessage) {
			var (
				now    = res.n.clock.Time()
				msgLen = len(msg.Bytes())
			)

			res.n.metrics.appResponse.numSent.Inc()
			res.n.sendFailRateCalculator.Observe(0, now)
			res.n.metrics.appResponse.sentBytes.Add(float64(msgLen))
			if saved := msg.BytesSavedCompression(); saved != 0 {
				res.n.metrics.appResponse.savedSentBytes.Observe(float64(saved))
			}
		},
		meterFailure: func(nodeID ids.ShortID, msg message.OutboundMessage) {
			now := res.n.clock.Time()
			res.n.metrics.appResponse.numFailed.Inc()
			res.n.sendFailRateCalculator.Observe(1, now)
		},
	}

	return res
}

// Assumes [n.stateLock] is not held.
func (mp *msgsProcessor) Send(msgType constants.MsgType, msg message.OutboundMessage, nodeIDs ids.ShortSet) ids.ShortSet {
	res := ids.NewShortSet(nodeIDs.Len())
	msgProc, ok := mp.processors[msgType]
	if ok {
		mp.n.log.Error("Unhandled message type %v. Sending nothing.", msgType)
		return res
	}

	for _, peerElement := range mp.n.getPeers(nodeIDs) {
		peer := peerElement.peer
		nodeID := peerElement.id
		if peer != nil && peer.finishedHandshake.GetValue() && peer.Send(msg, msgProc.canModifyMsg) {
			res.Add(nodeID)
			msgProc.meterSuccess(msg)
			continue
		}

		msgProc.meterFailure(nodeID, msg)
	}
	return res
}

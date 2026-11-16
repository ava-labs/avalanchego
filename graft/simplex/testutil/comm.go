// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package testutil

import (
	"sync"

	"github.com/ava-labs/avalanchego/graft/simplex/common"
	"github.com/stretchr/testify/require"
)

// MessageFilter is a function type that determines whether a message can be
// transmitted from one node to another.
// Parameters:
//   - msg: The message being evaluated for transmission
//   - from: The ID of the sending node
//   - to: The ID of the receiving node
//
// Returns:
//   - bool: true if the message can be transmitted, false otherwise
type MessageFilter func(msg *common.Message, from common.NodeID, to common.NodeID) bool

type NoopComm common.Nodes

func (n NoopComm) Validators() common.Nodes {
	return common.Nodes(n)
}

func (n NoopComm) Send(*common.Message, common.NodeID) {

}

func (n NoopComm) Broadcast(msg *common.Message) {

}

type TestComm struct {
	from          common.NodeID
	net           *BasicInMemoryNetwork
	messageFilter MessageFilter
	lock          sync.RWMutex
}

func NewTestComm(from common.NodeID, net *BasicInMemoryNetwork, messageFilter MessageFilter) *TestComm {
	return &TestComm{
		from:          from,
		net:           net,
		messageFilter: messageFilter,
	}
}

func (c *TestComm) Validators() common.Nodes {
	return c.net.nodeWeights
}

func (c *TestComm) Send(msg *common.Message, destination common.NodeID) {
	if !c.isMessagePermitted(msg, destination) {
		return
	}

	// cannot send if either [from] or [destination] is not connected
	if c.net.IsDisconnected(destination) || c.net.IsDisconnected(c.from) {
		for _, instance := range c.net.GetInstances() {
			if instance.E.ID == destination {
				instance.l.Info("Node is disconnected not sending message")
			}
		}

		return
	}

	c.maybeTranslateOutgoingToIncomingMessageTypes(msg)

	for _, instance := range c.net.GetInstances() {
		if instance.E.ID == c.from {
			instance.l.Info("Enqueuing message")
			continue
		}
		if instance.E.ID == destination {
			instance.enqueue(msg, c.from, destination)
			return
		}
	}
}

func (c *TestComm) SetFilter(filter MessageFilter) {
	c.lock.Lock()
	defer c.lock.Unlock()

	c.messageFilter = filter
}

func (c *TestComm) maybeTranslateOutgoingToIncomingMessageTypes(msg *common.Message) {
	if msg.VerifiedReplicationResponse != nil {
		data := make([]common.QuorumRound, 0, len(msg.VerifiedReplicationResponse.Data))

		for _, verifiedQuorumRound := range msg.VerifiedReplicationResponse.Data {
			// Outgoing block is of type verified block but incoming block is of type Block,
			// so we do a type cast because the test block implements both.
			quorumRound := common.QuorumRound{}
			if verifiedQuorumRound.EmptyNotarization != nil {
				quorumRound.EmptyNotarization = UnverifiedEmptyNotarization(verifiedQuorumRound.EmptyNotarization)
			}
			if verifiedQuorumRound.Block != nil {
				quorumRound.Block = verifiedQuorumRound.Block.(common.Block)
			}
			if verifiedQuorumRound.Notarization != nil {
				quorumRound.Notarization = UnverifiedNotarization(verifiedQuorumRound.Notarization)
			}
			if verifiedQuorumRound.Finalization != nil {
				quorumRound.Finalization = UnverifiedFinalization(verifiedQuorumRound.Finalization)
			}

			data = append(data, quorumRound)
		}

		latestRound := verifiedQRtoQR(msg.VerifiedReplicationResponse.LatestRound)
		latestSeq := verifiedQRtoQR(msg.VerifiedReplicationResponse.LatestFinalizedSeq)

		require.Nil(
			c.net.t,
			msg.ReplicationResponse,
			"message cannot include ReplicationResponse & VerifiedReplicationResponse",
		)

		msg.ReplicationResponse = &common.ReplicationResponse{
			Data:        data,
			LatestRound: latestRound,
			LatestSeq:   latestSeq,
		}
	}

	if msg.VerifiedBlockMessage != nil {
		require.Nil(c.net.t, msg.BlockMessage, "message cannot include BlockMessage & VerifiedBlockMessage")
		msg.BlockMessage = &common.BlockMessage{
			Block: msg.VerifiedBlockMessage.VerifiedBlock.(common.Block),
			Vote:  msg.VerifiedBlockMessage.Vote,
		}
	}
}

// UnverifiedQuorumRound returns the unverified counterpart of vqr.
func UnverifiedQuorumRound(vqr *common.VerifiedQuorumRound) *common.QuorumRound {
	return verifiedQRtoQR(vqr)
}

// UnverifiedNotarization returns the unverified counterpart of n.
func UnverifiedNotarization(n *common.VerifiedNotarization) *common.Notarization {
	if n == nil {
		return nil
	}
	return &common.Notarization{Vote: n.Vote, QC: n.QC.UnVerified()}
}

// UnverifiedFinalization returns the unverified counterpart of f.
func UnverifiedFinalization(f *common.VerifiedFinalization) *common.Finalization {
	if f == nil {
		return nil
	}
	return &common.Finalization{Finalization: f.Finalization, QC: f.QC.UnVerified()}
}

// UnverifiedEmptyNotarization returns the unverified counterpart of en.
func UnverifiedEmptyNotarization(en *common.VerifiedEmptyNotarization) *common.EmptyNotarization {
	if en == nil {
		return nil
	}
	return &common.EmptyNotarization{Vote: en.Vote, QC: en.QC.UnVerified()}
}

func verifiedQRtoQR(vqr *common.VerifiedQuorumRound) *common.QuorumRound {
	if vqr == nil {
		return nil
	}

	qr := &common.QuorumRound{
		Notarization:      UnverifiedNotarization(vqr.Notarization),
		Finalization:      UnverifiedFinalization(vqr.Finalization),
		EmptyNotarization: UnverifiedEmptyNotarization(vqr.EmptyNotarization),
	}

	if vqr.Block != nil {
		qr.Block = vqr.Block.(common.Block)
	}

	return qr
}

func (c *TestComm) isMessagePermitted(msg *common.Message, destination common.NodeID) bool {
	c.lock.RLock()
	defer c.lock.RUnlock()

	return c.messageFilter(msg, c.from, destination)
}

func (c *TestComm) Broadcast(msg *common.Message) {
	c.maybeTranslateOutgoingToIncomingMessageTypes(msg)

	for _, instance := range c.net.GetInstances() {
		if !c.isMessagePermitted(msg, instance.E.ID) {
			continue
		}
		// Skip sending the message to yourself or disconnected nodeWeights
		if c.from == instance.E.ID || c.net.IsDisconnected(instance.E.ID) || c.net.IsDisconnected(c.from) {
			continue
		}

		instance.enqueue(msg, c.from, instance.E.ID)
	}
}

// AllowAllMessages allows every message to be sent
func AllowAllMessages(*common.Message, common.NodeID, common.NodeID) bool {
	return true
}

func NewNoopComm(nodes common.NodeIDs) NoopComm {
	return NoopComm(nodes.EqualWeightedNodes())
}

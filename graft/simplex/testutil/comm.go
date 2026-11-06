// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package testutil

import (
	"bytes"
	"sync"

	"github.com/ava-labs/simplex/common"
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
			if bytes.Equal(instance.E.ID, destination) {
				instance.l.Info("Node is disconnected not sending message")
			}
		}

		return
	}

	c.maybeTranslateOutgoingToIncomingMessageTypes(msg)

	for _, instance := range c.net.GetInstances() {
		if bytes.Equal(instance.E.ID, c.from) {
			instance.l.Info("Enqueuing message")
			continue
		}
		if bytes.Equal(instance.E.ID, destination) {
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
				quorumRound.EmptyNotarization = verifiedQuorumRound.EmptyNotarization
			}
			if verifiedQuorumRound.VerifiedBlock != nil {
				quorumRound.Block = verifiedQuorumRound.VerifiedBlock.(common.Block)
			}
			if verifiedQuorumRound.Notarization != nil {
				quorumRound.Notarization = verifiedQuorumRound.Notarization
			}
			if verifiedQuorumRound.Finalization != nil {
				quorumRound.Finalization = verifiedQuorumRound.Finalization
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

func verifiedQRtoQR(vqr *common.VerifiedQuorumRound) *common.QuorumRound {
	if vqr == nil {
		return nil
	}

	qr := &common.QuorumRound{
		Notarization:      vqr.Notarization,
		Finalization:      vqr.Finalization,
		EmptyNotarization: vqr.EmptyNotarization,
	}

	if vqr.VerifiedBlock != nil {
		qr.Block = vqr.VerifiedBlock.(common.Block)
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
		if bytes.Equal(c.from, instance.E.ID) || c.net.IsDisconnected(instance.E.ID) || c.net.IsDisconnected(c.from) {
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

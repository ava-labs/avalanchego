// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package simplex

import (
	"errors"
	"fmt"

	"github.com/ava-labs/simplex"
	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/message"
	"github.com/ava-labs/avalanchego/proto/pb/p2p"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/networking/sender"
	"github.com/ava-labs/avalanchego/subnets"
	"github.com/ava-labs/avalanchego/utils/set"
)

var (
	_               simplex.Communication = (*Comm)(nil)
	errNodeNotFound                       = errors.New("node not found in the validator list")
)

type Comm struct {
	logger   simplex.Logger
	subnetID ids.ID
	chainID  ids.ID
	// broadcastNodes are the nodes that should receive broadcast messages
	broadcastNodes set.Set[ids.NodeID]
	// allNodes are the IDs of all the nodes in the subnet
	allNodes []simplex.NodeID

	// sender is used to send messages to other nodes
	sender     sender.ExternalSender
	msgBuilder message.OutboundMsgBuilder
}

func NewComm(config *Config) (*Comm, error) {
	if _, ok := config.Validators[config.Ctx.NodeID]; !ok {
		config.Log.Warn("Node is not a validator for the subnet",
			zap.Stringer("nodeID", config.Ctx.NodeID),
			zap.Stringer("chainID", config.Ctx.ChainID),
			zap.Stringer("subnetID", config.Ctx.SubnetID),
		)
		return nil, fmt.Errorf("our %w: %s", errNodeNotFound, config.Ctx.NodeID)
	}

	broadcastNodes := set.NewSet[ids.NodeID](len(config.Validators) - 1)
	allNodes := make([]simplex.NodeID, 0, len(config.Validators))
	// grab all the nodes that are validators for the subnet
	for _, vd := range config.Validators {
		allNodes = append(allNodes, vd.NodeID[:])
		if vd.NodeID == config.Ctx.NodeID {
			continue // skip our own node ID
		}

		broadcastNodes.Add(vd.NodeID)
	}

	return &Comm{
		subnetID:       config.Ctx.SubnetID,
		broadcastNodes: broadcastNodes,
		allNodes:       allNodes,
		logger:         config.Log,
		sender:         config.Sender,
		msgBuilder:     config.OutboundMsgBuilder,
		chainID:        config.Ctx.ChainID,
	}, nil
}

func (c *Comm) Nodes() []simplex.NodeID {
	return c.allNodes
}

func (c *Comm) Send(msg *simplex.Message, destination simplex.NodeID) {
	outboundMsg, err := c.simplexMessageToOutboundMessage(msg)
	if err != nil {
		c.logger.Error("Failed creating message", zap.Error(err))
		return
	}

	if outboundMsg == nil {
		c.logger.Debug("Outbound message is nil")
		return
	}

	dest, err := ids.ToNodeID(destination)
	if err != nil {
		c.logger.Error("Failed to convert destination NodeID", zap.Error(err))
		return
	}

	c.sender.Send(outboundMsg, common.SendConfig{NodeIDs: set.Of(dest)}, c.subnetID, subnets.NoOpAllower)
}

func (c *Comm) Broadcast(msg *simplex.Message) {
	outboundMsg, err := c.simplexMessageToOutboundMessage(msg)
	if err != nil {
		c.logger.Error("Failed creating message", zap.Error(err))
		return
	}

	c.sender.Send(outboundMsg, common.SendConfig{NodeIDs: c.broadcastNodes}, c.subnetID, subnets.NoOpAllower)
}

func (c *Comm) simplexMessageToOutboundMessage(msg *simplex.Message) (*message.OutboundMessage, error) {
	var simplexMsg *p2p.Simplex
	switch {
	case msg.VerifiedBlockMessage != nil:
		msg, err := newBlockProposal(c.chainID, msg.VerifiedBlockMessage)
		if err != nil {
			return nil, fmt.Errorf("failed to create block proposal: %w", err)
		}
		simplexMsg = msg
	case msg.VoteMessage != nil:
		simplexMsg = newVote(c.chainID, msg.VoteMessage)
	case msg.EmptyVoteMessage != nil:
		simplexMsg = newEmptyVote(c.chainID, msg.EmptyVoteMessage)
	case msg.FinalizeVote != nil:
		simplexMsg = newFinalizeVote(c.chainID, msg.FinalizeVote)
	case msg.Notarization != nil:
		simplexMsg = newNotarization(c.chainID, msg.Notarization)
	case msg.EmptyNotarization != nil:
		simplexMsg = newEmptyNotarization(c.chainID, msg.EmptyNotarization)
	case msg.Finalization != nil:
		simplexMsg = newFinalization(c.chainID, msg.Finalization)
	case msg.ReplicationRequest != nil:
		simplexMsg = newReplicationRequest(c.chainID, msg.ReplicationRequest)
	case msg.VerifiedReplicationResponse != nil:
		msg, err := newReplicationResponse(c.chainID, msg.VerifiedReplicationResponse)
		if err != nil {
			return nil, fmt.Errorf("failed to create replication response: %w", err)
		}
		simplexMsg = msg
	default:
		return nil, fmt.Errorf("unknown message type: %+v", msg)
	}

	return c.msgBuilder.SimplexMessage(simplexMsg)
}

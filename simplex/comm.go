// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
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
	// nodeID is this nodes ID
	nodeID simplex.NodeID
	// nodes are the IDs of all the nodes in the subnet
	nodes []simplex.NodeID
	// sender is used to send messages to other nodes
	sender     sender.ExternalSender
	msgBuilder message.OutboundMsgBuilder
}

func NewComm(config *Config) (*Comm, error) {
	nodes := make([]simplex.NodeID, 0, len(config.Validators))

	// grab all the nodes that are validators for the subnet
	for _, vd := range config.Validators {
		nodes = append(nodes, vd.NodeID[:])
	}

	if _, ok := config.Validators[config.Ctx.NodeID]; !ok {
		config.Log.Warn("Node is not a validator for the subnet",
			zap.Stringer("nodeID", config.Ctx.NodeID),
			zap.Stringer("chainID", config.Ctx.ChainID),
			zap.Stringer("subnetID", config.Ctx.SubnetID),
		)
		return nil, fmt.Errorf("our %w: %s", errNodeNotFound, config.Ctx.NodeID)
	}

	return &Comm{
		subnetID:   config.Ctx.SubnetID,
		nodes:      nodes,
		nodeID:     config.Ctx.NodeID[:],
		logger:     config.Log,
		sender:     config.Sender,
		msgBuilder: config.OutboundMsgBuilder,
		chainID:    config.Ctx.ChainID,
	}, nil
}

func (c *Comm) Nodes() []simplex.NodeID {
	return c.nodes
}

func (c *Comm) Send(msg *simplex.Message, destination simplex.NodeID) {
	outboundMsg, err := c.simplexMessageToOutboundMessage(msg)
	if err != nil {
		c.logger.Error("Failed creating message", zap.Error(err))
		return
	}

	c.send(outboundMsg, destination)
}

func (c *Comm) send(msg message.OutboundMessage, destination simplex.NodeID) {
	dest, err := ids.ToNodeID(destination)
	if err != nil {
		c.logger.Error("Failed to convert destination NodeID", zap.Error(err))
		return
	}

	c.sender.Send(msg, common.SendConfig{NodeIDs: set.Of(dest)}, c.subnetID, subnets.NoOpAllower)
}

func (c *Comm) Broadcast(msg *simplex.Message) {
	outboundMsg, err := c.simplexMessageToOutboundMessage(msg)
	if err != nil {
		c.logger.Error("Failed creating message", zap.Error(err))
		return
	}

	for _, node := range c.nodes {
		if node.Equals(c.nodeID) {
			continue
		}

		c.send(outboundMsg, node)
	}
}

func (c *Comm) simplexMessageToOutboundMessage(msg *simplex.Message) (message.OutboundMessage, error) {
	var simplexMsg *p2p.Simplex
	switch {
	case msg.VerifiedBlockMessage != nil:
		bytes, err := msg.VerifiedBlockMessage.VerifiedBlock.Bytes()
		if err != nil {
			return nil, fmt.Errorf("failed to serialize block: %w", err)
		}
		simplexMsg = newBlockProposal(c.chainID, bytes, msg.VerifiedBlockMessage.Vote)
	case msg.VoteMessage != nil:
		simplexMsg = newVote(c.chainID, msg.VoteMessage.Vote.BlockHeader, msg.VoteMessage.Signature)
	case msg.EmptyVoteMessage != nil:
		simplexMsg = newEmptyVote(c.chainID, msg.EmptyVoteMessage.Vote.ProtocolMetadata, msg.EmptyVoteMessage.Signature)
	case msg.FinalizeVote != nil:
		simplexMsg = newFinalizeVote(c.chainID, msg.FinalizeVote.Finalization.BlockHeader, msg.FinalizeVote.Signature)
	case msg.Notarization != nil:
		simplexMsg = newNotarization(c.chainID, msg.Notarization.Vote.BlockHeader, msg.Notarization.QC.Bytes())
	case msg.EmptyNotarization != nil:
		simplexMsg = newEmptyNotarization(c.chainID, msg.EmptyNotarization.Vote.ProtocolMetadata, msg.EmptyNotarization.QC.Bytes())
	case msg.Finalization != nil:
		simplexMsg = newFinalization(c.chainID, msg.Finalization.Finalization.BlockHeader, msg.Finalization.QC.Bytes())
	case msg.ReplicationRequest != nil:
		simplexMsg = newReplicationRequest(c.chainID, msg.ReplicationRequest.Seqs, msg.ReplicationRequest.LatestRound)
	case msg.VerifiedReplicationResponse != nil:
		msg, err := newReplicationResponse(c.chainID, msg.VerifiedReplicationResponse.Data, msg.VerifiedReplicationResponse.LatestRound)
		if err != nil {
			return nil, fmt.Errorf("failed to create replication response: %w", err)
		}
		simplexMsg = msg
	default:
		return nil, fmt.Errorf("unknown message type: %T", msg)
	}

	return c.msgBuilder.SimplexMessage(simplexMsg)
}

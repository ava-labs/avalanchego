// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package simplex

import (
	"errors"
	"fmt"
	"sort"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/message"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/networking/sender"
	"github.com/ava-labs/avalanchego/subnets"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/simplex"
	"go.uber.org/zap"
)

var (
	errNodeNotFound = errors.New("node not found in the validator list")
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
	msgBuilder message.SimplexOutboundMessageBuilder
}

func NewComm(config *Config) (*Comm, error) {
	var nodes []simplex.NodeID
	// grab all the nodes that are validators for the subnet
	for _, id := range config.Validators.GetValidatorIDs(config.Ctx.SubnetID) {
		nodes = append(nodes, id[:])
	}

	sortedNodes := sortNodes(nodes)

	var found bool
	for _, node := range sortedNodes {
		if node.Equals(config.Ctx.NodeID[:]) {
			found = true
			break
		}
	}

	if !found {
		return nil, fmt.Errorf("%w: %s", errNodeNotFound, config.Ctx.NodeID)
	}

	c := &Comm{
		subnetID:   config.Ctx.SubnetID,
		nodes:      sortedNodes,
		nodeID:     config.Ctx.NodeID[:],
		logger:     config.Log,
		sender:     config.Sender,
		msgBuilder: config.OutboundMsgBuilder,
		chainID:    config.Ctx.ChainID,
	}

	return c, nil
}

func sortNodes(nodes []simplex.NodeID) []simplex.NodeID {
	sort.Slice(nodes, func(i, j int) bool {
		return nodes[i].String() < nodes[j].String()
	})
	return nodes
}

func (c *Comm) ListNodes() []simplex.NodeID {
	return c.nodes
}

func (c *Comm) SendMessage(msg *simplex.Message, destination simplex.NodeID) {
	var outboundMessage message.OutboundMessage
	var err error
	switch {
	case msg.VerifiedBlockMessage != nil:
		outboundMessage, err = c.msgBuilder.BlockProposal(c.chainID, msg.VerifiedBlockMessage.VerifiedBlock.Bytes(), msg.VerifiedBlockMessage.Vote)
	case msg.VoteMessage != nil:
		outboundMessage, err = c.msgBuilder.Vote(c.chainID, msg.VoteMessage.Vote.BlockHeader, msg.VoteMessage.Signature)
	case msg.EmptyVoteMessage != nil:
		outboundMessage, err = c.msgBuilder.EmptyVote(c.chainID, msg.EmptyVoteMessage.Vote.ProtocolMetadata, msg.EmptyVoteMessage.Signature)
	case msg.FinalizeVote != nil:
		outboundMessage, err = c.msgBuilder.FinalizeVote(c.chainID, msg.FinalizeVote.Finalization.BlockHeader, msg.FinalizeVote.Signature)
	case msg.Notarization != nil:
		outboundMessage, err = c.msgBuilder.Notarization(c.chainID, msg.Notarization.Vote.BlockHeader, msg.Notarization.QC.Bytes())
	case msg.EmptyNotarization != nil:
		outboundMessage, err = c.msgBuilder.EmptyNotarization(c.chainID, msg.EmptyNotarization.Vote.ProtocolMetadata, msg.EmptyNotarization.QC.Bytes())
	case msg.Finalization != nil:
		outboundMessage, err = c.msgBuilder.Finalization(c.chainID, msg.Finalization.Finalization.BlockHeader, msg.Finalization.QC.Bytes())
	}

	if err != nil {
		c.logger.Error("Failed creating message: %w", zap.Error(err))
		return
	}

	dest := ids.NodeID(destination)

	c.sender.Send(outboundMessage, common.SendConfig{NodeIDs: set.Of(dest)}, c.subnetID, subnets.NoOpAllower)
}

func (c *Comm) Broadcast(msg *simplex.Message) {
	for _, node := range c.nodes {
		if node.Equals(c.nodeID) {
			continue
		}

		c.SendMessage(msg, node)
	}
}

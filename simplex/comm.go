// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package simplex

import (
	"bytes"
	"errors"
	"fmt"
	"slices"

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
			zap.String("nodeID", config.Ctx.NodeID.String()),
			zap.String("chainID", config.Ctx.ChainID.String()),
			zap.String("subnetID", config.Ctx.SubnetID.String()),
		)
		return nil, fmt.Errorf("%w could not find our node: %s", errNodeNotFound, config.Ctx.NodeID)
	}

	sortNodes(nodes)

	c := &Comm{
		subnetID:   config.Ctx.SubnetID,
		nodes:      nodes,
		nodeID:     config.Ctx.NodeID[:],
		logger:     config.Log,
		sender:     config.Sender,
		msgBuilder: config.OutboundMsgBuilder,
		chainID:    config.Ctx.ChainID,
	}

	return c, nil
}

// sortNodes sorts the nodes in place by their byte representations.
func sortNodes(nodes []simplex.NodeID) {
	slices.SortFunc(nodes, func(i, j simplex.NodeID) int {
		return bytes.Compare(i, j)
	})
}

func (c *Comm) ListNodes() []simplex.NodeID {
	return c.nodes
}

func (c *Comm) SendMessage(msg *simplex.Message, destination simplex.NodeID) {
	outboundMsg, err := c.simplexMessageToOutboundMessage(msg)
	if err != nil {
		c.logger.Error("Failed creating message", zap.Error(err))
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
	for _, node := range c.nodes {
		if node.Equals(c.nodeID) {
			continue
		}

		c.SendMessage(msg, node)
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
		simplexMsg = newP2PSimplexBlockProposal(c.chainID, bytes, msg.VerifiedBlockMessage.Vote)
	case msg.VoteMessage != nil:
		simplexMsg = newP2PSimplexVote(c.chainID, msg.VoteMessage.Vote.BlockHeader, msg.VoteMessage.Signature)
	case msg.EmptyVoteMessage != nil:
		simplexMsg = newP2PSimplexEmptyVote(c.chainID, msg.EmptyVoteMessage.Vote.ProtocolMetadata, msg.EmptyVoteMessage.Signature)
	case msg.FinalizeVote != nil:
		simplexMsg = newP2PSimplexFinalizeVote(c.chainID, msg.FinalizeVote.Finalization.BlockHeader, msg.FinalizeVote.Signature)
	case msg.Notarization != nil:
		simplexMsg = newP2PSimplexNotarization(c.chainID, msg.Notarization.Vote.BlockHeader, msg.Notarization.QC.Bytes())
	case msg.EmptyNotarization != nil:
		simplexMsg = newP2PSimplexEmptyNotarization(c.chainID, msg.EmptyNotarization.Vote.ProtocolMetadata, msg.EmptyNotarization.QC.Bytes())
	case msg.Finalization != nil:
		simplexMsg = newP2PSimplexFinalization(c.chainID, msg.Finalization.Finalization.BlockHeader, msg.Finalization.QC.Bytes())
	case msg.ReplicationRequest != nil:
		simplexMsg = newP2PSimplexReplicationRequest(c.chainID, msg.ReplicationRequest.Seqs, msg.ReplicationRequest.LatestRound)
	case msg.VerifiedReplicationResponse != nil:
		msg, err := newP2PSimplexReplicationResponse(c.chainID, msg.VerifiedReplicationResponse.Data, msg.VerifiedReplicationResponse.LatestRound)
		if err != nil {
			return nil, fmt.Errorf("failed to create replication response: %w", err)
		}
		simplexMsg = msg
	}

	return c.msgBuilder.SimplexMessage(simplexMsg)
}

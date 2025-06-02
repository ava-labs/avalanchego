package simplex

import (
	"simplex"
	"sort"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/message"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/subnets"
	"github.com/ava-labs/avalanchego/utils/set"
	"go.uber.org/zap"
)

type ValidatorLister interface {
	GetValidatorIDs(subnetID ids.ID) []ids.NodeID
}

type Sender interface {
	Send(
		msg message.OutboundMessage,
		config common.SendConfig,
		subnetID ids.ID,
		allower subnets.Allower,
	) set.Set[ids.NodeID]
}

type Comm struct {
	logger     simplex.Logger
	subnetID   ids.ID
	chainID    ids.ID
	myNodeID   simplex.NodeID
	nodes      []simplex.NodeID
	sender     Sender
	msgBuilder message.OutboundMsgBuilder
}

func NewComm(config *Config) *Comm {
	var nodes []simplex.NodeID
	// grab all the nodes that are validators for the subnet
	for _, id := range config.Validators.GetValidatorIDs(config.Ctx.SubnetID) {
		nodes = append(nodes, id[:])
	}

	sortedNodes := sortNodes(nodes)

	var found bool
	for i, node := range sortedNodes {
		if node.Equals(config.Ctx.NodeID[:]) {
			config.Ctx.Log.Info("Found my node ID", zap.Int("index", i))
			found = true
		}
	}

	if !found {
		config.Ctx.Log.Fatal("My node ID not found in validator list")
	}

	c := &Comm{
		subnetID:   config.Ctx.SubnetID,
		nodes:      sortedNodes,
		myNodeID:   config.Ctx.NodeID[:],
		logger:     config.Ctx.Log,
		sender:     config.Sender,
		msgBuilder: config.OutboundMsgBuilder,
		chainID:    config.Ctx.ChainID,
	}
	return c
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
	// var outboundMessage message.OutboundMessage
	// var err error
	// switch {
	// case msg.VerifiedBlockMessage != nil:
	// 	outboundMessage, err = c.msgBuilder.Block(c.chainID, msg.VerifiedBlockMessage.VerifiedBlock.Bytes(), msg.VerifiedBlockMessage.Vote.Vote.BlockHeader, msg.VerifiedBlockMessage.Vote.Signature)
	// case msg.VoteMessage != nil:
	// 	outboundMessage, err = c.msgBuilder.Vote(c.chainID, msg.VoteMessage.Vote.BlockHeader, msg.VoteMessage.Signature)
	// case msg.EmptyVoteMessage != nil:
	// 	outboundMessage, err = c.msgBuilder.EmptyVote(c.chainID, msg.EmptyVoteMessage.Vote.ProtocolMetadata, msg.EmptyVoteMessage.Signature)
	// case msg.Finalization != nil:
	// 	outboundMessage, err = c.msgBuilder.Finalization(c.chainID, msg.Finalization.Finalization.BlockHeader, msg.Finalization.Signature)
	// case msg.Notarization != nil:
	// 	outboundMessage, err = c.msgBuilder.Notarization(c.chainID, msg.Notarization.Vote.BlockHeader, msg.Notarization.QC.Bytes())
	// case msg.EmptyNotarization != nil:
	// 	outboundMessage, err = c.msgBuilder.EmptyNotarization(c.chainID, msg.EmptyNotarization.Vote.ProtocolMetadata, msg.EmptyNotarization.QC.Bytes())
	// case msg.FinalizationCertificate != nil:
	// 	outboundMessage, err = c.msgBuilder.FinalizationCertificate(c.chainID, msg.FinalizationCertificate.Finalization.BlockHeader, msg.FinalizationCertificate.QC.Bytes())
	// }

	// if err != nil {
	// 	c.logger.Error("Failed creating message: %w", zap.Error(err))
	// 	return
	// }

	// dest := ids.NodeID(destination)

	// c.sender.Send(outboundMessage, common.SendConfig{NodeIDs: set.Of(dest)}, c.subnetID, subnets.NoOpAllower)
}

func (c *Comm) Broadcast(msg *simplex.Message) {
	for _, node := range c.nodes {
		if node.Equals(c.myNodeID) {
			continue
		}

		c.SendMessage(msg, node)
	}
}

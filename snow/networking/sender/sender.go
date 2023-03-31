// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sender

import (
	"context"
	"fmt"

	"github.com/prometheus/client_golang/prometheus"

	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/message"
	"github.com/ava-labs/avalanchego/proto/pb/p2p"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/networking/router"
	"github.com/ava-labs/avalanchego/snow/networking/timeout"
	"github.com/ava-labs/avalanchego/subnets"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/set"
)

var _ common.Sender = (*sender)(nil)

// sender is a wrapper around an ExternalSender.
// Messages to this node are put directly into [router] rather than
// being sent over the network via the wrapped ExternalSender.
// sender registers outbound requests with [router] so that [router]
// fires a timeout if we don't get a response to the request.
type sender struct {
	ctx        *snow.ConsensusContext
	msgCreator message.OutboundMsgBuilder

	sender   ExternalSender // Actually does the sending over the network
	router   router.Router
	timeouts timeout.Manager

	// Request message type --> Counts how many of that request
	// have failed because the node was benched
	failedDueToBench map[message.Op]prometheus.Counter
	engineType       p2p.EngineType
	subnet           subnets.Subnet
}

func New(
	ctx *snow.ConsensusContext,
	msgCreator message.OutboundMsgBuilder,
	externalSender ExternalSender,
	router router.Router,
	timeouts timeout.Manager,
	engineType p2p.EngineType,
	subnet subnets.Subnet,
) (common.Sender, error) {
	s := &sender{
		ctx:              ctx,
		msgCreator:       msgCreator,
		sender:           externalSender,
		router:           router,
		timeouts:         timeouts,
		failedDueToBench: make(map[message.Op]prometheus.Counter, len(message.ConsensusRequestOps)),
		engineType:       engineType,
		subnet:           subnet,
	}

	for _, op := range message.ConsensusRequestOps {
		counter := prometheus.NewCounter(
			prometheus.CounterOpts{
				Name: fmt.Sprintf("%s_failed_benched", op),
				Help: fmt.Sprintf("# of times a %s request was not sent because the node was benched", op),
			},
		)

		switch engineType {
		case p2p.EngineType_ENGINE_TYPE_SNOWMAN:
			if err := ctx.Registerer.Register(counter); err != nil {
				return nil, fmt.Errorf("couldn't register metric for %s: %w", op, err)
			}
		case p2p.EngineType_ENGINE_TYPE_AVALANCHE:
			if err := ctx.AvalancheRegisterer.Register(counter); err != nil {
				return nil, fmt.Errorf("couldn't register metric for %s: %w", op, err)
			}
		default:
			return nil, fmt.Errorf("unknown engine type %s", engineType)
		}

		s.failedDueToBench[op] = counter
	}
	return s, nil
}

func (s *sender) SendGetStateSummaryFrontier(ctx context.Context, nodeIDs set.Set[ids.NodeID], requestID uint32) {
	ctx = utils.Detach(ctx)

	// Note that this timeout duration won't exactly match the one that gets
	// registered. That's OK.
	deadline := s.timeouts.TimeoutDuration()

	// Tell the router to expect a response message or a message notifying
	// that we won't get a response from each of these nodes.
	// We register timeouts for all nodes, regardless of whether we fail
	// to send them a message, to avoid busy looping when disconnected from
	// the internet.
	for nodeID := range nodeIDs {
		inMsg := message.InternalGetStateSummaryFrontierFailed(
			nodeID,
			s.ctx.ChainID,
			requestID,
		)
		s.router.RegisterRequest(
			ctx,
			nodeID,
			s.ctx.ChainID,
			s.ctx.ChainID,
			requestID,
			message.StateSummaryFrontierOp,
			inMsg,
			p2p.EngineType_ENGINE_TYPE_UNSPECIFIED,
		)
	}

	// Sending a message to myself. No need to send it over the network.
	// Just put it right into the router. Asynchronously to avoid deadlock.
	if nodeIDs.Contains(s.ctx.NodeID) {
		nodeIDs.Remove(s.ctx.NodeID)
		inMsg := message.InboundGetStateSummaryFrontier(
			s.ctx.ChainID,
			requestID,
			deadline,
			s.ctx.NodeID,
		)
		go s.router.HandleInbound(ctx, inMsg)
	}

	// Create the outbound message.
	outMsg, err := s.msgCreator.GetStateSummaryFrontier(
		s.ctx.ChainID,
		requestID,
		deadline,
	)

	// Send the message over the network.
	var sentTo set.Set[ids.NodeID]
	if err == nil {
		sentTo = s.sender.Send(
			outMsg,
			nodeIDs,
			s.ctx.SubnetID,
			s.subnet,
		)
	} else {
		s.ctx.Log.Error("failed to build message",
			zap.Stringer("messageOp", message.GetStateSummaryFrontierOp),
			zap.Stringer("chainID", s.ctx.ChainID),
			zap.Uint32("requestID", requestID),
			zap.Duration("deadline", deadline),
			zap.Error(err),
		)
	}

	for nodeID := range nodeIDs {
		if !sentTo.Contains(nodeID) {
			s.ctx.Log.Debug("failed to send message",
				zap.Stringer("messageOp", message.GetStateSummaryFrontierOp),
				zap.Stringer("nodeID", nodeID),
				zap.Stringer("chainID", s.ctx.ChainID),
				zap.Uint32("requestID", requestID),
			)
		}
	}
}

func (s *sender) SendStateSummaryFrontier(ctx context.Context, nodeID ids.NodeID, requestID uint32, summary []byte) {
	ctx = utils.Detach(ctx)

	// Sending this message to myself.
	if nodeID == s.ctx.NodeID {
		inMsg := message.InboundStateSummaryFrontier(
			s.ctx.ChainID,
			requestID,
			summary,
			nodeID,
		)
		go s.router.HandleInbound(ctx, inMsg)
		return
	}

	// Create the outbound message.
	outMsg, err := s.msgCreator.StateSummaryFrontier(
		s.ctx.ChainID,
		requestID,
		summary,
	)
	if err != nil {
		s.ctx.Log.Error("failed to build message",
			zap.Stringer("messageOp", message.StateSummaryFrontierOp),
			zap.Stringer("chainID", s.ctx.ChainID),
			zap.Uint32("requestID", requestID),
			zap.Binary("summaryBytes", summary),
			zap.Error(err),
		)
		return
	}

	// Send the message over the network.
	nodeIDs := set.NewSet[ids.NodeID](1)
	nodeIDs.Add(nodeID)
	sentTo := s.sender.Send(
		outMsg,
		nodeIDs,
		s.ctx.SubnetID,
		s.subnet,
	)
	if sentTo.Len() == 0 {
		s.ctx.Log.Debug("failed to send message",
			zap.Stringer("messageOp", message.StateSummaryFrontierOp),
			zap.Stringer("nodeID", nodeID),
			zap.Stringer("chainID", s.ctx.ChainID),
			zap.Uint32("requestID", requestID),
		)
		s.ctx.Log.Verbo("failed to send message",
			zap.Stringer("messageOp", message.StateSummaryFrontierOp),
			zap.Stringer("nodeID", nodeID),
			zap.Stringer("chainID", s.ctx.ChainID),
			zap.Uint32("requestID", requestID),
			zap.Binary("summary", summary),
		)
	}
}

func (s *sender) SendGetAcceptedStateSummary(ctx context.Context, nodeIDs set.Set[ids.NodeID], requestID uint32, heights []uint64) {
	ctx = utils.Detach(ctx)

	// Note that this timeout duration won't exactly match the one that gets
	// registered. That's OK.
	deadline := s.timeouts.TimeoutDuration()

	// Tell the router to expect a response message or a message notifying
	// that we won't get a response from each of these nodes.
	// We register timeouts for all nodes, regardless of whether we fail
	// to send them a message, to avoid busy looping when disconnected from
	// the internet.
	for nodeID := range nodeIDs {
		inMsg := message.InternalGetAcceptedStateSummaryFailed(
			nodeID,
			s.ctx.ChainID,
			requestID,
		)
		s.router.RegisterRequest(
			ctx,
			nodeID,
			s.ctx.ChainID,
			s.ctx.ChainID,
			requestID,
			message.AcceptedStateSummaryOp,
			inMsg,
			p2p.EngineType_ENGINE_TYPE_UNSPECIFIED,
		)
	}

	// Sending a message to myself. No need to send it over the network.
	// Just put it right into the router. Asynchronously to avoid deadlock.
	if nodeIDs.Contains(s.ctx.NodeID) {
		nodeIDs.Remove(s.ctx.NodeID)
		inMsg := message.InboundGetAcceptedStateSummary(
			s.ctx.ChainID,
			requestID,
			heights,
			deadline,
			s.ctx.NodeID,
		)
		go s.router.HandleInbound(ctx, inMsg)
	}

	// Create the outbound message.
	outMsg, err := s.msgCreator.GetAcceptedStateSummary(
		s.ctx.ChainID,
		requestID,
		deadline,
		heights,
	)

	// Send the message over the network.
	var sentTo set.Set[ids.NodeID]
	if err == nil {
		sentTo = s.sender.Send(
			outMsg,
			nodeIDs,
			s.ctx.SubnetID,
			s.subnet,
		)
	} else {
		s.ctx.Log.Error("failed to build message",
			zap.Stringer("messageOp", message.GetAcceptedStateSummaryOp),
			zap.Stringer("chainID", s.ctx.ChainID),
			zap.Uint32("requestID", requestID),
			zap.Uint64s("heights", heights),
			zap.Error(err),
		)
	}

	for nodeID := range nodeIDs {
		if !sentTo.Contains(nodeID) {
			s.ctx.Log.Debug("failed to send message",
				zap.Stringer("messageOp", message.GetAcceptedStateSummaryOp),
				zap.Stringer("nodeID", nodeID),
				zap.Stringer("chainID", s.ctx.ChainID),
				zap.Uint32("requestID", requestID),
				zap.Uint64s("heights", heights),
			)
		}
	}
}

func (s *sender) SendAcceptedStateSummary(ctx context.Context, nodeID ids.NodeID, requestID uint32, summaryIDs []ids.ID) {
	ctx = utils.Detach(ctx)

	if nodeID == s.ctx.NodeID {
		inMsg := message.InboundAcceptedStateSummary(
			s.ctx.ChainID,
			requestID,
			summaryIDs,
			nodeID,
		)
		go s.router.HandleInbound(ctx, inMsg)
		return
	}

	// Create the outbound message.
	outMsg, err := s.msgCreator.AcceptedStateSummary(
		s.ctx.ChainID,
		requestID,
		summaryIDs,
	)
	if err != nil {
		s.ctx.Log.Error("failed to build message",
			zap.Stringer("messageOp", message.AcceptedStateSummaryOp),
			zap.Stringer("chainID", s.ctx.ChainID),
			zap.Uint32("requestID", requestID),
			zap.Stringers("summaryIDs", summaryIDs),
			zap.Error(err),
		)
		return
	}

	// Send the message over the network.
	nodeIDs := set.NewSet[ids.NodeID](1)
	nodeIDs.Add(nodeID)
	sentTo := s.sender.Send(
		outMsg,
		nodeIDs,
		s.ctx.SubnetID,
		s.subnet,
	)
	if sentTo.Len() == 0 {
		s.ctx.Log.Debug("failed to send message",
			zap.Stringer("messageOp", message.AcceptedStateSummaryOp),
			zap.Stringer("nodeID", nodeID),
			zap.Stringer("chainID", s.ctx.ChainID),
			zap.Uint32("requestID", requestID),
			zap.Stringers("summaryIDs", summaryIDs),
		)
	}
}

func (s *sender) SendGetAcceptedFrontier(ctx context.Context, nodeIDs set.Set[ids.NodeID], requestID uint32) {
	ctx = utils.Detach(ctx)

	// Note that this timeout duration won't exactly match the one that gets
	// registered. That's OK.
	deadline := s.timeouts.TimeoutDuration()

	// Tell the router to expect a response message or a message notifying
	// that we won't get a response from each of these nodes.
	// We register timeouts for all nodes, regardless of whether we fail
	// to send them a message, to avoid busy looping when disconnected from
	// the internet.
	for nodeID := range nodeIDs {
		inMsg := message.InternalGetAcceptedFrontierFailed(
			nodeID,
			s.ctx.ChainID,
			requestID,
			s.engineType,
		)
		s.router.RegisterRequest(
			ctx,
			nodeID,
			s.ctx.ChainID,
			s.ctx.ChainID,
			requestID,
			message.AcceptedFrontierOp,
			inMsg,
			s.engineType,
		)
	}

	// Sending a message to myself. No need to send it over the network.
	// Just put it right into the router. Asynchronously to avoid deadlock.
	if nodeIDs.Contains(s.ctx.NodeID) {
		nodeIDs.Remove(s.ctx.NodeID)
		inMsg := message.InboundGetAcceptedFrontier(
			s.ctx.ChainID,
			requestID,
			deadline,
			s.ctx.NodeID,
			s.engineType,
		)
		go s.router.HandleInbound(ctx, inMsg)
	}

	// Create the outbound message.
	outMsg, err := s.msgCreator.GetAcceptedFrontier(
		s.ctx.ChainID,
		requestID,
		deadline,
		s.engineType,
	)

	// Send the message over the network.
	var sentTo set.Set[ids.NodeID]
	if err == nil {
		sentTo = s.sender.Send(
			outMsg,
			nodeIDs,
			s.ctx.SubnetID,
			s.subnet,
		)
	} else {
		s.ctx.Log.Error("failed to build message",
			zap.Stringer("messageOp", message.GetAcceptedFrontierOp),
			zap.Stringer("chainID", s.ctx.ChainID),
			zap.Uint32("requestID", requestID),
			zap.Duration("deadline", deadline),
			zap.Error(err),
		)
	}

	for nodeID := range nodeIDs {
		if !sentTo.Contains(nodeID) {
			s.ctx.Log.Debug("failed to send message",
				zap.Stringer("messageOp", message.GetAcceptedFrontierOp),
				zap.Stringer("nodeID", nodeID),
				zap.Stringer("chainID", s.ctx.ChainID),
				zap.Uint32("requestID", requestID),
			)
		}
	}
}

func (s *sender) SendAcceptedFrontier(ctx context.Context, nodeID ids.NodeID, requestID uint32, containerIDs []ids.ID) {
	ctx = utils.Detach(ctx)

	// Sending this message to myself.
	if nodeID == s.ctx.NodeID {
		inMsg := message.InboundAcceptedFrontier(
			s.ctx.ChainID,
			requestID,
			containerIDs,
			nodeID,
		)
		go s.router.HandleInbound(ctx, inMsg)
		return
	}

	// Create the outbound message.
	outMsg, err := s.msgCreator.AcceptedFrontier(
		s.ctx.ChainID,
		requestID,
		containerIDs,
	)
	if err != nil {
		s.ctx.Log.Error("failed to build message",
			zap.Stringer("messageOp", message.AcceptedFrontierOp),
			zap.Stringer("chainID", s.ctx.ChainID),
			zap.Uint32("requestID", requestID),
			zap.Stringers("containerIDs", containerIDs),
			zap.Error(err),
		)
		return
	}

	// Send the message over the network.
	nodeIDs := set.NewSet[ids.NodeID](1)
	nodeIDs.Add(nodeID)
	sentTo := s.sender.Send(
		outMsg,
		nodeIDs,
		s.ctx.SubnetID,
		s.subnet,
	)
	if sentTo.Len() == 0 {
		s.ctx.Log.Debug("failed to send message",
			zap.Stringer("messageOp", message.AcceptedFrontierOp),
			zap.Stringer("nodeID", nodeID),
			zap.Stringer("chainID", s.ctx.ChainID),
			zap.Uint32("requestID", requestID),
			zap.Stringers("containerIDs", containerIDs),
		)
	}
}

func (s *sender) SendGetAccepted(ctx context.Context, nodeIDs set.Set[ids.NodeID], requestID uint32, containerIDs []ids.ID) {
	ctx = utils.Detach(ctx)

	// Note that this timeout duration won't exactly match the one that gets
	// registered. That's OK.
	deadline := s.timeouts.TimeoutDuration()

	// Tell the router to expect a response message or a message notifying
	// that we won't get a response from each of these nodes.
	// We register timeouts for all nodes, regardless of whether we fail
	// to send them a message, to avoid busy looping when disconnected from
	// the internet.
	for nodeID := range nodeIDs {
		inMsg := message.InternalGetAcceptedFailed(
			nodeID,
			s.ctx.ChainID,
			requestID,
			s.engineType,
		)
		s.router.RegisterRequest(
			ctx,
			nodeID,
			s.ctx.ChainID,
			s.ctx.ChainID,
			requestID,
			message.AcceptedOp,
			inMsg,
			s.engineType,
		)
	}

	// Sending a message to myself. No need to send it over the network.
	// Just put it right into the router. Asynchronously to avoid deadlock.
	if nodeIDs.Contains(s.ctx.NodeID) {
		nodeIDs.Remove(s.ctx.NodeID)
		inMsg := message.InboundGetAccepted(
			s.ctx.ChainID,
			requestID,
			deadline,
			containerIDs,
			s.ctx.NodeID,
			s.engineType,
		)
		go s.router.HandleInbound(ctx, inMsg)
	}

	// Create the outbound message.
	outMsg, err := s.msgCreator.GetAccepted(
		s.ctx.ChainID,
		requestID,
		deadline,
		containerIDs,
		s.engineType,
	)

	// Send the message over the network.
	var sentTo set.Set[ids.NodeID]
	if err == nil {
		sentTo = s.sender.Send(
			outMsg,
			nodeIDs,
			s.ctx.SubnetID,
			s.subnet,
		)
	} else {
		s.ctx.Log.Error("failed to build message",
			zap.Stringer("messageOp", message.GetAcceptedOp),
			zap.Stringer("chainID", s.ctx.ChainID),
			zap.Uint32("requestID", requestID),
			zap.Stringers("containerIDs", containerIDs),
			zap.Error(err),
		)
	}

	for nodeID := range nodeIDs {
		if !sentTo.Contains(nodeID) {
			s.ctx.Log.Debug("failed to send message",
				zap.Stringer("messageOp", message.GetAcceptedOp),
				zap.Stringer("nodeID", nodeID),
				zap.Stringer("chainID", s.ctx.ChainID),
				zap.Uint32("requestID", requestID),
				zap.Stringers("containerIDs", containerIDs),
			)
		}
	}
}

func (s *sender) SendAccepted(ctx context.Context, nodeID ids.NodeID, requestID uint32, containerIDs []ids.ID) {
	ctx = utils.Detach(ctx)

	if nodeID == s.ctx.NodeID {
		inMsg := message.InboundAccepted(
			s.ctx.ChainID,
			requestID,
			containerIDs,
			nodeID,
		)
		go s.router.HandleInbound(ctx, inMsg)
		return
	}

	// Create the outbound message.
	outMsg, err := s.msgCreator.Accepted(s.ctx.ChainID, requestID, containerIDs)
	if err != nil {
		s.ctx.Log.Error("failed to build message",
			zap.Stringer("messageOp", message.AcceptedOp),
			zap.Stringer("chainID", s.ctx.ChainID),
			zap.Uint32("requestID", requestID),
			zap.Stringers("containerIDs", containerIDs),
			zap.Error(err),
		)
		return
	}

	// Send the message over the network.
	nodeIDs := set.NewSet[ids.NodeID](1)
	nodeIDs.Add(nodeID)
	sentTo := s.sender.Send(
		outMsg,
		nodeIDs,
		s.ctx.SubnetID,
		s.subnet,
	)
	if sentTo.Len() == 0 {
		s.ctx.Log.Debug("failed to send message",
			zap.Stringer("messageOp", message.AcceptedOp),
			zap.Stringer("nodeID", nodeID),
			zap.Stringer("chainID", s.ctx.ChainID),
			zap.Uint32("requestID", requestID),
			zap.Stringers("containerIDs", containerIDs),
		)
	}
}

func (s *sender) SendGetAncestors(ctx context.Context, nodeID ids.NodeID, requestID uint32, containerID ids.ID) {
	ctx = utils.Detach(ctx)

	// Tell the router to expect a response message or a message notifying
	// that we won't get a response from this node.
	inMsg := message.InternalGetAncestorsFailed(
		nodeID,
		s.ctx.ChainID,
		requestID,
		s.engineType,
	)
	s.router.RegisterRequest(
		ctx,
		nodeID,
		s.ctx.ChainID,
		s.ctx.ChainID,
		requestID,
		message.AncestorsOp,
		inMsg,
		s.engineType,
	)

	// Sending a GetAncestors to myself always fails.
	if nodeID == s.ctx.NodeID {
		go s.router.HandleInbound(ctx, inMsg)
		return
	}

	// [nodeID] may be benched. That is, they've been unresponsive so we don't
	// even bother sending requests to them. We just have them immediately fail.
	if s.timeouts.IsBenched(nodeID, s.ctx.ChainID) {
		s.failedDueToBench[message.GetAncestorsOp].Inc() // update metric
		s.timeouts.RegisterRequestToUnreachableValidator()
		go s.router.HandleInbound(ctx, inMsg)
		return
	}

	// Note that this timeout duration won't exactly match the one that gets
	// registered. That's OK.
	deadline := s.timeouts.TimeoutDuration()
	// Create the outbound message.
	outMsg, err := s.msgCreator.GetAncestors(
		s.ctx.ChainID,
		requestID,
		deadline,
		containerID,
		s.engineType,
	)
	if err != nil {
		s.ctx.Log.Error("failed to build message",
			zap.Stringer("messageOp", message.GetAncestorsOp),
			zap.Stringer("chainID", s.ctx.ChainID),
			zap.Uint32("requestID", requestID),
			zap.Stringer("containerID", containerID),
			zap.Error(err),
		)

		go s.router.HandleInbound(ctx, inMsg)
		return
	}

	// Send the message over the network.
	nodeIDs := set.NewSet[ids.NodeID](1)
	nodeIDs.Add(nodeID)
	sentTo := s.sender.Send(
		outMsg,
		nodeIDs,
		s.ctx.SubnetID,
		s.subnet,
	)
	if sentTo.Len() == 0 {
		s.ctx.Log.Debug("failed to send message",
			zap.Stringer("messageOp", message.GetAncestorsOp),
			zap.Stringer("nodeID", nodeID),
			zap.Stringer("chainID", s.ctx.ChainID),
			zap.Uint32("requestID", requestID),
			zap.Stringer("containerID", containerID),
		)

		s.timeouts.RegisterRequestToUnreachableValidator()
		go s.router.HandleInbound(ctx, inMsg)
	}
}

// SendAncestors sends an Ancestors message to the consensus engine running on
// the specified chain on the specified node.
// The Ancestors message gives the recipient the contents of several containers.
func (s *sender) SendAncestors(_ context.Context, nodeID ids.NodeID, requestID uint32, containers [][]byte) {
	// Create the outbound message.
	outMsg, err := s.msgCreator.Ancestors(s.ctx.ChainID, requestID, containers)
	if err != nil {
		s.ctx.Log.Error("failed to build message",
			zap.Stringer("messageOp", message.AncestorsOp),
			zap.Stringer("chainID", s.ctx.ChainID),
			zap.Uint32("requestID", requestID),
			zap.Int("numContainers", len(containers)),
			zap.Error(err),
		)
		return
	}

	// Send the message over the network.
	nodeIDs := set.NewSet[ids.NodeID](1)
	nodeIDs.Add(nodeID)
	sentTo := s.sender.Send(
		outMsg,
		nodeIDs,
		s.ctx.SubnetID,
		s.subnet,
	)
	if sentTo.Len() == 0 {
		s.ctx.Log.Debug("failed to send message",
			zap.Stringer("messageOp", message.AncestorsOp),
			zap.Stringer("nodeID", nodeID),
			zap.Stringer("chainID", s.ctx.ChainID),
			zap.Uint32("requestID", requestID),
			zap.Int("numContainers", len(containers)),
		)
	}
}

// SendGet sends a Get message to the consensus engine running on the specified
// chain to the specified node. The Get message signifies that this
// consensus engine would like the recipient to send this consensus engine the
// specified container.
func (s *sender) SendGet(ctx context.Context, nodeID ids.NodeID, requestID uint32, containerID ids.ID) {
	ctx = utils.Detach(ctx)

	// Tell the router to expect a response message or a message notifying
	// that we won't get a response from this node.
	inMsg := message.InternalGetFailed(
		nodeID,
		s.ctx.ChainID,
		requestID,
		s.engineType,
	)
	s.router.RegisterRequest(
		ctx,
		nodeID,
		s.ctx.ChainID,
		s.ctx.ChainID,
		requestID,
		message.PutOp,
		inMsg,
		s.engineType,
	)

	// Sending a Get to myself always fails.
	if nodeID == s.ctx.NodeID {
		go s.router.HandleInbound(ctx, inMsg)
		return
	}

	// [nodeID] may be benched. That is, they've been unresponsive so we don't
	// even bother sending requests to them. We just have them immediately fail.
	if s.timeouts.IsBenched(nodeID, s.ctx.ChainID) {
		s.failedDueToBench[message.GetOp].Inc() // update metric
		s.timeouts.RegisterRequestToUnreachableValidator()
		go s.router.HandleInbound(ctx, inMsg)
		return
	}

	// Note that this timeout duration won't exactly match the one that gets
	// registered. That's OK.
	deadline := s.timeouts.TimeoutDuration()
	// Create the outbound message.
	outMsg, err := s.msgCreator.Get(
		s.ctx.ChainID,
		requestID,
		deadline,
		containerID,
		s.engineType,
	)

	// Send the message over the network.
	var sentTo set.Set[ids.NodeID]
	if err == nil {
		nodeIDs := set.NewSet[ids.NodeID](1)
		nodeIDs.Add(nodeID)
		sentTo = s.sender.Send(
			outMsg,
			nodeIDs,
			s.ctx.SubnetID,
			s.subnet,
		)
	} else {
		s.ctx.Log.Error("failed to build message",
			zap.Stringer("messageOp", message.GetOp),
			zap.Stringer("chainID", s.ctx.ChainID),
			zap.Uint32("requestID", requestID),
			zap.Duration("deadline", deadline),
			zap.Stringer("containerID", containerID),
			zap.Error(err),
		)
	}

	if sentTo.Len() == 0 {
		s.ctx.Log.Debug("failed to send message",
			zap.Stringer("messageOp", message.GetOp),
			zap.Stringer("nodeID", nodeID),
			zap.Stringer("chainID", s.ctx.ChainID),
			zap.Uint32("requestID", requestID),
			zap.Stringer("containerID", containerID),
		)

		s.timeouts.RegisterRequestToUnreachableValidator()
		go s.router.HandleInbound(ctx, inMsg)
	}
}

// SendPut sends a Put message to the consensus engine running on the specified
// chain on the specified node.
// The Put message signifies that this consensus engine is giving to the
// recipient the contents of the specified container.
func (s *sender) SendPut(_ context.Context, nodeID ids.NodeID, requestID uint32, container []byte) {
	// Create the outbound message.
	outMsg, err := s.msgCreator.Put(s.ctx.ChainID, requestID, container, s.engineType)
	if err != nil {
		s.ctx.Log.Error("failed to build message",
			zap.Stringer("messageOp", message.PutOp),
			zap.Stringer("chainID", s.ctx.ChainID),
			zap.Uint32("requestID", requestID),
			zap.Binary("container", container),
			zap.Error(err),
		)
		return
	}

	// Send the message over the network.
	nodeIDs := set.NewSet[ids.NodeID](1)
	nodeIDs.Add(nodeID)
	sentTo := s.sender.Send(
		outMsg,
		nodeIDs,
		s.ctx.SubnetID,
		s.subnet,
	)
	if sentTo.Len() == 0 {
		s.ctx.Log.Debug("failed to send message",
			zap.Stringer("messageOp", message.PutOp),
			zap.Stringer("nodeID", nodeID),
			zap.Stringer("chainID", s.ctx.ChainID),
			zap.Uint32("requestID", requestID),
		)
		s.ctx.Log.Verbo("failed to send message",
			zap.Stringer("messageOp", message.PutOp),
			zap.Stringer("nodeID", nodeID),
			zap.Stringer("chainID", s.ctx.ChainID),
			zap.Uint32("requestID", requestID),
			zap.Binary("container", container),
		)
	}
}

// SendPushQuery sends a PushQuery message to the consensus engines running on
// the specified chains on the specified nodes.
// The PushQuery message signifies that this consensus engine would like each
// node to send their preferred frontier given the existence of the specified
// container.
func (s *sender) SendPushQuery(ctx context.Context, nodeIDs set.Set[ids.NodeID], requestID uint32, container []byte) {
	ctx = utils.Detach(ctx)

	// Tell the router to expect a response message or a message notifying
	// that we won't get a response from each of these nodes.
	// We register timeouts for all nodes, regardless of whether we fail
	// to send them a message, to avoid busy looping when disconnected from
	// the internet.
	for nodeID := range nodeIDs {
		inMsg := message.InternalQueryFailed(
			nodeID,
			s.ctx.ChainID,
			requestID,
			s.engineType,
		)
		s.router.RegisterRequest(
			ctx,
			nodeID,
			s.ctx.ChainID,
			s.ctx.ChainID,
			requestID,
			message.ChitsOp,
			inMsg,
			s.engineType,
		)
	}

	// Note that this timeout duration won't exactly match the one that gets
	// registered. That's OK.
	deadline := s.timeouts.TimeoutDuration()

	// Sending a message to myself. No need to send it over the network. Just
	// put it right into the router. Do so asynchronously to avoid deadlock.
	if nodeIDs.Contains(s.ctx.NodeID) {
		nodeIDs.Remove(s.ctx.NodeID)
		inMsg := message.InboundPushQuery(
			s.ctx.ChainID,
			requestID,
			deadline,
			container,
			s.ctx.NodeID,
			s.engineType,
		)
		go s.router.HandleInbound(ctx, inMsg)
	}

	// Some of [nodeIDs] may be benched. That is, they've been unresponsive so
	// we don't even bother sending messages to them. We just have them
	// immediately fail.
	for nodeID := range nodeIDs {
		if s.timeouts.IsBenched(nodeID, s.ctx.ChainID) {
			s.failedDueToBench[message.PushQueryOp].Inc() // update metric
			nodeIDs.Remove(nodeID)
			s.timeouts.RegisterRequestToUnreachableValidator()

			// Immediately register a failure. Do so asynchronously to avoid
			// deadlock.
			inMsg := message.InternalQueryFailed(
				nodeID,
				s.ctx.ChainID,
				requestID,
				s.engineType,
			)
			go s.router.HandleInbound(ctx, inMsg)
		}
	}

	// Create the outbound message.
	outMsg, err := s.msgCreator.PushQuery(
		s.ctx.ChainID,
		requestID,
		deadline,
		container,
		s.engineType,
	)

	// Send the message over the network.
	// [sentTo] are the IDs of validators who may receive the message.
	var sentTo set.Set[ids.NodeID]
	if err == nil {
		sentTo = s.sender.Send(
			outMsg,
			nodeIDs,
			s.ctx.SubnetID,
			s.subnet,
		)
	} else {
		s.ctx.Log.Error("failed to build message",
			zap.Stringer("messageOp", message.PushQueryOp),
			zap.Stringer("chainID", s.ctx.ChainID),
			zap.Uint32("requestID", requestID),
			zap.Binary("container", container),
			zap.Error(err),
		)
	}

	for nodeID := range nodeIDs {
		if !sentTo.Contains(nodeID) {
			s.ctx.Log.Debug("failed to send message",
				zap.Stringer("messageOp", message.PushQueryOp),
				zap.Stringer("nodeID", nodeID),
				zap.Stringer("chainID", s.ctx.ChainID),
				zap.Uint32("requestID", requestID),
			)
			s.ctx.Log.Verbo("failed to send message",
				zap.Stringer("messageOp", message.PushQueryOp),
				zap.Stringer("nodeID", nodeID),
				zap.Stringer("chainID", s.ctx.ChainID),
				zap.Uint32("requestID", requestID),
				zap.Binary("container", container),
			)

			// Register failures for nodes we didn't send a request to.
			s.timeouts.RegisterRequestToUnreachableValidator()
			inMsg := message.InternalQueryFailed(
				nodeID,
				s.ctx.ChainID,
				requestID,
				s.engineType,
			)
			go s.router.HandleInbound(ctx, inMsg)
		}
	}
}

// SendPullQuery sends a PullQuery message to the consensus engines running on
// the specified chains on the specified nodes.
// The PullQuery message signifies that this consensus engine would like each
// node to send their preferred frontier.
func (s *sender) SendPullQuery(ctx context.Context, nodeIDs set.Set[ids.NodeID], requestID uint32, containerID ids.ID) {
	ctx = utils.Detach(ctx)

	// Tell the router to expect a response message or a message notifying
	// that we won't get a response from each of these nodes.
	// We register timeouts for all nodes, regardless of whether we fail
	// to send them a message, to avoid busy looping when disconnected from
	// the internet.
	for nodeID := range nodeIDs {
		inMsg := message.InternalQueryFailed(
			nodeID,
			s.ctx.ChainID,
			requestID,
			s.engineType,
		)
		s.router.RegisterRequest(
			ctx,
			nodeID,
			s.ctx.ChainID,
			s.ctx.ChainID,
			requestID,
			message.ChitsOp,
			inMsg,
			s.engineType,
		)
	}

	// Note that this timeout duration won't exactly match the one that gets
	// registered. That's OK.
	deadline := s.timeouts.TimeoutDuration()

	// Sending a message to myself. No need to send it over the network. Just
	// put it right into the router. Do so asynchronously to avoid deadlock.
	if nodeIDs.Contains(s.ctx.NodeID) {
		nodeIDs.Remove(s.ctx.NodeID)
		inMsg := message.InboundPullQuery(
			s.ctx.ChainID,
			requestID,
			deadline,
			containerID,
			s.ctx.NodeID,
			s.engineType,
		)
		go s.router.HandleInbound(ctx, inMsg)
	}

	// Some of the nodes in [nodeIDs] may be benched. That is, they've been
	// unresponsive so we don't even bother sending messages to them. We just
	// have them immediately fail.
	for nodeID := range nodeIDs {
		if s.timeouts.IsBenched(nodeID, s.ctx.ChainID) {
			s.failedDueToBench[message.PullQueryOp].Inc() // update metric
			nodeIDs.Remove(nodeID)
			s.timeouts.RegisterRequestToUnreachableValidator()
			// Immediately register a failure. Do so asynchronously to avoid
			// deadlock.
			inMsg := message.InternalQueryFailed(
				nodeID,
				s.ctx.ChainID,
				requestID,
				s.engineType,
			)
			go s.router.HandleInbound(ctx, inMsg)
		}
	}

	// Create the outbound message.
	outMsg, err := s.msgCreator.PullQuery(
		s.ctx.ChainID,
		requestID,
		deadline,
		containerID,
		s.engineType,
	)

	// Send the message over the network.
	var sentTo set.Set[ids.NodeID]
	if err == nil {
		sentTo = s.sender.Send(
			outMsg,
			nodeIDs,
			s.ctx.SubnetID,
			s.subnet,
		)
	} else {
		s.ctx.Log.Error("failed to build message",
			zap.Stringer("messageOp", message.PullQueryOp),
			zap.Stringer("chainID", s.ctx.ChainID),
			zap.Uint32("requestID", requestID),
			zap.Duration("deadline", deadline),
			zap.Stringer("containerID", containerID),
			zap.Error(err),
		)
	}

	for nodeID := range nodeIDs {
		if !sentTo.Contains(nodeID) {
			s.ctx.Log.Debug("failed to send message",
				zap.Stringer("messageOp", message.PullQueryOp),
				zap.Stringer("nodeID", nodeID),
				zap.Stringer("chainID", s.ctx.ChainID),
				zap.Uint32("requestID", requestID),
				zap.Stringer("containerID", containerID),
			)

			// Register failures for nodes we didn't send a request to.
			s.timeouts.RegisterRequestToUnreachableValidator()
			inMsg := message.InternalQueryFailed(
				nodeID,
				s.ctx.ChainID,
				requestID,
				s.engineType,
			)
			go s.router.HandleInbound(ctx, inMsg)
		}
	}
}

// SendChits sends chits
func (s *sender) SendChits(ctx context.Context, nodeID ids.NodeID, requestID uint32, votes, accepted []ids.ID) {
	ctx = utils.Detach(ctx)

	// If [nodeID] is myself, send this message directly
	// to my own router rather than sending it over the network
	if nodeID == s.ctx.NodeID {
		inMsg := message.InboundChits(
			s.ctx.ChainID,
			requestID,
			votes,
			accepted,
			nodeID,
		)
		go s.router.HandleInbound(ctx, inMsg)
		return
	}

	// Create the outbound message.
	outMsg, err := s.msgCreator.Chits(s.ctx.ChainID, requestID, votes, accepted)
	if err != nil {
		s.ctx.Log.Error("failed to build message",
			zap.Stringer("messageOp", message.ChitsOp),
			zap.Stringer("chainID", s.ctx.ChainID),
			zap.Uint32("requestID", requestID),
			zap.Stringers("containerIDs", votes),
			zap.Error(err),
		)
		return
	}

	// Send the message over the network.
	nodeIDs := set.NewSet[ids.NodeID](1)
	nodeIDs.Add(nodeID)
	sentTo := s.sender.Send(
		outMsg,
		nodeIDs,
		s.ctx.SubnetID,
		s.subnet,
	)
	if sentTo.Len() == 0 {
		s.ctx.Log.Debug("failed to send message",
			zap.Stringer("messageOp", message.ChitsOp),
			zap.Stringer("nodeID", nodeID),
			zap.Stringer("chainID", s.ctx.ChainID),
			zap.Uint32("requestID", requestID),
			zap.Stringers("containerIDs", votes),
		)
	}
}

func (s *sender) SendCrossChainAppRequest(ctx context.Context, chainID ids.ID, requestID uint32, appRequestBytes []byte) error {
	ctx = utils.Detach(ctx)

	// The failed message is treated as if it was sent by the requested chain
	failedMsg := message.InternalCrossChainAppRequestFailed(
		s.ctx.NodeID,
		chainID,
		s.ctx.ChainID,
		requestID,
	)
	s.router.RegisterRequest(
		ctx,
		s.ctx.NodeID,
		s.ctx.ChainID,
		chainID,
		requestID,
		message.CrossChainAppResponseOp,
		failedMsg,
		p2p.EngineType_ENGINE_TYPE_UNSPECIFIED,
	)

	inMsg := message.InternalCrossChainAppRequest(
		s.ctx.NodeID,
		s.ctx.ChainID,
		chainID,
		requestID,
		s.timeouts.TimeoutDuration(),
		appRequestBytes,
	)
	go s.router.HandleInbound(ctx, inMsg)
	return nil
}

func (s *sender) SendCrossChainAppResponse(ctx context.Context, chainID ids.ID, requestID uint32, appResponseBytes []byte) error {
	ctx = utils.Detach(ctx)

	inMsg := message.InternalCrossChainAppResponse(
		s.ctx.NodeID,
		s.ctx.ChainID,
		chainID,
		requestID,
		appResponseBytes,
	)
	go s.router.HandleInbound(ctx, inMsg)
	return nil
}

// SendAppRequest sends an application-level request to the given nodes.
// The meaning of this request, and how it should be handled, is defined by the
// VM.
func (s *sender) SendAppRequest(ctx context.Context, nodeIDs set.Set[ids.NodeID], requestID uint32, appRequestBytes []byte) error {
	ctx = utils.Detach(ctx)

	// Tell the router to expect a response message or a message notifying
	// that we won't get a response from each of these nodes.
	// We register timeouts for all nodes, regardless of whether we fail
	// to send them a message, to avoid busy looping when disconnected from
	// the internet.
	for nodeID := range nodeIDs {
		inMsg := message.InternalAppRequestFailed(
			nodeID,
			s.ctx.ChainID,
			requestID,
		)
		s.router.RegisterRequest(
			ctx,
			nodeID,
			s.ctx.ChainID,
			s.ctx.ChainID,
			requestID,
			message.AppResponseOp,
			inMsg,
			p2p.EngineType_ENGINE_TYPE_UNSPECIFIED,
		)
	}

	// Note that this timeout duration won't exactly match the one that gets
	// registered. That's OK.
	deadline := s.timeouts.TimeoutDuration()

	// Sending a message to myself. No need to send it over the network. Just
	// put it right into the router. Do so asynchronously to avoid deadlock.
	if nodeIDs.Contains(s.ctx.NodeID) {
		nodeIDs.Remove(s.ctx.NodeID)
		inMsg := message.InboundAppRequest(
			s.ctx.ChainID,
			requestID,
			deadline,
			appRequestBytes,
			s.ctx.NodeID,
		)
		go s.router.HandleInbound(ctx, inMsg)
	}

	// Some of the nodes in [nodeIDs] may be benched. That is, they've been
	// unresponsive so we don't even bother sending messages to them. We just
	// have them immediately fail.
	for nodeID := range nodeIDs {
		if s.timeouts.IsBenched(nodeID, s.ctx.ChainID) {
			s.failedDueToBench[message.AppRequestOp].Inc() // update metric
			nodeIDs.Remove(nodeID)
			s.timeouts.RegisterRequestToUnreachableValidator()

			// Immediately register a failure. Do so asynchronously to avoid
			// deadlock.
			inMsg := message.InternalAppRequestFailed(
				nodeID,
				s.ctx.ChainID,
				requestID,
			)
			go s.router.HandleInbound(ctx, inMsg)
		}
	}

	// Create the outbound message.
	outMsg, err := s.msgCreator.AppRequest(
		s.ctx.ChainID,
		requestID,
		deadline,
		appRequestBytes,
	)

	// Send the message over the network.
	// [sentTo] are the IDs of nodes who may receive the message.
	var sentTo set.Set[ids.NodeID]
	if err == nil {
		sentTo = s.sender.Send(
			outMsg,
			nodeIDs,
			s.ctx.SubnetID,
			s.subnet,
		)
	} else {
		s.ctx.Log.Error("failed to build message",
			zap.Stringer("messageOp", message.AppRequestOp),
			zap.Stringer("chainID", s.ctx.ChainID),
			zap.Uint32("requestID", requestID),
			zap.Binary("payload", appRequestBytes),
			zap.Error(err),
		)
	}

	for nodeID := range nodeIDs {
		if !sentTo.Contains(nodeID) {
			s.ctx.Log.Debug("failed to send message",
				zap.Stringer("messageOp", message.AppRequestOp),
				zap.Stringer("nodeID", nodeID),
				zap.Stringer("chainID", s.ctx.ChainID),
				zap.Uint32("requestID", requestID),
			)
			s.ctx.Log.Verbo("failed to send message",
				zap.Stringer("messageOp", message.AppRequestOp),
				zap.Stringer("nodeID", nodeID),
				zap.Stringer("chainID", s.ctx.ChainID),
				zap.Uint32("requestID", requestID),
				zap.Binary("payload", appRequestBytes),
			)

			// Register failures for nodes we didn't send a request to.
			s.timeouts.RegisterRequestToUnreachableValidator()
			inMsg := message.InternalAppRequestFailed(
				nodeID,
				s.ctx.ChainID,
				requestID,
			)
			go s.router.HandleInbound(ctx, inMsg)
		}
	}
	return nil
}

// SendAppResponse sends a response to an application-level request from the
// given node
func (s *sender) SendAppResponse(ctx context.Context, nodeID ids.NodeID, requestID uint32, appResponseBytes []byte) error {
	ctx = utils.Detach(ctx)

	if nodeID == s.ctx.NodeID {
		inMsg := message.InboundAppResponse(
			s.ctx.ChainID,
			requestID,
			appResponseBytes,
			nodeID,
		)
		go s.router.HandleInbound(ctx, inMsg)
		return nil
	}

	// Create the outbound message.
	outMsg, err := s.msgCreator.AppResponse(
		s.ctx.ChainID,
		requestID,
		appResponseBytes,
	)
	if err != nil {
		s.ctx.Log.Error("failed to build message",
			zap.Stringer("messageOp", message.AppResponseOp),
			zap.Stringer("chainID", s.ctx.ChainID),
			zap.Uint32("requestID", requestID),
			zap.Binary("payload", appResponseBytes),
			zap.Error(err),
		)
		return nil
	}

	// Send the message over the network.
	nodeIDs := set.NewSet[ids.NodeID](1)
	nodeIDs.Add(nodeID)
	sentTo := s.sender.Send(
		outMsg,
		nodeIDs,
		s.ctx.SubnetID,
		s.subnet,
	)
	if sentTo.Len() == 0 {
		s.ctx.Log.Debug("failed to send message",
			zap.Stringer("messageOp", message.AppResponseOp),
			zap.Stringer("nodeID", nodeID),
			zap.Stringer("chainID", s.ctx.ChainID),
			zap.Uint32("requestID", requestID),
		)
		s.ctx.Log.Verbo("failed to send message",
			zap.Stringer("messageOp", message.AppResponseOp),
			zap.Stringer("nodeID", nodeID),
			zap.Stringer("chainID", s.ctx.ChainID),
			zap.Uint32("requestID", requestID),
			zap.Binary("payload", appResponseBytes),
		)
	}
	return nil
}

func (s *sender) SendAppGossipSpecific(_ context.Context, nodeIDs set.Set[ids.NodeID], appGossipBytes []byte) error {
	// Create the outbound message.
	outMsg, err := s.msgCreator.AppGossip(s.ctx.ChainID, appGossipBytes)
	if err != nil {
		s.ctx.Log.Error("failed to build message",
			zap.Stringer("messageOp", message.AppGossipOp),
			zap.Stringer("chainID", s.ctx.ChainID),
			zap.Binary("payload", appGossipBytes),
			zap.Error(err),
		)
		return nil
	}

	// Send the message over the network.
	sentTo := s.sender.Send(
		outMsg,
		nodeIDs,
		s.ctx.SubnetID,
		s.subnet,
	)
	if sentTo.Len() == 0 {
		for nodeID := range nodeIDs {
			if !sentTo.Contains(nodeID) {
				s.ctx.Log.Debug("failed to send message",
					zap.Stringer("messageOp", message.AppGossipOp),
					zap.Stringer("nodeID", nodeID),
					zap.Stringer("chainID", s.ctx.ChainID),
				)
				s.ctx.Log.Verbo("failed to send message",
					zap.Stringer("messageOp", message.AppGossipOp),
					zap.Stringer("nodeID", nodeID),
					zap.Stringer("chainID", s.ctx.ChainID),
					zap.Binary("payload", appGossipBytes),
				)
			}
		}
	}
	return nil
}

// SendAppGossip sends an application-level gossip message.
func (s *sender) SendAppGossip(_ context.Context, appGossipBytes []byte) error {
	// Create the outbound message.
	outMsg, err := s.msgCreator.AppGossip(s.ctx.ChainID, appGossipBytes)
	if err != nil {
		s.ctx.Log.Error("failed to build message",
			zap.Stringer("messageOp", message.AppGossipOp),
			zap.Stringer("chainID", s.ctx.ChainID),
			zap.Binary("payload", appGossipBytes),
			zap.Error(err),
		)
		return nil
	}

	gossipConfig := s.subnet.Config().GossipConfig
	validatorSize := int(gossipConfig.AppGossipValidatorSize)
	nonValidatorSize := int(gossipConfig.AppGossipNonValidatorSize)
	peerSize := int(gossipConfig.AppGossipPeerSize)

	sentTo := s.sender.Gossip(
		outMsg,
		s.ctx.SubnetID,
		validatorSize,
		nonValidatorSize,
		peerSize,
		s.subnet,
	)
	if sentTo.Len() == 0 {
		s.ctx.Log.Debug("failed to send message",
			zap.Stringer("messageOp", message.AppGossipOp),
			zap.Stringer("chainID", s.ctx.ChainID),
		)
		s.ctx.Log.Verbo("failed to send message",
			zap.Stringer("messageOp", message.AppGossipOp),
			zap.Stringer("chainID", s.ctx.ChainID),
			zap.Binary("payload", appGossipBytes),
		)
	}
	return nil
}

// SendGossip gossips the provided container
func (s *sender) SendGossip(_ context.Context, container []byte) {
	// Create the outbound message.
	outMsg, err := s.msgCreator.Put(
		s.ctx.ChainID,
		constants.GossipMsgRequestID,
		container,
		s.engineType,
	)
	if err != nil {
		s.ctx.Log.Error("failed to build message",
			zap.Stringer("messageOp", message.PutOp),
			zap.Stringer("chainID", s.ctx.ChainID),
			zap.Binary("container", container),
			zap.Error(err),
		)
		return
	}

	gossipConfig := s.subnet.Config().GossipConfig
	sentTo := s.sender.Gossip(
		outMsg,
		s.ctx.SubnetID,
		int(gossipConfig.AcceptedFrontierValidatorSize),
		int(gossipConfig.AcceptedFrontierNonValidatorSize),
		int(gossipConfig.AcceptedFrontierPeerSize),
		s.subnet,
	)
	if sentTo.Len() == 0 {
		s.ctx.Log.Debug("failed to send message",
			zap.Stringer("messageOp", message.PutOp),
			zap.Stringer("chainID", s.ctx.ChainID),
		)
		s.ctx.Log.Verbo("failed to send message",
			zap.Stringer("messageOp", message.PutOp),
			zap.Stringer("chainID", s.ctx.ChainID),
			zap.Binary("container", container),
		)
	}
}

// Accept is called after every consensus decision
func (s *sender) Accept(ctx *snow.ConsensusContext, _ ids.ID, container []byte) error {
	if ctx.State.Get().State != snow.NormalOp {
		// don't gossip during bootstrapping
		return nil
	}

	// Create the outbound message.
	outMsg, err := s.msgCreator.Put(
		s.ctx.ChainID,
		constants.GossipMsgRequestID,
		container,
		s.engineType,
	)
	if err != nil {
		s.ctx.Log.Error("failed to build message",
			zap.Stringer("messageOp", message.PutOp),
			zap.Stringer("chainID", s.ctx.ChainID),
			zap.Binary("container", container),
			zap.Error(err),
		)
		return nil
	}

	gossipConfig := s.subnet.Config().GossipConfig
	sentTo := s.sender.Gossip(
		outMsg,
		s.ctx.SubnetID,
		int(gossipConfig.OnAcceptValidatorSize),
		int(gossipConfig.OnAcceptNonValidatorSize),
		int(gossipConfig.OnAcceptPeerSize),
		s.subnet,
	)
	if sentTo.Len() == 0 {
		s.ctx.Log.Debug("failed to send message",
			zap.Stringer("messageOp", message.PutOp),
			zap.Stringer("chainID", s.ctx.ChainID),
		)
		s.ctx.Log.Verbo("failed to send message",
			zap.Stringer("messageOp", message.PutOp),
			zap.Stringer("chainID", s.ctx.ChainID),
			zap.Binary("container", container),
		)
	}
	return nil
}

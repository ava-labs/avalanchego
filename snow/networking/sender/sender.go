// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sender

import (
	"context"

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
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/set"
)

const opLabel = "op"

var (
	_ common.Sender = (*sender)(nil)

	opLabels = []string{opLabel}
)

// sender is a wrapper around an ExternalSender.
// Messages to this node are put directly into [router] rather than
// being sent over the network via the wrapped ExternalSender.
// sender registers outbound requests with [router] so that [router]
// fires a timeout if we don't get a response to the request.
type sender struct {
	ctx        *snow.ConsensusContext
	msgCreator message.OutboundMsgBuilder

	sender   ExternalSender // Actually does the sending over the network
	router   router.InternalHandler
	timeouts timeout.Manager

	// Counts how many request have failed because the node was benched
	failedDueToBench *prometheus.CounterVec // op

	engineType p2p.EngineType
	subnet     subnets.Subnet
}

func New(
	ctx *snow.ConsensusContext,
	msgCreator message.OutboundMsgBuilder,
	externalSender ExternalSender,
	router router.InternalHandler,
	timeouts timeout.Manager,
	engineType p2p.EngineType,
	subnet subnets.Subnet,
	reg prometheus.Registerer,
) (common.Sender, error) {
	s := &sender{
		ctx:        ctx,
		msgCreator: msgCreator,
		sender:     externalSender,
		router:     router,
		timeouts:   timeouts,
		failedDueToBench: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "failed_benched",
				Help: "requests dropped because a node was benched",
			},
			opLabels,
		),
		engineType: engineType,
		subnet:     subnet,
	}
	return s, reg.Register(s.failedDueToBench)
}

func (s *sender) SendGetStateSummaryFrontier(ctx context.Context, nodeIDs set.Set[ids.NodeID], requestID uint32) {
	ctx = context.WithoutCancel(ctx)

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
		s.router.HandleInternal(ctx, inMsg)
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
			common.SendConfig{
				NodeIDs: nodeIDs,
			},
			s.ctx.SubnetID,
			s.subnet,
		)
	} else {
		s.ctx.Log.Error("failed to build message",
			zap.Stringer("messageOp", message.GetStateSummaryFrontierOp),
			zap.Uint32("requestID", requestID),
			zap.Duration("deadline", deadline),
			zap.Error(err),
		)
	}

	s.ctx.Log.Debug("sent message",
		zap.Stringer("messageOp", message.GetStateSummaryFrontierOp),
		zap.Stringers("nodeIDs", sentTo.List()),
		zap.Uint32("requestID", requestID),
	)

	for nodeID := range nodeIDs {
		if !sentTo.Contains(nodeID) {
			s.ctx.Log.Debug("failed to send message",
				zap.Stringer("messageOp", message.GetStateSummaryFrontierOp),
				zap.Stringer("nodeID", nodeID),
				zap.Uint32("requestID", requestID),
			)
		}
	}
}

func (s *sender) SendStateSummaryFrontier(ctx context.Context, nodeID ids.NodeID, requestID uint32, summary []byte) {
	ctx = context.WithoutCancel(ctx)

	// Sending this message to myself.
	if nodeID == s.ctx.NodeID {
		inMsg := message.InboundStateSummaryFrontier(
			s.ctx.ChainID,
			requestID,
			summary,
			nodeID,
		)
		s.router.HandleInternal(ctx, inMsg)
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
			zap.Uint32("requestID", requestID),
			zap.Binary("summaryBytes", summary),
			zap.Error(err),
		)
		return
	}

	// Send the message over the network.
	nodeIDs := set.Of(nodeID)
	sentTo := s.sender.Send(
		outMsg,
		common.SendConfig{
			NodeIDs: nodeIDs,
		},
		s.ctx.SubnetID,
		s.subnet,
	)
	if sentTo.Len() == 0 {
		if s.ctx.Log.Enabled(logging.Verbo) {
			s.ctx.Log.Verbo("failed to send message",
				zap.Stringer("messageOp", message.StateSummaryFrontierOp),
				zap.Stringer("nodeID", nodeID),
				zap.Uint32("requestID", requestID),
				zap.Binary("summary", summary),
			)
		} else {
			s.ctx.Log.Debug("failed to send message",
				zap.Stringer("messageOp", message.StateSummaryFrontierOp),
				zap.Stringer("nodeID", nodeID),
				zap.Uint32("requestID", requestID),
			)
		}
	} else {
		s.ctx.Log.Debug("sent message",
			zap.Stringer("messageOp", message.StateSummaryFrontierOp),
			zap.Stringer("nodeID", nodeID),
			zap.Uint32("requestID", requestID),
		)
	}
}

func (s *sender) SendGetAcceptedStateSummary(ctx context.Context, nodeIDs set.Set[ids.NodeID], requestID uint32, heights []uint64) {
	ctx = context.WithoutCancel(ctx)

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
		s.router.HandleInternal(ctx, inMsg)
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
			common.SendConfig{
				NodeIDs: nodeIDs,
			},
			s.ctx.SubnetID,
			s.subnet,
		)
	} else {
		s.ctx.Log.Error("failed to build message",
			zap.Stringer("messageOp", message.GetAcceptedStateSummaryOp),
			zap.Uint32("requestID", requestID),
			zap.Uint64s("heights", heights),
			zap.Error(err),
		)
	}

	s.ctx.Log.Debug("sent message",
		zap.Stringer("messageOp", message.GetAcceptedStateSummaryOp),
		zap.Stringers("nodeIDs", sentTo.List()),
		zap.Uint32("requestID", requestID),
		zap.Uint64s("heights", heights),
	)

	for nodeID := range nodeIDs {
		if !sentTo.Contains(nodeID) {
			s.ctx.Log.Debug("failed to send message",
				zap.Stringer("messageOp", message.GetAcceptedStateSummaryOp),
				zap.Stringer("nodeID", nodeID),
				zap.Uint32("requestID", requestID),
				zap.Uint64s("heights", heights),
			)
		}
	}
}

func (s *sender) SendAcceptedStateSummary(ctx context.Context, nodeID ids.NodeID, requestID uint32, summaryIDs []ids.ID) {
	ctx = context.WithoutCancel(ctx)

	if nodeID == s.ctx.NodeID {
		inMsg := message.InboundAcceptedStateSummary(
			s.ctx.ChainID,
			requestID,
			summaryIDs,
			nodeID,
		)
		s.router.HandleInternal(ctx, inMsg)
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
			zap.Uint32("requestID", requestID),
			zap.Stringers("summaryIDs", summaryIDs),
			zap.Error(err),
		)
		return
	}

	// Send the message over the network.
	nodeIDs := set.Of(nodeID)
	sentTo := s.sender.Send(
		outMsg,
		common.SendConfig{
			NodeIDs: nodeIDs,
		},
		s.ctx.SubnetID,
		s.subnet,
	)
	if sentTo.Len() == 0 {
		s.ctx.Log.Debug("failed to send message",
			zap.Stringer("messageOp", message.AcceptedStateSummaryOp),
			zap.Stringer("nodeID", nodeID),
			zap.Uint32("requestID", requestID),
			zap.Stringers("summaryIDs", summaryIDs),
		)
	} else {
		s.ctx.Log.Debug("sent message",
			zap.Stringer("messageOp", message.AcceptedStateSummaryOp),
			zap.Stringer("nodeID", nodeID),
			zap.Uint32("requestID", requestID),
			zap.Stringers("summaryIDs", summaryIDs),
		)
	}
}

func (s *sender) SendGetAcceptedFrontier(ctx context.Context, nodeIDs set.Set[ids.NodeID], requestID uint32) {
	ctx = context.WithoutCancel(ctx)

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
		)
		s.router.RegisterRequest(
			ctx,
			nodeID,
			s.ctx.ChainID,
			requestID,
			message.AcceptedFrontierOp,
			inMsg,
			p2p.EngineType_ENGINE_TYPE_UNSPECIFIED,
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
		)
		s.router.HandleInternal(ctx, inMsg)
	}

	// Create the outbound message.
	outMsg, err := s.msgCreator.GetAcceptedFrontier(
		s.ctx.ChainID,
		requestID,
		deadline,
	)

	// Send the message over the network.
	var sentTo set.Set[ids.NodeID]
	if err == nil {
		sentTo = s.sender.Send(
			outMsg,
			common.SendConfig{
				NodeIDs: nodeIDs,
			},
			s.ctx.SubnetID,
			s.subnet,
		)
	} else {
		s.ctx.Log.Error("failed to build message",
			zap.Stringer("messageOp", message.GetAcceptedFrontierOp),
			zap.Uint32("requestID", requestID),
			zap.Duration("deadline", deadline),
			zap.Error(err),
		)
	}

	s.ctx.Log.Debug("sent message",
		zap.Stringer("messageOp", message.GetAcceptedFrontierOp),
		zap.Stringers("nodeIDs", sentTo.List()),
		zap.Uint32("requestID", requestID),
	)

	for nodeID := range nodeIDs {
		if !sentTo.Contains(nodeID) {
			s.ctx.Log.Debug("failed to send message",
				zap.Stringer("messageOp", message.GetAcceptedFrontierOp),
				zap.Stringer("nodeID", nodeID),
				zap.Uint32("requestID", requestID),
			)
		}
	}
}

func (s *sender) SendAcceptedFrontier(ctx context.Context, nodeID ids.NodeID, requestID uint32, containerID ids.ID) {
	ctx = context.WithoutCancel(ctx)

	// Sending this message to myself.
	if nodeID == s.ctx.NodeID {
		inMsg := message.InboundAcceptedFrontier(
			s.ctx.ChainID,
			requestID,
			containerID,
			nodeID,
		)
		s.router.HandleInternal(ctx, inMsg)
		return
	}

	// Create the outbound message.
	outMsg, err := s.msgCreator.AcceptedFrontier(
		s.ctx.ChainID,
		requestID,
		containerID,
	)
	if err != nil {
		s.ctx.Log.Error("failed to build message",
			zap.Stringer("messageOp", message.AcceptedFrontierOp),
			zap.Uint32("requestID", requestID),
			zap.Stringer("containerID", containerID),
			zap.Error(err),
		)
		return
	}

	// Send the message over the network.
	nodeIDs := set.Of(nodeID)
	sentTo := s.sender.Send(
		outMsg,
		common.SendConfig{
			NodeIDs: nodeIDs,
		},
		s.ctx.SubnetID,
		s.subnet,
	)
	if sentTo.Len() == 0 {
		s.ctx.Log.Debug("failed to send message",
			zap.Stringer("messageOp", message.AcceptedFrontierOp),
			zap.Stringer("nodeID", nodeID),
			zap.Uint32("requestID", requestID),
			zap.Stringer("containerID", containerID),
		)
	} else {
		s.ctx.Log.Debug("sent message",
			zap.Stringer("messageOp", message.AcceptedFrontierOp),
			zap.Stringer("nodeID", nodeID),
			zap.Uint32("requestID", requestID),
			zap.Stringer("containerID", containerID),
		)
	}
}

func (s *sender) SendGetAccepted(ctx context.Context, nodeIDs set.Set[ids.NodeID], requestID uint32, containerIDs []ids.ID) {
	ctx = context.WithoutCancel(ctx)

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
		)
		s.router.RegisterRequest(
			ctx,
			nodeID,
			s.ctx.ChainID,
			requestID,
			message.AcceptedOp,
			inMsg,
			p2p.EngineType_ENGINE_TYPE_UNSPECIFIED,
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
		)
		s.router.HandleInternal(ctx, inMsg)
	}

	// Create the outbound message.
	outMsg, err := s.msgCreator.GetAccepted(
		s.ctx.ChainID,
		requestID,
		deadline,
		containerIDs,
	)

	// Send the message over the network.
	var sentTo set.Set[ids.NodeID]
	if err == nil {
		sentTo = s.sender.Send(
			outMsg,
			common.SendConfig{
				NodeIDs: nodeIDs,
			},
			s.ctx.SubnetID,
			s.subnet,
		)
	} else {
		s.ctx.Log.Error("failed to build message",
			zap.Stringer("messageOp", message.GetAcceptedOp),
			zap.Uint32("requestID", requestID),
			zap.Stringers("containerIDs", containerIDs),
			zap.Error(err),
		)
	}

	s.ctx.Log.Debug("sent message",
		zap.Stringer("messageOp", message.GetAcceptedOp),
		zap.Stringers("nodeIDs", sentTo.List()),
		zap.Uint32("requestID", requestID),
		zap.Stringers("containerIDs", containerIDs),
	)

	for nodeID := range nodeIDs {
		if !sentTo.Contains(nodeID) {
			s.ctx.Log.Debug("failed to send message",
				zap.Stringer("messageOp", message.GetAcceptedOp),
				zap.Stringer("nodeID", nodeID),
				zap.Uint32("requestID", requestID),
				zap.Stringers("containerIDs", containerIDs),
			)
		}
	}
}

func (s *sender) SendAccepted(ctx context.Context, nodeID ids.NodeID, requestID uint32, containerIDs []ids.ID) {
	ctx = context.WithoutCancel(ctx)

	if nodeID == s.ctx.NodeID {
		inMsg := message.InboundAccepted(
			s.ctx.ChainID,
			requestID,
			containerIDs,
			nodeID,
		)
		s.router.HandleInternal(ctx, inMsg)
		return
	}

	// Create the outbound message.
	outMsg, err := s.msgCreator.Accepted(s.ctx.ChainID, requestID, containerIDs)
	if err != nil {
		s.ctx.Log.Error("failed to build message",
			zap.Stringer("messageOp", message.AcceptedOp),
			zap.Uint32("requestID", requestID),
			zap.Stringers("containerIDs", containerIDs),
			zap.Error(err),
		)
		return
	}

	// Send the message over the network.
	nodeIDs := set.Of(nodeID)
	sentTo := s.sender.Send(
		outMsg,
		common.SendConfig{
			NodeIDs: nodeIDs,
		},
		s.ctx.SubnetID,
		s.subnet,
	)
	if sentTo.Len() == 0 {
		s.ctx.Log.Debug("failed to send message",
			zap.Stringer("messageOp", message.AcceptedOp),
			zap.Stringer("nodeID", nodeID),
			zap.Uint32("requestID", requestID),
			zap.Stringers("containerIDs", containerIDs),
		)
	} else {
		s.ctx.Log.Debug("sent message",
			zap.Stringer("messageOp", message.AcceptedOp),
			zap.Stringer("nodeID", nodeID),
			zap.Uint32("requestID", requestID),
			zap.Stringers("containerIDs", containerIDs),
		)
	}
}

func (s *sender) SendGetAncestors(ctx context.Context, nodeID ids.NodeID, requestID uint32, containerID ids.ID) {
	ctx = context.WithoutCancel(ctx)

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
		requestID,
		message.AncestorsOp,
		inMsg,
		s.engineType,
	)

	// Sending a GetAncestors to myself will fail. To avoid constantly sending
	// myself requests when not connected to any peers, we rely on the timeout
	// firing to deliver the GetAncestorsFailed message.
	if nodeID == s.ctx.NodeID {
		return
	}

	// [nodeID] may be benched. That is, they've been unresponsive so we don't
	// even bother sending requests to them. We just have them immediately fail.
	if s.timeouts.IsBenched(nodeID, s.ctx.ChainID) {
		s.failedDueToBench.With(prometheus.Labels{
			opLabel: message.GetAncestorsOp.String(),
		}).Inc()
		s.timeouts.RegisterRequestToUnreachableValidator()
		s.router.HandleInternal(ctx, inMsg)
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
			zap.Uint32("requestID", requestID),
			zap.Stringer("containerID", containerID),
			zap.Error(err),
		)

		s.router.HandleInternal(ctx, inMsg)
		return
	}

	// Send the message over the network.
	nodeIDs := set.Of(nodeID)
	sentTo := s.sender.Send(
		outMsg,
		common.SendConfig{
			NodeIDs: nodeIDs,
		},
		s.ctx.SubnetID,
		s.subnet,
	)
	if sentTo.Len() == 0 {
		s.ctx.Log.Debug("failed to send message",
			zap.Stringer("messageOp", message.GetAncestorsOp),
			zap.Stringer("nodeID", nodeID),
			zap.Uint32("requestID", requestID),
			zap.Stringer("containerID", containerID),
		)

		s.timeouts.RegisterRequestToUnreachableValidator()
		s.router.HandleInternal(ctx, inMsg)
	} else {
		s.ctx.Log.Debug("sent message",
			zap.Stringer("messageOp", message.GetAncestorsOp),
			zap.Stringer("nodeID", nodeID),
			zap.Uint32("requestID", requestID),
			zap.Stringer("containerID", containerID),
		)
	}
}

func (s *sender) SendAncestors(_ context.Context, nodeID ids.NodeID, requestID uint32, containers [][]byte) {
	// Create the outbound message.
	outMsg, err := s.msgCreator.Ancestors(s.ctx.ChainID, requestID, containers)
	if err != nil {
		s.ctx.Log.Error("failed to build message",
			zap.Stringer("messageOp", message.AncestorsOp),
			zap.Uint32("requestID", requestID),
			zap.Int("numContainers", len(containers)),
			zap.Error(err),
		)
		return
	}

	// Send the message over the network.
	nodeIDs := set.Of(nodeID)
	sentTo := s.sender.Send(
		outMsg,
		common.SendConfig{
			NodeIDs: nodeIDs,
		},
		s.ctx.SubnetID,
		s.subnet,
	)
	if sentTo.Len() == 0 {
		s.ctx.Log.Debug("failed to send message",
			zap.Stringer("messageOp", message.AncestorsOp),
			zap.Stringer("nodeID", nodeID),
			zap.Uint32("requestID", requestID),
			zap.Int("numContainers", len(containers)),
		)
	} else {
		s.ctx.Log.Debug("sent message",
			zap.Stringer("messageOp", message.AncestorsOp),
			zap.Stringer("nodeID", nodeID),
			zap.Uint32("requestID", requestID),
			zap.Int("numContainers", len(containers)),
		)
	}
}

func (s *sender) SendGet(ctx context.Context, nodeID ids.NodeID, requestID uint32, containerID ids.ID) {
	ctx = context.WithoutCancel(ctx)

	// Tell the router to expect a response message or a message notifying
	// that we won't get a response from this node.
	inMsg := message.InternalGetFailed(
		nodeID,
		s.ctx.ChainID,
		requestID,
	)
	s.router.RegisterRequest(
		ctx,
		nodeID,
		s.ctx.ChainID,
		requestID,
		message.PutOp,
		inMsg,
		p2p.EngineType_ENGINE_TYPE_UNSPECIFIED,
	)

	// Sending a Get to myself always fails.
	if nodeID == s.ctx.NodeID {
		s.router.HandleInternal(ctx, inMsg)
		return
	}

	// [nodeID] may be benched. That is, they've been unresponsive so we don't
	// even bother sending requests to them. We just have them immediately fail.
	if s.timeouts.IsBenched(nodeID, s.ctx.ChainID) {
		s.failedDueToBench.With(prometheus.Labels{
			opLabel: message.GetOp.String(),
		}).Inc()
		s.timeouts.RegisterRequestToUnreachableValidator()
		s.router.HandleInternal(ctx, inMsg)
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
	)

	// Send the message over the network.
	var sentTo set.Set[ids.NodeID]
	if err == nil {
		nodeIDs := set.Of(nodeID)
		sentTo = s.sender.Send(
			outMsg,
			common.SendConfig{
				NodeIDs: nodeIDs,
			},
			s.ctx.SubnetID,
			s.subnet,
		)
	} else {
		s.ctx.Log.Error("failed to build message",
			zap.Stringer("messageOp", message.GetOp),
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
			zap.Uint32("requestID", requestID),
			zap.Stringer("containerID", containerID),
		)

		s.timeouts.RegisterRequestToUnreachableValidator()
		s.router.HandleInternal(ctx, inMsg)
	} else {
		s.ctx.Log.Debug("sent message",
			zap.Stringer("messageOp", message.GetOp),
			zap.Stringer("nodeID", nodeID),
			zap.Uint32("requestID", requestID),
			zap.Stringer("containerID", containerID),
		)
	}
}

func (s *sender) SendPut(_ context.Context, nodeID ids.NodeID, requestID uint32, container []byte) {
	// Create the outbound message.
	outMsg, err := s.msgCreator.Put(s.ctx.ChainID, requestID, container)
	if err != nil {
		s.ctx.Log.Error("failed to build message",
			zap.Stringer("messageOp", message.PutOp),
			zap.Uint32("requestID", requestID),
			zap.Binary("container", container),
			zap.Error(err),
		)
		return
	}

	// Send the message over the network.
	nodeIDs := set.Of(nodeID)
	sentTo := s.sender.Send(
		outMsg,
		common.SendConfig{
			NodeIDs: nodeIDs,
		},
		s.ctx.SubnetID,
		s.subnet,
	)
	if sentTo.Len() == 0 {
		if s.ctx.Log.Enabled(logging.Verbo) {
			s.ctx.Log.Verbo("failed to send message",
				zap.Stringer("messageOp", message.PutOp),
				zap.Stringer("nodeID", nodeID),
				zap.Uint32("requestID", requestID),
				zap.Binary("container", container),
			)
		} else {
			s.ctx.Log.Debug("failed to send message",
				zap.Stringer("messageOp", message.PutOp),
				zap.Stringer("nodeID", nodeID),
				zap.Uint32("requestID", requestID),
			)
		}
	} else {
		s.ctx.Log.Debug("sent message",
			zap.Stringer("messageOp", message.PutOp),
			zap.Stringer("nodeID", nodeID),
			zap.Uint32("requestID", requestID),
		)
	}
}

func (s *sender) SendPushQuery(
	ctx context.Context,
	nodeIDs set.Set[ids.NodeID],
	requestID uint32,
	container []byte,
	requestedHeight uint64,
) {
	ctx = context.WithoutCancel(ctx)

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
		)
		s.router.RegisterRequest(
			ctx,
			nodeID,
			s.ctx.ChainID,
			requestID,
			message.ChitsOp,
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
		inMsg := message.InboundPushQuery(
			s.ctx.ChainID,
			requestID,
			deadline,
			container,
			requestedHeight,
			s.ctx.NodeID,
		)
		s.router.HandleInternal(ctx, inMsg)
	}

	// Some of [nodeIDs] may be benched. That is, they've been unresponsive so
	// we don't even bother sending messages to them. We just have them
	// immediately fail.
	for nodeID := range nodeIDs {
		if s.timeouts.IsBenched(nodeID, s.ctx.ChainID) {
			s.failedDueToBench.With(prometheus.Labels{
				opLabel: message.PushQueryOp.String(),
			}).Inc()
			nodeIDs.Remove(nodeID)
			s.timeouts.RegisterRequestToUnreachableValidator()

			// Immediately register a failure. Do so asynchronously to avoid
			// deadlock.
			inMsg := message.InternalQueryFailed(
				nodeID,
				s.ctx.ChainID,
				requestID,
			)
			s.router.HandleInternal(ctx, inMsg)
		}
	}

	// Create the outbound message.
	outMsg, err := s.msgCreator.PushQuery(
		s.ctx.ChainID,
		requestID,
		deadline,
		container,
		requestedHeight,
	)

	// Send the message over the network.
	// [sentTo] are the IDs of validators who may receive the message.
	var sentTo set.Set[ids.NodeID]
	if err == nil {
		sentTo = s.sender.Send(
			outMsg,
			common.SendConfig{
				NodeIDs: nodeIDs,
			},
			s.ctx.SubnetID,
			s.subnet,
		)
	} else {
		s.ctx.Log.Error("failed to build message",
			zap.Stringer("messageOp", message.PushQueryOp),
			zap.Uint32("requestID", requestID),
			zap.Binary("container", container),
			zap.Uint64("requestedHeight", requestedHeight),
			zap.Error(err),
		)
	}

	s.ctx.Log.Debug("sent message",
		zap.Stringer("messageOp", message.PushQueryOp),
		zap.Stringers("nodeIDs", sentTo.List()),
		zap.Uint32("requestID", requestID),
		zap.Uint64("requestedHeight", requestedHeight),
	)

	for nodeID := range nodeIDs {
		if !sentTo.Contains(nodeID) {
			if s.ctx.Log.Enabled(logging.Verbo) {
				s.ctx.Log.Verbo("failed to send message",
					zap.Stringer("messageOp", message.PushQueryOp),
					zap.Stringer("nodeID", nodeID),
					zap.Uint32("requestID", requestID),
					zap.Binary("container", container),
					zap.Uint64("requestedHeight", requestedHeight),
				)
			} else {
				s.ctx.Log.Debug("failed to send message",
					zap.Stringer("messageOp", message.PushQueryOp),
					zap.Stringer("nodeID", nodeID),
					zap.Uint32("requestID", requestID),
					zap.Uint64("requestedHeight", requestedHeight),
				)
			}

			// Register failures for nodes we didn't send a request to.
			s.timeouts.RegisterRequestToUnreachableValidator()
			inMsg := message.InternalQueryFailed(
				nodeID,
				s.ctx.ChainID,
				requestID,
			)
			s.router.HandleInternal(ctx, inMsg)
		}
	}
}

func (s *sender) SendPullQuery(
	ctx context.Context,
	nodeIDs set.Set[ids.NodeID],
	requestID uint32,
	containerID ids.ID,
	requestedHeight uint64,
) {
	ctx = context.WithoutCancel(ctx)

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
		)
		s.router.RegisterRequest(
			ctx,
			nodeID,
			s.ctx.ChainID,
			requestID,
			message.ChitsOp,
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
		inMsg := message.InboundPullQuery(
			s.ctx.ChainID,
			requestID,
			deadline,
			containerID,
			requestedHeight,
			s.ctx.NodeID,
		)
		s.router.HandleInternal(ctx, inMsg)
	}

	// Some of the nodes in [nodeIDs] may be benched. That is, they've been
	// unresponsive so we don't even bother sending messages to them. We just
	// have them immediately fail.
	for nodeID := range nodeIDs {
		if s.timeouts.IsBenched(nodeID, s.ctx.ChainID) {
			s.failedDueToBench.With(prometheus.Labels{
				opLabel: message.PullQueryOp.String(),
			}).Inc()
			nodeIDs.Remove(nodeID)
			s.timeouts.RegisterRequestToUnreachableValidator()
			// Immediately register a failure. Do so asynchronously to avoid
			// deadlock.
			inMsg := message.InternalQueryFailed(
				nodeID,
				s.ctx.ChainID,
				requestID,
			)
			s.router.HandleInternal(ctx, inMsg)
		}
	}

	// Create the outbound message.
	outMsg, err := s.msgCreator.PullQuery(
		s.ctx.ChainID,
		requestID,
		deadline,
		containerID,
		requestedHeight,
	)

	// Send the message over the network.
	var sentTo set.Set[ids.NodeID]
	if err == nil {
		sentTo = s.sender.Send(
			outMsg,
			common.SendConfig{
				NodeIDs: nodeIDs,
			},
			s.ctx.SubnetID,
			s.subnet,
		)
	} else {
		s.ctx.Log.Error("failed to build message",
			zap.Stringer("messageOp", message.PullQueryOp),
			zap.Uint32("requestID", requestID),
			zap.Duration("deadline", deadline),
			zap.Stringer("containerID", containerID),
			zap.Uint64("requestedHeight", requestedHeight),
			zap.Error(err),
		)
	}

	s.ctx.Log.Debug("sent message",
		zap.Stringer("messageOp", message.PullQueryOp),
		zap.Stringers("nodeIDs", sentTo.List()),
		zap.Uint32("requestID", requestID),
		zap.Stringer("containerID", containerID),
		zap.Uint64("requestedHeight", requestedHeight),
	)

	for nodeID := range nodeIDs {
		if !sentTo.Contains(nodeID) {
			s.ctx.Log.Debug("failed to send message",
				zap.Stringer("messageOp", message.PullQueryOp),
				zap.Stringer("nodeID", nodeID),
				zap.Uint32("requestID", requestID),
				zap.Stringer("containerID", containerID),
				zap.Uint64("requestedHeight", requestedHeight),
			)

			// Register failures for nodes we didn't send a request to.
			s.timeouts.RegisterRequestToUnreachableValidator()
			inMsg := message.InternalQueryFailed(
				nodeID,
				s.ctx.ChainID,
				requestID,
			)
			s.router.HandleInternal(ctx, inMsg)
		}
	}
}

func (s *sender) SendChits(
	ctx context.Context,
	nodeID ids.NodeID,
	requestID uint32,
	preferredID ids.ID,
	preferredIDAtHeight ids.ID,
	acceptedID ids.ID,
	acceptedHeight uint64,
) {
	ctx = context.WithoutCancel(ctx)

	// If [nodeID] is myself, send this message directly
	// to my own router rather than sending it over the network
	if nodeID == s.ctx.NodeID {
		inMsg := message.InboundChits(
			s.ctx.ChainID,
			requestID,
			preferredID,
			preferredIDAtHeight,
			acceptedID,
			nodeID,
		)
		s.router.HandleInternal(ctx, inMsg)
		return
	}

	// Create the outbound message.
	outMsg, err := s.msgCreator.Chits(s.ctx.ChainID, requestID, preferredID, preferredIDAtHeight, acceptedID, acceptedHeight)
	if err != nil {
		s.ctx.Log.Error("failed to build message",
			zap.Stringer("messageOp", message.ChitsOp),
			zap.Uint32("requestID", requestID),
			zap.Stringer("preferredID", preferredID),
			zap.Stringer("preferredIDAtHeight", preferredIDAtHeight),
			zap.Stringer("acceptedID", acceptedID),
			zap.Error(err),
		)
		return
	}

	// Send the message over the network.
	nodeIDs := set.Of(nodeID)
	sentTo := s.sender.Send(
		outMsg,
		common.SendConfig{
			NodeIDs: nodeIDs,
		},
		s.ctx.SubnetID,
		s.subnet,
	)
	if sentTo.Len() == 0 {
		s.ctx.Log.Debug("failed to send message",
			zap.Stringer("messageOp", message.ChitsOp),
			zap.Stringer("nodeID", nodeID),
			zap.Uint32("requestID", requestID),
			zap.Stringer("preferredID", preferredID),
			zap.Stringer("preferredIDAtHeight", preferredIDAtHeight),
			zap.Stringer("acceptedID", acceptedID),
		)
	} else {
		s.ctx.Log.Debug("sent message",
			zap.Stringer("messageOp", message.ChitsOp),
			zap.Stringer("nodeID", nodeID),
			zap.Uint32("requestID", requestID),
			zap.Stringer("preferredID", preferredID),
			zap.Stringer("preferredIDAtHeight", preferredIDAtHeight),
			zap.Stringer("acceptedID", acceptedID),
		)
	}
}

func (s *sender) SendAppRequest(ctx context.Context, nodeIDs set.Set[ids.NodeID], requestID uint32, appRequestBytes []byte) error {
	ctx = context.WithoutCancel(ctx)

	// Tell the router to expect a response message or a message notifying
	// that we won't get a response from each of these nodes.
	// We register timeouts for all nodes, regardless of whether we fail
	// to send them a message, to avoid busy looping when disconnected from
	// the internet.
	for nodeID := range nodeIDs {
		inMsg := message.InboundAppError(
			nodeID,
			s.ctx.ChainID,
			requestID,
			common.ErrTimeout.Code,
			common.ErrTimeout.Message,
		)
		s.router.RegisterRequest(
			ctx,
			nodeID,
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
		s.router.HandleInternal(ctx, inMsg)
	}

	// Some of the nodes in [nodeIDs] may be benched. That is, they've been
	// unresponsive so we don't even bother sending messages to them. We just
	// have them immediately fail.
	for nodeID := range nodeIDs {
		if s.timeouts.IsBenched(nodeID, s.ctx.ChainID) {
			s.failedDueToBench.With(prometheus.Labels{
				opLabel: message.AppRequestOp.String(),
			}).Inc()
			nodeIDs.Remove(nodeID)
			s.timeouts.RegisterRequestToUnreachableValidator()

			// Immediately register a failure. Do so asynchronously to avoid
			// deadlock.
			inMsg := message.InboundAppError(
				nodeID,
				s.ctx.ChainID,
				requestID,
				common.ErrTimeout.Code,
				common.ErrTimeout.Message,
			)
			s.router.HandleInternal(ctx, inMsg)
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
			common.SendConfig{
				NodeIDs: nodeIDs,
			},
			s.ctx.SubnetID,
			s.subnet,
		)
	} else {
		s.ctx.Log.Error("failed to build message",
			zap.Stringer("messageOp", message.AppRequestOp),
			zap.Uint32("requestID", requestID),
			zap.Binary("payload", appRequestBytes),
			zap.Error(err),
		)
	}

	s.ctx.Log.Debug("sent message",
		zap.Stringer("messageOp", message.AppRequestOp),
		zap.Stringers("nodeIDs", nodeIDs.List()),
		zap.Uint32("requestID", requestID),
	)

	for nodeID := range nodeIDs {
		if !sentTo.Contains(nodeID) {
			if s.ctx.Log.Enabled(logging.Verbo) {
				s.ctx.Log.Verbo("failed to send message",
					zap.Stringer("messageOp", message.AppRequestOp),
					zap.Stringer("nodeID", nodeID),
					zap.Uint32("requestID", requestID),
					zap.Binary("payload", appRequestBytes),
				)
			} else {
				s.ctx.Log.Debug("failed to send message",
					zap.Stringer("messageOp", message.AppRequestOp),
					zap.Stringer("nodeID", nodeID),
					zap.Uint32("requestID", requestID),
				)
			}

			// Register failures for nodes we didn't send a request to.
			s.timeouts.RegisterRequestToUnreachableValidator()
			inMsg := message.InboundAppError(
				nodeID,
				s.ctx.ChainID,
				requestID,
				common.ErrTimeout.Code,
				common.ErrTimeout.Message,
			)
			s.router.HandleInternal(ctx, inMsg)
		}
	}
	return nil
}

func (s *sender) SendAppResponse(ctx context.Context, nodeID ids.NodeID, requestID uint32, appResponseBytes []byte) error {
	ctx = context.WithoutCancel(ctx)

	if nodeID == s.ctx.NodeID {
		inMsg := message.InboundAppResponse(
			s.ctx.ChainID,
			requestID,
			appResponseBytes,
			nodeID,
		)
		s.router.HandleInternal(ctx, inMsg)
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
			zap.Uint32("requestID", requestID),
			zap.Binary("payload", appResponseBytes),
			zap.Error(err),
		)
		return nil
	}

	// Send the message over the network.
	nodeIDs := set.Of(nodeID)
	sentTo := s.sender.Send(
		outMsg,
		common.SendConfig{
			NodeIDs: nodeIDs,
		},
		s.ctx.SubnetID,
		s.subnet,
	)
	if sentTo.Len() == 0 {
		if s.ctx.Log.Enabled(logging.Verbo) {
			s.ctx.Log.Verbo("failed to send message",
				zap.Stringer("messageOp", message.AppResponseOp),
				zap.Stringer("nodeID", nodeID),
				zap.Uint32("requestID", requestID),
				zap.Binary("payload", appResponseBytes),
			)
		} else {
			s.ctx.Log.Debug("failed to send message",
				zap.Stringer("messageOp", message.AppResponseOp),
				zap.Stringer("nodeID", nodeID),
				zap.Uint32("requestID", requestID),
			)
		}
	} else {
		s.ctx.Log.Debug("sent message",
			zap.Stringer("messageOp", message.AppResponseOp),
			zap.Stringer("nodeID", nodeID),
			zap.Uint32("requestID", requestID),
		)
	}
	return nil
}

func (s *sender) SendAppError(ctx context.Context, nodeID ids.NodeID, requestID uint32, errorCode int32, errorMessage string) error {
	ctx = context.WithoutCancel(ctx)

	if nodeID == s.ctx.NodeID {
		inMsg := message.InboundAppError(
			nodeID,
			s.ctx.ChainID,
			requestID,
			errorCode,
			errorMessage,
		)
		s.router.HandleInternal(ctx, inMsg)
		return nil
	}

	// Create the outbound message.
	outMsg, err := s.msgCreator.AppError(
		s.ctx.ChainID,
		requestID,
		errorCode,
		errorMessage,
	)
	if err != nil {
		s.ctx.Log.Error("failed to build message",
			zap.Stringer("messageOp", message.AppErrorOp),
			zap.Stringer("nodeID", nodeID),
			zap.Uint32("requestID", requestID),
			zap.Int32("errorCode", errorCode),
			zap.String("errorMessage", errorMessage),
			zap.Error(err),
		)
		return nil
	}

	// Send the message over the network.
	sentTo := s.sender.Send(
		outMsg,
		common.SendConfig{
			NodeIDs: set.Of(nodeID),
		},
		s.ctx.SubnetID,
		s.subnet,
	)
	if sentTo.Len() == 0 {
		if s.ctx.Log.Enabled(logging.Verbo) {
			s.ctx.Log.Verbo("failed to send message",
				zap.Stringer("messageOp", message.AppErrorOp),
				zap.Stringer("nodeID", nodeID),
				zap.Uint32("requestID", requestID),
				zap.Int32("errorCode", errorCode),
				zap.String("errorMessage", errorMessage),
			)
		} else {
			s.ctx.Log.Debug("failed to send message",
				zap.Stringer("messageOp", message.AppErrorOp),
				zap.Stringer("nodeID", nodeID),
				zap.Uint32("requestID", requestID),
				zap.Int32("errorCode", errorCode),
				zap.String("errorMessage", errorMessage),
			)
		}
	} else {
		s.ctx.Log.Debug("sent message",
			zap.Stringer("messageOp", message.AppErrorOp),
			zap.Stringer("nodeID", nodeID),
			zap.Uint32("requestID", requestID),
			zap.Int32("errorCode", errorCode),
			zap.String("errorMessage", errorMessage),
		)
	}
	return nil
}

func (s *sender) SendAppGossip(
	_ context.Context,
	config common.SendConfig,
	appGossipBytes []byte,
) error {
	// Create the outbound message.
	outMsg, err := s.msgCreator.AppGossip(s.ctx.ChainID, appGossipBytes)
	if err != nil {
		s.ctx.Log.Error("failed to build message",
			zap.Stringer("messageOp", message.AppGossipOp),
			zap.Binary("payload", appGossipBytes),
			zap.Error(err),
		)
		return nil
	}

	sentTo := s.sender.Send(
		outMsg,
		config,
		s.ctx.SubnetID,
		s.subnet,
	)
	if sentTo.Len() == 0 {
		if s.ctx.Log.Enabled(logging.Verbo) {
			s.ctx.Log.Verbo("failed to send message",
				zap.Stringer("messageOp", message.AppGossipOp),
				zap.Binary("payload", appGossipBytes),
			)
		} else {
			s.ctx.Log.Debug("failed to send message",
				zap.Stringer("messageOp", message.AppGossipOp),
			)
		}
	} else {
		s.ctx.Log.Debug("sent message",
			zap.Stringer("messageOp", message.AppGossipOp),
			zap.Stringers("nodeIDs", sentTo.List()),
		)
	}
	return nil
}

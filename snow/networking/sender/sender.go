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
	for nodeID := range nodeIDs {
		s.router.RegisterRequest(
			ctx,
			nodeID,
			s.ctx.ChainID,
			requestID,
			message.StateSummaryFrontierOp,
			message.InternalGetStateSummaryFrontierFailed(
				nodeID,
				s.ctx.ChainID,
				requestID,
			),
			p2p.EngineType_ENGINE_TYPE_UNSPECIFIED,
		)
	}

	// The deadline is used as a best-effort communication to the peer for when
	// we expect the message by. It's not guaranteed to exactly match our
	// registered timeout.
	deadline := s.timeouts.TimeoutDuration()
	if nodeIDs.Contains(s.ctx.NodeID) {
		nodeIDs.Remove(s.ctx.NodeID)
		s.router.HandleInternal(
			ctx,
			message.InboundGetStateSummaryFrontier(
				s.ctx.ChainID,
				requestID,
				deadline,
				s.ctx.NodeID,
			),
		)
	}

	log := s.ctx.Log.With(
		zap.Stringer("messageOp", message.GetStateSummaryFrontierOp),
		zap.Uint32("requestID", requestID),
		zap.Duration("deadline", deadline),
	)
	to := common.SendConfig{
		NodeIDs: nodeIDs,
	}
	msg, err := s.msgCreator.GetStateSummaryFrontier(
		s.ctx.ChainID,
		requestID,
		deadline,
	)
	s.sendUnlessError(log, to, msg, err)
}

func (s *sender) SendStateSummaryFrontier(ctx context.Context, nodeID ids.NodeID, requestID uint32, summary []byte) {
	ctx = context.WithoutCancel(ctx)
	if nodeID == s.ctx.NodeID {
		s.router.HandleInternal(
			ctx,
			message.InboundStateSummaryFrontier(
				s.ctx.ChainID,
				requestID,
				summary,
				nodeID,
			),
		)
		return
	}

	log := s.logWith(
		zap.Binary("summary", summary), // only logged if verbo
		zap.Stringer("messageOp", message.StateSummaryFrontierOp),
		zap.Uint32("requestID", requestID),
	)
	to := common.SendConfig{
		NodeIDs: set.Of(nodeID),
	}
	msg, err := s.msgCreator.StateSummaryFrontier(
		s.ctx.ChainID,
		requestID,
		summary,
	)
	s.sendUnlessError(log, to, msg, err)
}

func (s *sender) SendGetAcceptedStateSummary(ctx context.Context, nodeIDs set.Set[ids.NodeID], requestID uint32, heights []uint64) {
	ctx = context.WithoutCancel(ctx)
	for nodeID := range nodeIDs {
		s.router.RegisterRequest(
			ctx,
			nodeID,
			s.ctx.ChainID,
			requestID,
			message.AcceptedStateSummaryOp,
			message.InternalGetAcceptedStateSummaryFailed(
				nodeID,
				s.ctx.ChainID,
				requestID,
			),
			p2p.EngineType_ENGINE_TYPE_UNSPECIFIED,
		)
	}

	// The deadline is used as a best-effort communication to the peer for when
	// we expect the message by. It's not guaranteed to exactly match our
	// registered timeout.
	deadline := s.timeouts.TimeoutDuration()
	if nodeIDs.Contains(s.ctx.NodeID) {
		nodeIDs.Remove(s.ctx.NodeID)
		s.router.HandleInternal(
			ctx,
			message.InboundGetAcceptedStateSummary(
				s.ctx.ChainID,
				requestID,
				heights,
				deadline,
				s.ctx.NodeID,
			),
		)
	}

	log := s.ctx.Log.With(
		zap.Stringer("messageOp", message.GetAcceptedStateSummaryOp),
		zap.Uint32("requestID", requestID),
		zap.Duration("deadline", deadline),
		zap.Uint64s("heights", heights),
	)
	to := common.SendConfig{
		NodeIDs: nodeIDs,
	}
	msg, err := s.msgCreator.GetAcceptedStateSummary(
		s.ctx.ChainID,
		requestID,
		deadline,
		heights,
	)
	s.sendUnlessError(log, to, msg, err)
}

func (s *sender) SendAcceptedStateSummary(ctx context.Context, nodeID ids.NodeID, requestID uint32, summaryIDs []ids.ID) {
	ctx = context.WithoutCancel(ctx)
	if nodeID == s.ctx.NodeID {
		s.router.HandleInternal(
			ctx,
			message.InboundAcceptedStateSummary(
				s.ctx.ChainID,
				requestID,
				summaryIDs,
				nodeID,
			),
		)
		return
	}

	log := s.ctx.Log.With(
		zap.Stringer("messageOp", message.AcceptedStateSummaryOp),
		zap.Uint32("requestID", requestID),
		zap.Stringers("summaryIDs", summaryIDs),
	)
	to := common.SendConfig{
		NodeIDs: set.Of(nodeID),
	}
	msg, err := s.msgCreator.AcceptedStateSummary(
		s.ctx.ChainID,
		requestID,
		summaryIDs,
	)
	s.sendUnlessError(log, to, msg, err)
}

func (s *sender) SendGetAcceptedFrontier(ctx context.Context, nodeIDs set.Set[ids.NodeID], requestID uint32) {
	ctx = context.WithoutCancel(ctx)
	for nodeID := range nodeIDs {
		s.router.RegisterRequest(
			ctx,
			nodeID,
			s.ctx.ChainID,
			requestID,
			message.AcceptedFrontierOp,
			message.InternalGetAcceptedFrontierFailed(
				nodeID,
				s.ctx.ChainID,
				requestID,
			),
			p2p.EngineType_ENGINE_TYPE_UNSPECIFIED,
		)
	}

	// The deadline is used as a best-effort communication to the peer for when
	// we expect the message by. It's not guaranteed to exactly match our
	// registered timeout.
	deadline := s.timeouts.TimeoutDuration()
	if nodeIDs.Contains(s.ctx.NodeID) {
		nodeIDs.Remove(s.ctx.NodeID)
		s.router.HandleInternal(
			ctx,
			message.InboundGetAcceptedFrontier(
				s.ctx.ChainID,
				requestID,
				deadline,
				s.ctx.NodeID,
			),
		)
	}

	log := s.ctx.Log.With(
		zap.Stringer("messageOp", message.GetAcceptedFrontierOp),
		zap.Uint32("requestID", requestID),
		zap.Duration("deadline", deadline),
	)
	to := common.SendConfig{
		NodeIDs: nodeIDs,
	}
	msg, err := s.msgCreator.GetAcceptedFrontier(
		s.ctx.ChainID,
		requestID,
		deadline,
	)
	s.sendUnlessError(log, to, msg, err)
}

func (s *sender) SendAcceptedFrontier(ctx context.Context, nodeID ids.NodeID, requestID uint32, containerID ids.ID) {
	ctx = context.WithoutCancel(ctx)
	if nodeID == s.ctx.NodeID {
		s.router.HandleInternal(
			ctx,
			message.InboundAcceptedFrontier(
				s.ctx.ChainID,
				requestID,
				containerID,
				nodeID,
			),
		)
		return
	}

	log := s.ctx.Log.With(
		zap.Stringer("messageOp", message.AcceptedFrontierOp),
		zap.Uint32("requestID", requestID),
		zap.Stringer("containerID", containerID),
	)
	to := common.SendConfig{
		NodeIDs: set.Of(nodeID),
	}
	msg, err := s.msgCreator.AcceptedFrontier(
		s.ctx.ChainID,
		requestID,
		containerID,
	)
	s.sendUnlessError(log, to, msg, err)
}

func (s *sender) SendGetAccepted(ctx context.Context, nodeIDs set.Set[ids.NodeID], requestID uint32, containerIDs []ids.ID) {
	ctx = context.WithoutCancel(ctx)
	for nodeID := range nodeIDs {
		s.router.RegisterRequest(
			ctx,
			nodeID,
			s.ctx.ChainID,
			requestID,
			message.AcceptedOp,
			message.InternalGetAcceptedFailed(
				nodeID,
				s.ctx.ChainID,
				requestID,
			),
			p2p.EngineType_ENGINE_TYPE_UNSPECIFIED,
		)
	}

	// The deadline is used as a best-effort communication to the peer for when
	// we expect the message by. It's not guaranteed to exactly match our
	// registered timeout.
	deadline := s.timeouts.TimeoutDuration()
	if nodeIDs.Contains(s.ctx.NodeID) {
		nodeIDs.Remove(s.ctx.NodeID)
		s.router.HandleInternal(
			ctx,
			message.InboundGetAccepted(
				s.ctx.ChainID,
				requestID,
				deadline,
				containerIDs,
				s.ctx.NodeID,
			),
		)
	}

	log := s.ctx.Log.With(
		zap.Stringer("messageOp", message.GetAcceptedOp),
		zap.Uint32("requestID", requestID),
		zap.Duration("deadline", deadline),
		zap.Stringers("containerIDs", containerIDs),
	)
	to := common.SendConfig{
		NodeIDs: nodeIDs,
	}
	msg, err := s.msgCreator.GetAccepted(
		s.ctx.ChainID,
		requestID,
		deadline,
		containerIDs,
	)
	s.sendUnlessError(log, to, msg, err)
}

func (s *sender) SendAccepted(ctx context.Context, nodeID ids.NodeID, requestID uint32, containerIDs []ids.ID) {
	ctx = context.WithoutCancel(ctx)
	if nodeID == s.ctx.NodeID {
		s.router.HandleInternal(
			ctx,
			message.InboundAccepted(
				s.ctx.ChainID,
				requestID,
				containerIDs,
				nodeID,
			),
		)
		return
	}

	log := s.ctx.Log.With(
		zap.Stringer("messageOp", message.AcceptedOp),
		zap.Uint32("requestID", requestID),
		zap.Stringers("containerIDs", containerIDs),
	)
	to := common.SendConfig{
		NodeIDs: set.Of(nodeID),
	}
	msg, err := s.msgCreator.Accepted(s.ctx.ChainID, requestID, containerIDs)
	s.sendUnlessError(log, to, msg, err)
}

func (s *sender) SendGetAncestors(ctx context.Context, nodeID ids.NodeID, requestID uint32, containerID ids.ID) {
	ctx = context.WithoutCancel(ctx)
	getFailed := message.InternalGetAncestorsFailed(
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
		getFailed,
		s.engineType,
	)

	// Sending a GetAncestors to myself will fail. To avoid constantly sending
	// myself requests when not connected to any peers, we rely on the timeout
	// firing to deliver the GetAncestorsFailed message.
	if nodeID == s.ctx.NodeID {
		return
	}

	if s.timeouts.IsBenched(nodeID, s.ctx.ChainID) {
		s.failedDueToBench.With(prometheus.Labels{
			opLabel: message.GetAncestorsOp.String(),
		}).Inc()
		s.timeouts.RegisterRequestToUnreachableValidator()
		s.router.HandleInternal(ctx, getFailed)
		return
	}

	// The deadline is used as a best-effort communication to the peer for when
	// we expect the message by. It's not guaranteed to exactly match our
	// registered timeout.
	deadline := s.timeouts.TimeoutDuration()
	log := s.ctx.Log.With(
		zap.Stringer("messageOp", message.GetAncestorsOp),
		zap.Uint32("requestID", requestID),
		zap.Duration("deadline", deadline),
		zap.Stringer("containerID", containerID),
		zap.Stringer("engineType", s.engineType),
	)
	to := common.SendConfig{
		NodeIDs: set.Of(nodeID),
	}
	msg, err := s.msgCreator.GetAncestors(
		s.ctx.ChainID,
		requestID,
		deadline,
		containerID,
		s.engineType,
	)
	sent := s.sendUnlessError(log, to, msg, err)
	if sent.Len() == 0 {
		s.timeouts.RegisterRequestToUnreachableValidator()
		s.router.HandleInternal(ctx, getFailed)
	}
}

func (s *sender) SendAncestors(_ context.Context, nodeID ids.NodeID, requestID uint32, containers [][]byte) {
	log := s.ctx.Log.With(
		zap.Stringer("messageOp", message.AncestorsOp),
		zap.Uint32("requestID", requestID),
		zap.Int("numContainers", len(containers)),
	)
	to := common.SendConfig{
		NodeIDs: set.Of(nodeID),
	}
	msg, err := s.msgCreator.Ancestors(s.ctx.ChainID, requestID, containers)
	s.sendUnlessError(log, to, msg, err)
}

func (s *sender) SendGet(ctx context.Context, nodeID ids.NodeID, requestID uint32, containerID ids.ID) {
	ctx = context.WithoutCancel(ctx)
	getFailed := message.InternalGetFailed(
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
		getFailed,
		p2p.EngineType_ENGINE_TYPE_UNSPECIFIED,
	)

	// Sending a Get to myself always fails.
	if nodeID == s.ctx.NodeID {
		s.router.HandleInternal(ctx, getFailed)
		return
	}

	if s.timeouts.IsBenched(nodeID, s.ctx.ChainID) {
		s.failedDueToBench.With(prometheus.Labels{
			opLabel: message.GetOp.String(),
		}).Inc()
		s.timeouts.RegisterRequestToUnreachableValidator()
		s.router.HandleInternal(ctx, getFailed)
		return
	}

	// The deadline is used as a best-effort communication to the peer for when
	// we expect the message by. It's not guaranteed to exactly match our
	// registered timeout.
	deadline := s.timeouts.TimeoutDuration()
	log := s.ctx.Log.With(
		zap.Stringer("messageOp", message.GetOp),
		zap.Uint32("requestID", requestID),
		zap.Duration("deadline", deadline),
		zap.Stringer("containerID", containerID),
	)
	to := common.SendConfig{
		NodeIDs: set.Of(nodeID),
	}
	msg, err := s.msgCreator.Get(
		s.ctx.ChainID,
		requestID,
		deadline,
		containerID,
	)
	sent := s.sendUnlessError(log, to, msg, err)
	if sent.Len() == 0 {
		s.timeouts.RegisterRequestToUnreachableValidator()
		s.router.HandleInternal(ctx, getFailed)
	}
}

func (s *sender) SendPut(_ context.Context, nodeID ids.NodeID, requestID uint32, container []byte) {
	log := s.logWith(
		zap.Binary("container", container), // only logged if verbo
		zap.Stringer("messageOp", message.PutOp),
		zap.Uint32("requestID", requestID),
	)
	to := common.SendConfig{
		NodeIDs: set.Of(nodeID),
	}
	msg, err := s.msgCreator.Put(s.ctx.ChainID, requestID, container)
	s.sendUnlessError(log, to, msg, err)
}

func (s *sender) SendPushQuery(
	ctx context.Context,
	nodeIDs set.Set[ids.NodeID],
	requestID uint32,
	container []byte,
	requestedHeight uint64,
) {
	ctx = context.WithoutCancel(ctx)
	for nodeID := range nodeIDs {
		s.router.RegisterRequest(
			ctx,
			nodeID,
			s.ctx.ChainID,
			requestID,
			message.ChitsOp,
			message.InternalQueryFailed(
				nodeID,
				s.ctx.ChainID,
				requestID,
			),
			p2p.EngineType_ENGINE_TYPE_UNSPECIFIED,
		)
	}

	// The deadline is used as a best-effort communication to the peer for when
	// we expect the message by. It's not guaranteed to exactly match our
	// registered timeout.
	deadline := s.timeouts.TimeoutDuration()
	if nodeIDs.Contains(s.ctx.NodeID) {
		nodeIDs.Remove(s.ctx.NodeID)
		s.router.HandleInternal(
			ctx,
			message.InboundPushQuery(
				s.ctx.ChainID,
				requestID,
				deadline,
				container,
				requestedHeight,
				s.ctx.NodeID,
			),
		)
	}

	for nodeID := range nodeIDs {
		if s.timeouts.IsBenched(nodeID, s.ctx.ChainID) {
			s.failedDueToBench.With(prometheus.Labels{
				opLabel: message.PushQueryOp.String(),
			}).Inc()
			nodeIDs.Remove(nodeID)
			s.timeouts.RegisterRequestToUnreachableValidator()
			s.router.HandleInternal(
				ctx,
				message.InternalQueryFailed(
					nodeID,
					s.ctx.ChainID,
					requestID,
				),
			)
		}
	}

	log := s.logWith(
		zap.Binary("container", container), // only logged if verbo
		zap.Stringer("messageOp", message.PushQueryOp),
		zap.Uint32("requestID", requestID),
		zap.Duration("deadline", deadline),
		zap.Int("containerLen", len(container)),
		zap.Uint64("requestedHeight", requestedHeight),
	)
	to := common.SendConfig{
		NodeIDs: nodeIDs,
	}
	msg, err := s.msgCreator.PushQuery(
		s.ctx.ChainID,
		requestID,
		deadline,
		container,
		requestedHeight,
	)
	sent := s.sendUnlessError(log, to, msg, err)
	for nodeID := range nodeIDs {
		if !sent.Contains(nodeID) {
			s.timeouts.RegisterRequestToUnreachableValidator()
			s.router.HandleInternal(
				ctx,
				message.InternalQueryFailed(
					nodeID,
					s.ctx.ChainID,
					requestID,
				),
			)
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
	for nodeID := range nodeIDs {
		s.router.RegisterRequest(
			ctx,
			nodeID,
			s.ctx.ChainID,
			requestID,
			message.ChitsOp,
			message.InternalQueryFailed(
				nodeID,
				s.ctx.ChainID,
				requestID,
			),
			p2p.EngineType_ENGINE_TYPE_UNSPECIFIED,
		)
	}

	// The deadline is used as a best-effort communication to the peer for when
	// we expect the message by. It's not guaranteed to exactly match our
	// registered timeout.
	deadline := s.timeouts.TimeoutDuration()
	if nodeIDs.Contains(s.ctx.NodeID) {
		nodeIDs.Remove(s.ctx.NodeID)
		s.router.HandleInternal(
			ctx,
			message.InboundPullQuery(
				s.ctx.ChainID,
				requestID,
				deadline,
				containerID,
				requestedHeight,
				s.ctx.NodeID,
			),
		)
	}

	for nodeID := range nodeIDs {
		if s.timeouts.IsBenched(nodeID, s.ctx.ChainID) {
			s.failedDueToBench.With(prometheus.Labels{
				opLabel: message.PullQueryOp.String(),
			}).Inc()
			nodeIDs.Remove(nodeID)
			s.timeouts.RegisterRequestToUnreachableValidator()
			s.router.HandleInternal(
				ctx,
				message.InternalQueryFailed(
					nodeID,
					s.ctx.ChainID,
					requestID,
				),
			)
		}
	}

	log := s.ctx.Log.With(
		zap.Stringer("messageOp", message.PullQueryOp),
		zap.Uint32("requestID", requestID),
		zap.Duration("deadline", deadline),
		zap.Stringer("containerID", containerID),
		zap.Uint64("requestedHeight", requestedHeight),
	)
	to := common.SendConfig{
		NodeIDs: nodeIDs,
	}
	msg, err := s.msgCreator.PullQuery(
		s.ctx.ChainID,
		requestID,
		deadline,
		containerID,
		requestedHeight,
	)
	sent := s.sendUnlessError(log, to, msg, err)
	for nodeID := range nodeIDs {
		if !sent.Contains(nodeID) {
			s.timeouts.RegisterRequestToUnreachableValidator()
			s.router.HandleInternal(
				ctx,
				message.InternalQueryFailed(
					nodeID,
					s.ctx.ChainID,
					requestID,
				),
			)
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
	if nodeID == s.ctx.NodeID {
		s.router.HandleInternal(
			ctx,
			message.InboundChits(
				s.ctx.ChainID,
				requestID,
				preferredID,
				preferredIDAtHeight,
				acceptedID,
				nodeID,
			),
		)
		return
	}

	log := s.ctx.Log.With(
		zap.Stringer("messageOp", message.ChitsOp),
		zap.Uint32("requestID", requestID),
		zap.Stringer("preferredID", preferredID),
		zap.Stringer("preferredIDAtHeight", preferredIDAtHeight),
		zap.Stringer("acceptedID", acceptedID),
		zap.Uint64("acceptedHeight", acceptedHeight),
	)
	to := common.SendConfig{
		NodeIDs: set.Of(nodeID),
	}
	msg, err := s.msgCreator.Chits(
		s.ctx.ChainID,
		requestID,
		preferredID,
		preferredIDAtHeight,
		acceptedID,
		acceptedHeight,
	)
	s.sendUnlessError(log, to, msg, err)
}

func (s *sender) SendAppRequest(ctx context.Context, nodeIDs set.Set[ids.NodeID], requestID uint32, bytes []byte) error {
	ctx = context.WithoutCancel(ctx)
	for nodeID := range nodeIDs {
		s.router.RegisterRequest(
			ctx,
			nodeID,
			s.ctx.ChainID,
			requestID,
			message.AppResponseOp,
			message.InboundAppError(
				nodeID,
				s.ctx.ChainID,
				requestID,
				common.ErrTimeout.Code,
				common.ErrTimeout.Message,
			),
			p2p.EngineType_ENGINE_TYPE_UNSPECIFIED,
		)
	}

	// The deadline is used as a best-effort communication to the peer for when
	// we expect the message by. It's not guaranteed to exactly match our
	// registered timeout.
	deadline := s.timeouts.TimeoutDuration()
	if nodeIDs.Contains(s.ctx.NodeID) {
		nodeIDs.Remove(s.ctx.NodeID)
		s.router.HandleInternal(
			ctx,
			message.InboundAppRequest(
				s.ctx.ChainID,
				requestID,
				deadline,
				bytes,
				s.ctx.NodeID,
			),
		)
	}

	for nodeID := range nodeIDs {
		if s.timeouts.IsBenched(nodeID, s.ctx.ChainID) {
			s.failedDueToBench.With(prometheus.Labels{
				opLabel: message.AppRequestOp.String(),
			}).Inc()
			nodeIDs.Remove(nodeID)
			s.timeouts.RegisterRequestToUnreachableValidator()
			s.router.HandleInternal(
				ctx,
				message.InboundAppError(
					nodeID,
					s.ctx.ChainID,
					requestID,
					common.ErrTimeout.Code,
					common.ErrTimeout.Message,
				),
			)
		}
	}

	log := s.logWith(
		zap.Binary("payload", bytes),
		zap.Stringer("messageOp", message.AppRequestOp),
		zap.Uint32("requestID", requestID),
		zap.Duration("deadline", deadline),
		zap.Int("payloadLen", len(bytes)),
	)
	to := common.SendConfig{
		NodeIDs: nodeIDs,
	}
	msg, err := s.msgCreator.AppRequest(
		s.ctx.ChainID,
		requestID,
		deadline,
		bytes,
	)
	sent := s.sendUnlessError(log, to, msg, err)
	for nodeID := range nodeIDs {
		if !sent.Contains(nodeID) {
			s.timeouts.RegisterRequestToUnreachableValidator()
			s.router.HandleInternal(
				ctx,
				message.InboundAppError(
					nodeID,
					s.ctx.ChainID,
					requestID,
					common.ErrTimeout.Code,
					common.ErrTimeout.Message,
				),
			)
		}
	}
	return nil
}

func (s *sender) SendAppResponse(ctx context.Context, nodeID ids.NodeID, requestID uint32, bytes []byte) error {
	ctx = context.WithoutCancel(ctx)
	if nodeID == s.ctx.NodeID {
		s.router.HandleInternal(
			ctx,
			message.InboundAppResponse(
				s.ctx.ChainID,
				requestID,
				bytes,
				nodeID,
			),
		)
		return nil
	}

	log := s.logWith(
		zap.Binary("payload", bytes),
		zap.Stringer("messageOp", message.AppResponseOp),
		zap.Uint32("requestID", requestID),
		zap.Int("payloadLen", len(bytes)),
	)
	to := common.SendConfig{
		NodeIDs: set.Of(nodeID),
	}
	msg, err := s.msgCreator.AppResponse(
		s.ctx.ChainID,
		requestID,
		bytes,
	)
	s.sendUnlessError(log, to, msg, err)
	return nil
}

func (s *sender) SendAppError(ctx context.Context, nodeID ids.NodeID, requestID uint32, errorCode int32, errorMessage string) error {
	ctx = context.WithoutCancel(ctx)
	if nodeID == s.ctx.NodeID {
		s.router.HandleInternal(
			ctx,
			message.InboundAppError(
				nodeID,
				s.ctx.ChainID,
				requestID,
				errorCode,
				errorMessage,
			),
		)
		return nil
	}

	log := s.ctx.Log.With(
		zap.Stringer("messageOp", message.AppErrorOp),
		zap.Uint32("requestID", requestID),
		zap.Int32("errorCode", errorCode),
		zap.String("errorMessage", errorMessage),
	)
	to := common.SendConfig{
		NodeIDs: set.Of(nodeID),
	}
	msg, err := s.msgCreator.AppError(
		s.ctx.ChainID,
		requestID,
		errorCode,
		errorMessage,
	)
	s.sendUnlessError(log, to, msg, err)
	return nil
}

func (s *sender) SendAppGossip(
	_ context.Context,
	to common.SendConfig,
	bytes []byte,
) error {
	log := s.logWith(
		zap.Binary("payload", bytes), // only if verbo
		zap.Stringer("messageOp", message.AppGossipOp),
	)
	msg, err := s.msgCreator.AppGossip(s.ctx.ChainID, bytes)
	s.sendUnlessError(log, to, msg, err)
	return nil
}

func (s *sender) logWith(verboField zap.Field, fields ...zap.Field) logging.Logger {
	if s.ctx.Log.Enabled(logging.Verbo) {
		fields = append(fields, verboField)
	}
	return s.ctx.Log.With(fields...)
}

func (s *sender) sendUnlessError(
	log logging.Logger,
	to common.SendConfig,
	msg *message.OutboundMessage,
	err error,
) set.Set[ids.NodeID] {
	if err != nil {
		log.Error("failed to build message",
			zap.Error(err),
		)
		return nil
	}
	sent := s.sender.Send(msg, to, s.ctx.SubnetID, s.subnet)
	log.Debug("sent message",
		zap.Reflect("to", to),
		zap.Reflect("sent", sent),
	)
	return sent
}

// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sender

import (
	"context"

	"go.opentelemetry.io/otel/attribute"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/trace"
	"github.com/ava-labs/avalanchego/utils/set"

	oteltrace "go.opentelemetry.io/otel/trace"
)

var _ common.Sender = (*tracedSender)(nil)

type tracedSender struct {
	sender common.Sender
	tracer trace.Tracer
}

func Trace(sender common.Sender, tracer trace.Tracer) common.Sender {
	return &tracedSender{
		sender: sender,
		tracer: tracer,
	}
}

func (s *tracedSender) SendGetStateSummaryFrontier(ctx context.Context, nodeIDs set.Set[ids.NodeID], requestID uint32) {
	ctx, span := s.tracer.Start(ctx, "tracedSender.SendGetStateSummaryFrontier", oteltrace.WithAttributes(
		attribute.Int64("requestID", int64(requestID)),
	))
	defer span.End()

	s.sender.SendGetStateSummaryFrontier(ctx, nodeIDs, requestID)
}

func (s *tracedSender) SendStateSummaryFrontier(ctx context.Context, nodeID ids.NodeID, requestID uint32, summary []byte) {
	ctx, span := s.tracer.Start(ctx, "tracedSender.SendStateSummaryFrontier", oteltrace.WithAttributes(
		attribute.Stringer("recipients", nodeID),
		attribute.Int64("requestID", int64(requestID)),
		attribute.Int("summaryLen", len(summary)),
	))
	defer span.End()

	s.sender.SendStateSummaryFrontier(ctx, nodeID, requestID, summary)
}

func (s *tracedSender) SendGetAcceptedStateSummary(ctx context.Context, nodeIDs set.Set[ids.NodeID], requestID uint32, heights []uint64) {
	ctx, span := s.tracer.Start(ctx, "tracedSender.SendGetAcceptedStateSummary", oteltrace.WithAttributes(
		attribute.Int64("requestID", int64(requestID)),
		attribute.Int("numHeights", len(heights)),
	))
	defer span.End()

	s.sender.SendGetAcceptedStateSummary(ctx, nodeIDs, requestID, heights)
}

func (s *tracedSender) SendAcceptedStateSummary(ctx context.Context, nodeID ids.NodeID, requestID uint32, summaryIDs []ids.ID) {
	ctx, span := s.tracer.Start(ctx, "tracedSender.SendAcceptedStateSummary", oteltrace.WithAttributes(
		attribute.Stringer("recipients", nodeID),
		attribute.Int64("requestID", int64(requestID)),
		attribute.Int("numSummaryIDs", len(summaryIDs)),
	))
	defer span.End()

	s.sender.SendAcceptedStateSummary(ctx, nodeID, requestID, summaryIDs)
}

func (s *tracedSender) SendGetAcceptedFrontier(ctx context.Context, nodeIDs set.Set[ids.NodeID], requestID uint32) {
	ctx, span := s.tracer.Start(ctx, "tracedSender.SendGetAcceptedFrontier", oteltrace.WithAttributes(
		attribute.Int64("requestID", int64(requestID)),
	))
	defer span.End()

	s.sender.SendGetAcceptedFrontier(ctx, nodeIDs, requestID)
}

func (s *tracedSender) SendAcceptedFrontier(ctx context.Context, nodeID ids.NodeID, requestID uint32, containerID ids.ID) {
	ctx, span := s.tracer.Start(ctx, "tracedSender.SendAcceptedFrontier", oteltrace.WithAttributes(
		attribute.Stringer("recipients", nodeID),
		attribute.Int64("requestID", int64(requestID)),
		attribute.Stringer("containerID", containerID),
	))
	defer span.End()

	s.sender.SendAcceptedFrontier(ctx, nodeID, requestID, containerID)
}

func (s *tracedSender) SendGetAccepted(ctx context.Context, nodeIDs set.Set[ids.NodeID], requestID uint32, containerIDs []ids.ID) {
	ctx, span := s.tracer.Start(ctx, "tracedSender.SendGetAccepted", oteltrace.WithAttributes(
		attribute.Int64("requestID", int64(requestID)),
		attribute.Int("numContainerIDs", len(containerIDs)),
	))
	defer span.End()

	s.sender.SendGetAccepted(ctx, nodeIDs, requestID, containerIDs)
}

func (s *tracedSender) SendAccepted(ctx context.Context, nodeID ids.NodeID, requestID uint32, containerIDs []ids.ID) {
	ctx, span := s.tracer.Start(ctx, "tracedSender.SendAccepted", oteltrace.WithAttributes(
		attribute.Stringer("recipients", nodeID),
		attribute.Int64("requestID", int64(requestID)),
		attribute.Int("numContainerIDs", len(containerIDs)),
	))
	defer span.End()

	s.sender.SendAccepted(ctx, nodeID, requestID, containerIDs)
}

func (s *tracedSender) SendGetAncestors(ctx context.Context, nodeID ids.NodeID, requestID uint32, containerID ids.ID) {
	ctx, span := s.tracer.Start(ctx, "tracedSender.SendGetAncestors", oteltrace.WithAttributes(
		attribute.Stringer("recipients", nodeID),
		attribute.Int64("requestID", int64(requestID)),
		attribute.Stringer("containerID", containerID),
	))
	defer span.End()

	s.sender.SendGetAncestors(ctx, nodeID, requestID, containerID)
}

func (s *tracedSender) SendAncestors(ctx context.Context, nodeID ids.NodeID, requestID uint32, containers [][]byte) {
	_, span := s.tracer.Start(ctx, "tracedSender.SendAncestors", oteltrace.WithAttributes(
		attribute.Stringer("recipients", nodeID),
		attribute.Int64("requestID", int64(requestID)),
		attribute.Int("numContainers", len(containers)),
	))
	defer span.End()

	s.sender.SendAncestors(ctx, nodeID, requestID, containers)
}

func (s *tracedSender) SendGet(ctx context.Context, nodeID ids.NodeID, requestID uint32, containerID ids.ID) {
	ctx, span := s.tracer.Start(ctx, "tracedSender.SendGet", oteltrace.WithAttributes(
		attribute.Stringer("recipients", nodeID),
		attribute.Int64("requestID", int64(requestID)),
		attribute.Stringer("containerID", containerID),
	))
	defer span.End()

	s.sender.SendGet(ctx, nodeID, requestID, containerID)
}

func (s *tracedSender) SendPut(ctx context.Context, nodeID ids.NodeID, requestID uint32, container []byte) {
	_, span := s.tracer.Start(ctx, "tracedSender.SendPut", oteltrace.WithAttributes(
		attribute.Stringer("recipients", nodeID),
		attribute.Int64("requestID", int64(requestID)),
		attribute.Int("containerLen", len(container)),
	))
	defer span.End()

	s.sender.SendPut(ctx, nodeID, requestID, container)
}

func (s *tracedSender) SendPushQuery(ctx context.Context, nodeIDs set.Set[ids.NodeID], requestID uint32, container []byte, requestedHeight uint64) {
	ctx, span := s.tracer.Start(ctx, "tracedSender.SendPushQuery", oteltrace.WithAttributes(
		attribute.Int64("requestID", int64(requestID)),
		attribute.Int("containerLen", len(container)),
		attribute.Int64("requestedHeight", int64(requestedHeight)),
	))
	defer span.End()

	s.sender.SendPushQuery(ctx, nodeIDs, requestID, container, requestedHeight)
}

func (s *tracedSender) SendPullQuery(ctx context.Context, nodeIDs set.Set[ids.NodeID], requestID uint32, containerID ids.ID, requestedHeight uint64) {
	ctx, span := s.tracer.Start(ctx, "tracedSender.SendPullQuery", oteltrace.WithAttributes(
		attribute.Int64("requestID", int64(requestID)),
		attribute.Stringer("containerID", containerID),
		attribute.Int64("requestedHeight", int64(requestedHeight)),
	))
	defer span.End()

	s.sender.SendPullQuery(ctx, nodeIDs, requestID, containerID, requestedHeight)
}

func (s *tracedSender) SendChits(ctx context.Context, nodeID ids.NodeID, requestID uint32, preferredID ids.ID, preferredIDAtHeight ids.ID, acceptedID ids.ID, acceptedHeight uint64) {
	ctx, span := s.tracer.Start(ctx, "tracedSender.SendChits", oteltrace.WithAttributes(
		attribute.Stringer("recipients", nodeID),
		attribute.Int64("requestID", int64(requestID)),
		attribute.Stringer("preferredID", preferredID),
		attribute.Stringer("preferredIDAtHeight", preferredIDAtHeight),
		attribute.Stringer("acceptedID", acceptedID),
		attribute.Int("acceptedHeight", int(acceptedHeight)),
	))
	defer span.End()

	s.sender.SendChits(ctx, nodeID, requestID, preferredID, preferredIDAtHeight, acceptedID, acceptedHeight)
}

func (s *tracedSender) SendAppRequest(ctx context.Context, nodeIDs set.Set[ids.NodeID], requestID uint32, appRequestBytes []byte) error {
	ctx, span := s.tracer.Start(ctx, "tracedSender.SendAppRequest", oteltrace.WithAttributes(
		attribute.Int64("requestID", int64(requestID)),
		attribute.Int("requestLen", len(appRequestBytes)),
	))
	defer span.End()

	return s.sender.SendAppRequest(ctx, nodeIDs, requestID, appRequestBytes)
}

func (s *tracedSender) SendAppResponse(ctx context.Context, nodeID ids.NodeID, requestID uint32, appResponseBytes []byte) error {
	ctx, span := s.tracer.Start(ctx, "tracedSender.SendAppResponse", oteltrace.WithAttributes(
		attribute.Stringer("recipients", nodeID),
		attribute.Int64("requestID", int64(requestID)),
		attribute.Int("responseLen", len(appResponseBytes)),
	))
	defer span.End()

	return s.sender.SendAppResponse(ctx, nodeID, requestID, appResponseBytes)
}

func (s *tracedSender) SendAppError(ctx context.Context, nodeID ids.NodeID, requestID uint32, errorCode int32, errorMessage string) error {
	ctx, span := s.tracer.Start(ctx, "tracedSender.SendAppError", oteltrace.WithAttributes(
		attribute.Stringer("nodeID", nodeID),
		attribute.Int64("requestID", int64(requestID)),
		attribute.Int64("errorCode", int64(errorCode)),
		attribute.String("errorMessage", errorMessage),
	))
	defer span.End()

	return s.sender.SendAppError(ctx, nodeID, requestID, errorCode, errorMessage)
}

func (s *tracedSender) SendAppGossip(
	ctx context.Context,
	config common.SendConfig,
	appGossipBytes []byte,
) error {
	_, span := s.tracer.Start(ctx, "tracedSender.SendAppGossip", oteltrace.WithAttributes(
		attribute.Int("numNodeIDs", config.NodeIDs.Len()),
		attribute.Int("numValidators", config.Validators),
		attribute.Int("numNonValidators", config.NonValidators),
		attribute.Int("numPeers", config.Peers),
		attribute.Int("gossipLen", len(appGossipBytes)),
	))
	defer span.End()

	return s.sender.SendAppGossip(
		ctx,
		config,
		appGossipBytes,
	)
}

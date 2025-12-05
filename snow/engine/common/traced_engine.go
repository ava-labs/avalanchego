// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package common

import (
	"context"
	"time"

	"go.opentelemetry.io/otel/attribute"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/proto/pb/p2p"
	"github.com/ava-labs/avalanchego/trace"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/version"

	oteltrace "go.opentelemetry.io/otel/trace"
)

var _ Engine = (*tracedEngine)(nil)

type tracedEngine struct {
	engine Engine
	tracer trace.Tracer
}

func TraceEngine(engine Engine, tracer trace.Tracer) Engine {
	return &tracedEngine{
		engine: engine,
		tracer: tracer,
	}
}

func (e *tracedEngine) GetStateSummaryFrontier(ctx context.Context, nodeID ids.NodeID, requestID uint32) error {
	ctx, span := e.tracer.Start(ctx, "tracedEngine.GetStateSummaryFrontier", oteltrace.WithAttributes(
		attribute.Stringer("nodeID", nodeID),
		attribute.Int64("requestID", int64(requestID)),
	))
	defer span.End()

	return e.engine.GetStateSummaryFrontier(ctx, nodeID, requestID)
}

func (e *tracedEngine) StateSummaryFrontier(ctx context.Context, nodeID ids.NodeID, requestID uint32, summary []byte) error {
	ctx, span := e.tracer.Start(ctx, "tracedEngine.StateSummaryFrontier", oteltrace.WithAttributes(
		attribute.Stringer("nodeID", nodeID),
		attribute.Int64("requestID", int64(requestID)),
		attribute.Int("summaryLen", len(summary)),
	))
	defer span.End()

	return e.engine.StateSummaryFrontier(ctx, nodeID, requestID, summary)
}

func (e *tracedEngine) GetStateSummaryFrontierFailed(ctx context.Context, nodeID ids.NodeID, requestID uint32) error {
	ctx, span := e.tracer.Start(ctx, "tracedEngine.GetStateSummaryFrontierFailed", oteltrace.WithAttributes(
		attribute.Stringer("nodeID", nodeID),
		attribute.Int64("requestID", int64(requestID)),
	))
	defer span.End()

	return e.engine.GetStateSummaryFrontierFailed(ctx, nodeID, requestID)
}

func (e *tracedEngine) GetAcceptedStateSummary(ctx context.Context, nodeID ids.NodeID, requestID uint32, heights set.Set[uint64]) error {
	ctx, span := e.tracer.Start(ctx, "tracedEngine.GetAcceptedStateSummary", oteltrace.WithAttributes(
		attribute.Stringer("nodeID", nodeID),
		attribute.Int64("requestID", int64(requestID)),
		attribute.Int("numHeights", heights.Len()),
	))
	defer span.End()

	return e.engine.GetAcceptedStateSummary(ctx, nodeID, requestID, heights)
}

func (e *tracedEngine) AcceptedStateSummary(ctx context.Context, nodeID ids.NodeID, requestID uint32, summaryIDs set.Set[ids.ID]) error {
	ctx, span := e.tracer.Start(ctx, "tracedEngine.AcceptedStateSummary", oteltrace.WithAttributes(
		attribute.Stringer("nodeID", nodeID),
		attribute.Int64("requestID", int64(requestID)),
		attribute.Int("numSummaryIDs", summaryIDs.Len()),
	))
	defer span.End()

	return e.engine.AcceptedStateSummary(ctx, nodeID, requestID, summaryIDs)
}

func (e *tracedEngine) GetAcceptedStateSummaryFailed(ctx context.Context, nodeID ids.NodeID, requestID uint32) error {
	ctx, span := e.tracer.Start(ctx, "tracedEngine.GetAcceptedStateSummaryFailed", oteltrace.WithAttributes(
		attribute.Stringer("nodeID", nodeID),
		attribute.Int64("requestID", int64(requestID)),
	))
	defer span.End()

	return e.engine.GetAcceptedStateSummaryFailed(ctx, nodeID, requestID)
}

func (e *tracedEngine) GetAcceptedFrontier(ctx context.Context, nodeID ids.NodeID, requestID uint32) error {
	ctx, span := e.tracer.Start(ctx, "tracedEngine.GetAcceptedFrontier", oteltrace.WithAttributes(
		attribute.Stringer("nodeID", nodeID),
		attribute.Int64("requestID", int64(requestID)),
	))
	defer span.End()

	return e.engine.GetAcceptedFrontier(ctx, nodeID, requestID)
}

func (e *tracedEngine) AcceptedFrontier(ctx context.Context, nodeID ids.NodeID, requestID uint32, containerID ids.ID) error {
	ctx, span := e.tracer.Start(ctx, "tracedEngine.AcceptedFrontier", oteltrace.WithAttributes(
		attribute.Stringer("nodeID", nodeID),
		attribute.Int64("requestID", int64(requestID)),
		attribute.Stringer("containerID", containerID),
	))
	defer span.End()

	return e.engine.AcceptedFrontier(ctx, nodeID, requestID, containerID)
}

func (e *tracedEngine) GetAcceptedFrontierFailed(ctx context.Context, nodeID ids.NodeID, requestID uint32) error {
	ctx, span := e.tracer.Start(ctx, "tracedEngine.GetAcceptedFrontierFailed", oteltrace.WithAttributes(
		attribute.Stringer("nodeID", nodeID),
		attribute.Int64("requestID", int64(requestID)),
	))
	defer span.End()

	return e.engine.GetAcceptedFrontierFailed(ctx, nodeID, requestID)
}

func (e *tracedEngine) GetAccepted(ctx context.Context, nodeID ids.NodeID, requestID uint32, containerIDs set.Set[ids.ID]) error {
	ctx, span := e.tracer.Start(ctx, "tracedEngine.GetAccepted", oteltrace.WithAttributes(
		attribute.Stringer("nodeID", nodeID),
		attribute.Int64("requestID", int64(requestID)),
		attribute.Int("numContainerIDs", containerIDs.Len()),
	))
	defer span.End()

	return e.engine.GetAccepted(ctx, nodeID, requestID, containerIDs)
}

func (e *tracedEngine) Accepted(ctx context.Context, nodeID ids.NodeID, requestID uint32, containerIDs set.Set[ids.ID]) error {
	ctx, span := e.tracer.Start(ctx, "tracedEngine.Accepted", oteltrace.WithAttributes(
		attribute.Stringer("nodeID", nodeID),
		attribute.Int64("requestID", int64(requestID)),
		attribute.Int("numContainerIDs", containerIDs.Len()),
	))
	defer span.End()

	return e.engine.Accepted(ctx, nodeID, requestID, containerIDs)
}

func (e *tracedEngine) GetAcceptedFailed(ctx context.Context, nodeID ids.NodeID, requestID uint32) error {
	ctx, span := e.tracer.Start(ctx, "tracedEngine.GetAcceptedFailed", oteltrace.WithAttributes(
		attribute.Stringer("nodeID", nodeID),
		attribute.Int64("requestID", int64(requestID)),
	))
	defer span.End()

	return e.engine.GetAcceptedFailed(ctx, nodeID, requestID)
}

func (e *tracedEngine) GetAncestors(ctx context.Context, nodeID ids.NodeID, requestID uint32, containerID ids.ID) error {
	ctx, span := e.tracer.Start(ctx, "tracedEngine.GetAncestors", oteltrace.WithAttributes(
		attribute.Stringer("nodeID", nodeID),
		attribute.Int64("requestID", int64(requestID)),
		attribute.Stringer("containerID", containerID),
	))
	defer span.End()

	return e.engine.GetAncestors(ctx, nodeID, requestID, containerID)
}

func (e *tracedEngine) Ancestors(ctx context.Context, nodeID ids.NodeID, requestID uint32, containers [][]byte) error {
	ctx, span := e.tracer.Start(ctx, "tracedEngine.Ancestors", oteltrace.WithAttributes(
		attribute.Stringer("nodeID", nodeID),
		attribute.Int64("requestID", int64(requestID)),
		attribute.Int("numContainers", len(containers)),
	))
	defer span.End()

	return e.engine.Ancestors(ctx, nodeID, requestID, containers)
}

func (e *tracedEngine) GetAncestorsFailed(ctx context.Context, nodeID ids.NodeID, requestID uint32) error {
	ctx, span := e.tracer.Start(ctx, "tracedEngine.GetAncestorsFailed", oteltrace.WithAttributes(
		attribute.Stringer("nodeID", nodeID),
		attribute.Int64("requestID", int64(requestID)),
	))
	defer span.End()

	return e.engine.GetAncestorsFailed(ctx, nodeID, requestID)
}

func (e *tracedEngine) Get(ctx context.Context, nodeID ids.NodeID, requestID uint32, containerID ids.ID) error {
	ctx, span := e.tracer.Start(ctx, "tracedEngine.Get", oteltrace.WithAttributes(
		attribute.Stringer("nodeID", nodeID),
		attribute.Int64("requestID", int64(requestID)),
		attribute.Stringer("containerID", containerID),
	))
	defer span.End()

	return e.engine.Get(ctx, nodeID, requestID, containerID)
}

func (e *tracedEngine) Put(ctx context.Context, nodeID ids.NodeID, requestID uint32, container []byte) error {
	ctx, span := e.tracer.Start(ctx, "tracedEngine.Put", oteltrace.WithAttributes(
		attribute.Stringer("nodeID", nodeID),
		attribute.Int64("requestID", int64(requestID)),
		attribute.Int("containerLen", len(container)),
	))
	defer span.End()

	return e.engine.Put(ctx, nodeID, requestID, container)
}

func (e *tracedEngine) GetFailed(ctx context.Context, nodeID ids.NodeID, requestID uint32) error {
	ctx, span := e.tracer.Start(ctx, "tracedEngine.GetFailed", oteltrace.WithAttributes(
		attribute.Stringer("nodeID", nodeID),
		attribute.Int64("requestID", int64(requestID)),
	))
	defer span.End()

	return e.engine.GetFailed(ctx, nodeID, requestID)
}

func (e *tracedEngine) PullQuery(ctx context.Context, nodeID ids.NodeID, requestID uint32, containerID ids.ID, requestedHeight uint64) error {
	ctx, span := e.tracer.Start(ctx, "tracedEngine.PullQuery", oteltrace.WithAttributes(
		attribute.Stringer("nodeID", nodeID),
		attribute.Int64("requestID", int64(requestID)),
		attribute.Stringer("containerID", containerID),
		attribute.Int64("requestedHeight", int64(requestedHeight)),
	))
	defer span.End()

	return e.engine.PullQuery(ctx, nodeID, requestID, containerID, requestedHeight)
}

func (e *tracedEngine) PushQuery(ctx context.Context, nodeID ids.NodeID, requestID uint32, container []byte, requestedHeight uint64) error {
	ctx, span := e.tracer.Start(ctx, "tracedEngine.PushQuery", oteltrace.WithAttributes(
		attribute.Stringer("nodeID", nodeID),
		attribute.Int64("requestID", int64(requestID)),
		attribute.Int("containerLen", len(container)),
		attribute.Int64("requestedHeight", int64(requestedHeight)),
	))
	defer span.End()

	return e.engine.PushQuery(ctx, nodeID, requestID, container, requestedHeight)
}

func (e *tracedEngine) Simplex(ctx context.Context, nodeID ids.NodeID, msg *p2p.Simplex) error {
	ctx, span := e.tracer.Start(ctx, "tracedEngine.SimplexMessage", oteltrace.WithAttributes(
		attribute.Stringer("nodeID", nodeID),
	))
	defer span.End()

	return e.engine.Simplex(ctx, nodeID, msg)
}

func (e *tracedEngine) Chits(ctx context.Context, nodeID ids.NodeID, requestID uint32, preferredID ids.ID, preferredIDAtHeight ids.ID, acceptedID ids.ID, acceptedHeight uint64) error {
	ctx, span := e.tracer.Start(ctx, "tracedEngine.Chits", oteltrace.WithAttributes(
		attribute.Stringer("nodeID", nodeID),
		attribute.Int64("requestID", int64(requestID)),
		attribute.Stringer("preferredID", preferredID),
		attribute.Stringer("preferredIDAtHeight", preferredIDAtHeight),
		attribute.Stringer("acceptedID", acceptedID),
	))
	defer span.End()

	return e.engine.Chits(ctx, nodeID, requestID, preferredID, preferredIDAtHeight, acceptedID, acceptedHeight)
}

func (e *tracedEngine) QueryFailed(ctx context.Context, nodeID ids.NodeID, requestID uint32) error {
	ctx, span := e.tracer.Start(ctx, "tracedEngine.QueryFailed", oteltrace.WithAttributes(
		attribute.Stringer("nodeID", nodeID),
		attribute.Int64("requestID", int64(requestID)),
	))
	defer span.End()

	return e.engine.QueryFailed(ctx, nodeID, requestID)
}

func (e *tracedEngine) AppRequest(ctx context.Context, nodeID ids.NodeID, requestID uint32, deadline time.Time, request []byte) error {
	ctx, span := e.tracer.Start(ctx, "tracedEngine.AppRequest", oteltrace.WithAttributes(
		attribute.Stringer("nodeID", nodeID),
		attribute.Int64("requestID", int64(requestID)),
		attribute.Int("requestLen", len(request)),
	))
	defer span.End()

	return e.engine.AppRequest(ctx, nodeID, requestID, deadline, request)
}

func (e *tracedEngine) AppResponse(ctx context.Context, nodeID ids.NodeID, requestID uint32, response []byte) error {
	ctx, span := e.tracer.Start(ctx, "tracedEngine.AppResponse", oteltrace.WithAttributes(
		attribute.Stringer("nodeID", nodeID),
		attribute.Int64("requestID", int64(requestID)),
		attribute.Int("responseLen", len(response)),
	))
	defer span.End()

	return e.engine.AppResponse(ctx, nodeID, requestID, response)
}

func (e *tracedEngine) AppRequestFailed(ctx context.Context, nodeID ids.NodeID, requestID uint32, appErr *AppError) error {
	ctx, span := e.tracer.Start(ctx, "tracedEngine.AppRequestFailed", oteltrace.WithAttributes(
		attribute.Stringer("nodeID", nodeID),
		attribute.Int64("requestID", int64(requestID)),
	))
	defer span.End()

	return e.engine.AppRequestFailed(ctx, nodeID, requestID, appErr)
}

func (e *tracedEngine) AppGossip(ctx context.Context, nodeID ids.NodeID, msg []byte) error {
	ctx, span := e.tracer.Start(ctx, "tracedEngine.AppGossip", oteltrace.WithAttributes(
		attribute.Stringer("nodeID", nodeID),
		attribute.Int("gossipLen", len(msg)),
	))
	defer span.End()

	return e.engine.AppGossip(ctx, nodeID, msg)
}

func (e *tracedEngine) Connected(ctx context.Context, nodeID ids.NodeID, nodeVersion *version.Application) error {
	ctx, span := e.tracer.Start(ctx, "tracedEngine.Connected", oteltrace.WithAttributes(
		attribute.Stringer("nodeID", nodeID),
		attribute.Stringer("version", nodeVersion),
	))
	defer span.End()

	return e.engine.Connected(ctx, nodeID, nodeVersion)
}

func (e *tracedEngine) Disconnected(ctx context.Context, nodeID ids.NodeID) error {
	ctx, span := e.tracer.Start(ctx, "tracedEngine.Disconnected", oteltrace.WithAttributes(
		attribute.Stringer("nodeID", nodeID),
	))
	defer span.End()

	return e.engine.Disconnected(ctx, nodeID)
}

func (e *tracedEngine) Gossip(ctx context.Context) error {
	ctx, span := e.tracer.Start(ctx, "tracedEngine.Gossip")
	defer span.End()

	return e.engine.Gossip(ctx)
}

func (e *tracedEngine) Shutdown(ctx context.Context) error {
	ctx, span := e.tracer.Start(ctx, "tracedEngine.Shutdown")
	defer span.End()

	return e.engine.Shutdown(ctx)
}

func (e *tracedEngine) Notify(ctx context.Context, msg Message) error {
	ctx, span := e.tracer.Start(ctx, "tracedEngine.Notify", oteltrace.WithAttributes(
		attribute.Stringer("message", msg),
	))
	defer span.End()

	return e.engine.Notify(ctx, msg)
}

func (e *tracedEngine) Start(ctx context.Context, startReqID uint32) error {
	ctx, span := e.tracer.Start(ctx, "tracedEngine.Start", oteltrace.WithAttributes(
		attribute.Int64("requestID", int64(startReqID)),
	))
	defer span.End()

	return e.engine.Start(ctx, startReqID)
}

func (e *tracedEngine) HealthCheck(ctx context.Context) (interface{}, error) {
	ctx, span := e.tracer.Start(ctx, "tracedEngine.HealthCheck")
	defer span.End()

	return e.engine.HealthCheck(ctx)
}

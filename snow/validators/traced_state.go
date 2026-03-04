// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package validators

import (
	"context"

	"go.opentelemetry.io/otel/attribute"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/trace"

	oteltrace "go.opentelemetry.io/otel/trace"
)

var _ State = (*tracedState)(nil)

type tracedState struct {
	s                         State
	getMinimumHeightTag       string
	getCurrentHeightTag       string
	getSubnetIDTag            string
	getWarpValidatorSetsTag   string
	getWarpValidatorSetTag    string
	getValidatorSetTag        string
	getCurrentValidatorSetTag string
	tracer                    trace.Tracer
}

func Trace(s State, name string, tracer trace.Tracer) State {
	return &tracedState{
		s:                         s,
		getMinimumHeightTag:       name + ".GetMinimumHeight",
		getCurrentHeightTag:       name + ".GetCurrentHeight",
		getSubnetIDTag:            name + ".GetSubnetID",
		getWarpValidatorSetsTag:   name + ".GetWarpValidatorSets",
		getWarpValidatorSetTag:    name + ".GetWarpValidatorSet",
		getValidatorSetTag:        name + ".GetValidatorSet",
		getCurrentValidatorSetTag: name + ".GetCurrentValidatorSet",
		tracer:                    tracer,
	}
}

func (s *tracedState) GetMinimumHeight(ctx context.Context) (uint64, error) {
	ctx, span := s.tracer.Start(ctx, s.getMinimumHeightTag)
	defer span.End()

	return s.s.GetMinimumHeight(ctx)
}

func (s *tracedState) GetCurrentHeight(ctx context.Context) (uint64, error) {
	ctx, span := s.tracer.Start(ctx, s.getCurrentHeightTag)
	defer span.End()

	return s.s.GetCurrentHeight(ctx)
}

func (s *tracedState) GetSubnetID(ctx context.Context, chainID ids.ID) (ids.ID, error) {
	ctx, span := s.tracer.Start(ctx, s.getSubnetIDTag, oteltrace.WithAttributes(
		attribute.Stringer("chainID", chainID),
	))
	defer span.End()

	return s.s.GetSubnetID(ctx, chainID)
}

func (s *tracedState) GetWarpValidatorSets(
	ctx context.Context,
	height uint64,
) (map[ids.ID]WarpSet, error) {
	ctx, span := s.tracer.Start(ctx, s.getWarpValidatorSetsTag, oteltrace.WithAttributes(
		attribute.Int64("height", int64(height)),
	))
	defer span.End()

	return s.s.GetWarpValidatorSets(ctx, height)
}

func (s *tracedState) GetWarpValidatorSet(
	ctx context.Context,
	height uint64,
	subnetID ids.ID,
) (WarpSet, error) {
	ctx, span := s.tracer.Start(ctx, s.getWarpValidatorSetTag, oteltrace.WithAttributes(
		attribute.Int64("height", int64(height)),
		attribute.Stringer("subnetID", subnetID),
	))
	defer span.End()

	return s.s.GetWarpValidatorSet(ctx, height, subnetID)
}

func (s *tracedState) GetValidatorSet(
	ctx context.Context,
	height uint64,
	subnetID ids.ID,
) (map[ids.NodeID]*GetValidatorOutput, error) {
	ctx, span := s.tracer.Start(ctx, s.getValidatorSetTag, oteltrace.WithAttributes(
		attribute.Int64("height", int64(height)),
		attribute.Stringer("subnetID", subnetID),
	))
	defer span.End()

	return s.s.GetValidatorSet(ctx, height, subnetID)
}

func (s *tracedState) GetCurrentValidatorSet(
	ctx context.Context,
	subnetID ids.ID,
) (map[ids.ID]*GetCurrentValidatorOutput, uint64, error) {
	ctx, span := s.tracer.Start(ctx, s.getCurrentValidatorSetTag, oteltrace.WithAttributes(
		attribute.Stringer("subnetID", subnetID),
	))
	defer span.End()

	return s.s.GetCurrentValidatorSet(ctx, subnetID)
}

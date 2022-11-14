// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package validators

import (
	"context"
	"fmt"

	"go.opentelemetry.io/otel/attribute"

	oteltrace "go.opentelemetry.io/otel/trace"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/trace"
)

var _ State = (*tracedState)(nil)

type tracedState struct {
	s                State
	getMinimumHeight string
	getCurrentHeight string
	getValidatorSet  string
	tracer           trace.Tracer
}

func Trace(s State, name string, tracer trace.Tracer) State {
	return &tracedState{
		s:                s,
		getMinimumHeight: fmt.Sprintf("%s.GetMinimumHeight", name),
		getCurrentHeight: fmt.Sprintf("%s.GetCurrentHeight", name),
		getValidatorSet:  fmt.Sprintf("%s.GetValidatorSet", name),
		tracer:           tracer,
	}
}

func (s *tracedState) GetMinimumHeight(ctx context.Context) (uint64, error) {
	ctx, span := s.tracer.Start(ctx, s.getMinimumHeight)
	defer span.End()

	return s.s.GetMinimumHeight(ctx)
}

func (s *tracedState) GetCurrentHeight(ctx context.Context) (uint64, error) {
	ctx, span := s.tracer.Start(ctx, s.getCurrentHeight)
	defer span.End()

	return s.s.GetCurrentHeight(ctx)
}

func (s *tracedState) GetValidatorSet(ctx context.Context, height uint64, subnetID ids.ID) (map[ids.NodeID]uint64, error) {
	ctx, span := s.tracer.Start(ctx, s.getValidatorSet, oteltrace.WithAttributes(
		attribute.Int64("height", int64(height)),
		attribute.Stringer("subnetID", subnetID),
	))
	defer span.End()

	return s.s.GetValidatorSet(ctx, height, subnetID)
}

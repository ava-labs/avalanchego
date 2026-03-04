// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package common

import (
	"context"

	"github.com/ava-labs/avalanchego/trace"
)

var _ StateSyncer = (*tracedStateSyncer)(nil)

type tracedStateSyncer struct {
	Engine
	stateSyncer StateSyncer
	tracer      trace.Tracer
}

func TraceStateSyncer(stateSyncer StateSyncer, tracer trace.Tracer) StateSyncer {
	return &tracedStateSyncer{
		Engine:      TraceEngine(stateSyncer, tracer),
		stateSyncer: stateSyncer,
		tracer:      tracer,
	}
}

func (e *tracedStateSyncer) IsEnabled(ctx context.Context) (bool, error) {
	ctx, span := e.tracer.Start(ctx, "tracedStateSyncer.IsEnabled")
	defer span.End()

	return e.stateSyncer.IsEnabled(ctx)
}

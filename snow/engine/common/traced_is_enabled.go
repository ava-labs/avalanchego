// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package common

import (
	"context"

	"github.com/ava-labs/avalanchego/trace"
)

type Enabler interface {
	IsEnabled(ctx context.Context) (bool, error)
}

type tracedIsEnabled struct {
	backend Enabler
	tracer  trace.Tracer
}

func NewTracedIsEnabled(backend Enabler, tracer trace.Tracer) Enabler {
	return &tracedIsEnabled{
		backend: backend,
		tracer:  tracer,
	}
}

func (e *tracedIsEnabled) IsEnabled(ctx context.Context) (bool, error) {
	ctx, span := e.tracer.Start(ctx, "tracedIsEnabled.IsEnabled")
	defer span.End()

	return e.backend.IsEnabled(ctx)
}

// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package common

import (
	"context"

	"github.com/ava-labs/avalanchego/trace"
)

type Clearer interface {
	Clear(ctx context.Context) error
}

type tracedClearer struct {
	backend Clearer
	tracer  trace.Tracer
}

func (c *tracedClearer) Clear(ctx context.Context) error {
	ctx, span := c.tracer.Start(ctx, "tracedClearer.Clear")
	defer span.End()

	return c.backend.Clear(ctx)
}

func NewTracedClearer(backend Clearer, tracer trace.Tracer) Clearer {
	return &tracedClearer{
		backend: backend,
		tracer:  tracer,
	}
}

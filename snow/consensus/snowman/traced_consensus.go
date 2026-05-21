// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package snowman

import (
	"context"

	"go.opentelemetry.io/otel/attribute"

	"github.com/MetalBlockchain/metalgo/ids"
	"github.com/MetalBlockchain/metalgo/trace"
	"github.com/MetalBlockchain/metalgo/utils/bag"

	oteltrace "go.opentelemetry.io/otel/trace"
)

var _ Consensus = (*tracedConsensus)(nil)

type tracedConsensus struct {
	Consensus
	tracer trace.Tracer
}

func Trace(consensus Consensus, tracer trace.Tracer) Consensus {
	return &tracedConsensus{
		Consensus: consensus,
		tracer:    tracer,
	}
}

func (c *tracedConsensus) RecordPoll(ctx context.Context, votes bag.Bag[ids.ID]) error {
	ctx, span := c.tracer.Start(ctx, "tracedConsensus.RecordPoll", oteltrace.WithAttributes(
		attribute.Int("numVotes", votes.Len()),
		attribute.Int("numBlkIDs", len(votes.List())),
	))
	defer span.End()

	return c.Consensus.RecordPoll(ctx, votes)
}

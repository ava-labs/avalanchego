// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package snowman

import (
	"context"

	"go.opentelemetry.io/otel/attribute"

	oteltrace "go.opentelemetry.io/otel/trace"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/trace"
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

func (c *tracedConsensus) Add(ctx context.Context, blk Block) error {
	ctx, span := c.tracer.Start(ctx, "tracedConsensus.Add", oteltrace.WithAttributes(
		attribute.Stringer("blkID", blk.ID()),
		attribute.Int64("height", int64(blk.Height())),
	))
	defer span.End()

	return c.Consensus.Add(ctx, blk)
}

func (c *tracedConsensus) RecordPoll(ctx context.Context, votes ids.Bag) error {
	ctx, span := c.tracer.Start(ctx, "tracedConsensus.RecordPoll", oteltrace.WithAttributes(
		attribute.Int("numVotes", votes.Len()),
		attribute.Int("numBlkIDs", len(votes.List())),
	))
	defer span.End()

	return c.Consensus.RecordPoll(ctx, votes)
}

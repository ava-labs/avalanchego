// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package bootstrap

import (
	"context"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
)

var (
	_ Appraiser     = (*appraiseAcceptor)(nil)
	_ snowman.Block = (*blockAcceptor)(nil)
)

type appraiseAcceptor struct {
	appraiser   Appraiser
	ctx         *snow.ConsensusContext
	numAccepted prometheus.Counter
}

func (a *appraiseAcceptor) AppraiseBlock(ctx context.Context, bytes []byte) (snowman.Block, error) {
	blk, err := a.appraiser.AppraiseBlock(ctx, bytes)
	if err != nil {
		return nil, err
	}
	return &blockAcceptor{
		Block:       blk,
		ctx:         a.ctx,
		numAccepted: a.numAccepted,
	}, nil
}

type blockAcceptor struct {
	snowman.Block

	ctx         *snow.ConsensusContext
	numAccepted prometheus.Counter
}

func (b *blockAcceptor) Accept(ctx context.Context) error {
	if err := b.ctx.BlockAcceptor.Accept(b.ctx, b.ID(), b.Bytes()); err != nil {
		return err
	}
	err := b.Block.Accept(ctx)
	b.numAccepted.Inc()
	return err
}

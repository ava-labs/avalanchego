// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package avalanche

import (
	"context"
	"errors"

	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/engine/common"
)

var (
	_ common.Engine = (*engine)(nil)

	errUnexpectedStart = errors.New("unexpectedly started engine")
)

type engine struct {
	common.AllGetsServer

	// list of NoOpsHandler for messages dropped by engine
	common.StateSummaryFrontierHandler
	common.AcceptedStateSummaryHandler
	common.AcceptedFrontierHandler
	common.AcceptedHandler
	common.AncestorsHandler
	common.PutHandler
	common.QueryHandler
	common.ChitsHandler
	common.AppHandler
	common.InternalHandler
	common.SimplexHandler

	ctx *snow.ConsensusContext
}

func New(
	ctx *snow.ConsensusContext,
	gets common.AllGetsServer,
) common.Engine {
	return &engine{
		AllGetsServer:               gets,
		StateSummaryFrontierHandler: common.NewNoOpStateSummaryFrontierHandler(ctx.Log),
		AcceptedStateSummaryHandler: common.NewNoOpAcceptedStateSummaryHandler(ctx.Log),
		AcceptedFrontierHandler:     common.NewNoOpAcceptedFrontierHandler(ctx.Log),
		AcceptedHandler:             common.NewNoOpAcceptedHandler(ctx.Log),
		AncestorsHandler:            common.NewNoOpAncestorsHandler(ctx.Log),
		PutHandler:                  common.NewNoOpPutHandler(ctx.Log),
		QueryHandler:                common.NewNoOpQueryHandler(ctx.Log),
		ChitsHandler:                common.NewNoOpChitsHandler(ctx.Log),
		AppHandler:                  common.NewNoOpAppHandler(ctx.Log),
		InternalHandler:             common.NewNoOpInternalHandler(ctx.Log),
		SimplexHandler:              common.NewNoOpSimplexHandler(ctx.Log),
		ctx:                         ctx,
	}
}

func (*engine) Start(context.Context, uint32) error {
	return errUnexpectedStart
}

func (e *engine) Context() *snow.ConsensusContext {
	return e.ctx
}

func (*engine) HealthCheck(context.Context) (interface{}, error) {
	return nil, nil
}

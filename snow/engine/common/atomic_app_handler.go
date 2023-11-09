// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package common

import (
	"context"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils"
)

var _ AtomicAppHandler = (*atomicAppHandler)(nil)

type AtomicAppHandler interface {
	AppHandler

	Set(AppHandler)
}

type atomicAppHandler struct {
	handler utils.Atomic[AppHandler]
}

func NewAtomicAppHandler(h AppHandler) AtomicAppHandler {
	a := &atomicAppHandler{}
	a.handler.Set(h)
	return a
}

func (a *atomicAppHandler) CrossChainAppRequest(
	ctx context.Context,
	chainID ids.ID,
	requestID uint32,
	deadline time.Time,
	msg []byte,
) error {
	h := a.handler.Get()
	return h.CrossChainAppRequest(
		ctx,
		chainID,
		requestID,
		deadline,
		msg,
	)
}

func (a *atomicAppHandler) CrossChainAppRequestFailed(
	ctx context.Context,
	chainID ids.ID,
	requestID uint32,
) error {
	h := a.handler.Get()
	return h.CrossChainAppRequestFailed(
		ctx,
		chainID,
		requestID,
	)
}

func (a *atomicAppHandler) CrossChainAppResponse(
	ctx context.Context,
	chainID ids.ID,
	requestID uint32,
	msg []byte,
) error {
	h := a.handler.Get()
	return h.CrossChainAppResponse(
		ctx,
		chainID,
		requestID,
		msg,
	)
}

func (a *atomicAppHandler) AppRequest(
	ctx context.Context,
	nodeID ids.NodeID,
	requestID uint32,
	deadline time.Time,
	msg []byte,
) error {
	h := a.handler.Get()
	return h.AppRequest(
		ctx,
		nodeID,
		requestID,
		deadline,
		msg,
	)
}

func (a *atomicAppHandler) AppRequestFailed(
	ctx context.Context,
	nodeID ids.NodeID,
	requestID uint32,
) error {
	h := a.handler.Get()
	return h.AppRequestFailed(
		ctx,
		nodeID,
		requestID,
	)
}

func (a *atomicAppHandler) AppResponse(
	ctx context.Context,
	nodeID ids.NodeID,
	requestID uint32,
	msg []byte,
) error {
	h := a.handler.Get()
	return h.AppResponse(
		ctx,
		nodeID,
		requestID,
		msg,
	)
}

func (a *atomicAppHandler) AppGossip(
	ctx context.Context,
	nodeID ids.NodeID,
	msg []byte,
) error {
	h := a.handler.Get()
	return h.AppGossip(
		ctx,
		nodeID,
		msg,
	)
}

func (a *atomicAppHandler) Set(h AppHandler) {
	a.handler.Set(h)
}

// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package p2p

import (
	"context"
	"time"

	"github.com/ava-labs/avalanchego/ids"
)

type Handler interface {
	AppGossip(ctx context.Context, nodeID ids.NodeID, gossipBytes []byte) error
	AppRequest(ctx context.Context, nodeID ids.NodeID, requestID uint32, deadline time.Time, requestBytes []byte) ([]byte, error)
	CrossChainAppRequest(ctx context.Context, chainID ids.ID, requestID uint32, deadline time.Time, requestBytes []byte) ([]byte, error)
}

type responder struct {
	handler Handler
	client  *Client
}

func (r responder) AppRequest(ctx context.Context, nodeID ids.NodeID, requestID uint32, deadline time.Time, request []byte) error {
	appResponse, err := r.handler.AppRequest(ctx, nodeID, requestID, deadline, request)
	if err != nil {
		return err
	}

	return r.client.AppResponse(ctx, nodeID, requestID, appResponse)
}

func (r responder) AppGossip(ctx context.Context, nodeID ids.NodeID, msg []byte) error {
	return r.handler.AppGossip(ctx, nodeID, msg)
}

func (r responder) CrossChainAppRequest(ctx context.Context, chainID ids.ID, requestID uint32, deadline time.Time, request []byte) error {
	appResponse, err := r.handler.CrossChainAppRequest(ctx, chainID, requestID, deadline, request)
	if err != nil {
		return err
	}

	return r.client.CrossChainAppResponse(ctx, chainID, requestID, appResponse)
}

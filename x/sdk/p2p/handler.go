// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package p2p

import (
	"context"
	"errors"
	"time"

	"github.com/ava-labs/avalanchego/ids"
)

var ErrAppRequestFailed = errors.New("app request failed")

type Handler interface {
	AppGossip(ctx context.Context, nodeID ids.NodeID, gossipBytes []byte) error
	AppRequest(ctx context.Context, nodeID ids.NodeID, requestID uint32, deadline time.Time, requestBytes []byte) ([]byte, error)
	CrossChainAppRequest(ctx context.Context, chainID ids.ID, requestID uint32, deadline time.Time, requestBytes []byte) ([]byte, error)
}

type responder struct {
	handler Handler
	client  *Client
}

func (a responder) AppRequest(ctx context.Context, nodeID ids.NodeID, requestID uint32, deadline time.Time, request []byte) error {
	appResponse, err := a.handler.AppRequest(ctx, nodeID, requestID, deadline, request)
	if err != nil {
		return err
	}

	return a.client.AppResponse(ctx, nodeID, requestID, appResponse)
}

func (a responder) AppGossip(ctx context.Context, nodeID ids.NodeID, msg []byte) error {
	return a.handler.AppGossip(ctx, nodeID, msg)
}

func (a responder) CrossChainAppRequest(ctx context.Context, chainID ids.ID, requestID uint32, deadline time.Time, request []byte) error {
	appResponse, err := a.handler.CrossChainAppRequest(ctx, chainID, requestID, deadline, request)
	if err != nil {
		return err
	}

	return a.client.CrossChainAppResponse(ctx, chainID, requestID, appResponse)
}

// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package p2p

import (
	"context"
	"errors"
	"fmt"

	"golang.org/x/exp/maps"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/utils/sampler"
	"github.com/ava-labs/avalanchego/utils/set"
)

var ErrRequestPending = errors.New("request pending")

type AppResponseCallback func(
	nodeID ids.NodeID,
	responseBytes []byte,
	err error,
)
type CrossChainAppResponseCallback func(
	chainID ids.ID,
	responseBytes []byte,
	err error,
)

type Client struct {
	handlerID uint8
	router    *Router
	sender    common.AppSender
}

func (c *Client) AppRequestAny(
	ctx context.Context,
	appRequestBytes []byte,
	onResponse AppResponseCallback,
) error {
	c.router.lock.RLock()
	peers := maps.Keys(c.router.peers)
	c.router.lock.RUnlock()

	s := sampler.NewUniform()
	s.Initialize(uint64(len(peers)))
	drawn, err := s.Next()
	if err != nil {
		return err
	}

	nodeIDs := set.Set[ids.NodeID]{peers[drawn]: struct{}{}}
	return c.AppRequest(ctx, nodeIDs, appRequestBytes, onResponse)
}

func (c *Client) AppRequest(
	ctx context.Context,
	nodeIDs set.Set[ids.NodeID],
	appRequestBytes []byte,
	onResponse AppResponseCallback,
) error {
	appRequestBytes = c.prefixMessage(appRequestBytes)
	for nodeID := range nodeIDs {

		c.router.lock.Lock()
		requestID := c.router.requestID

		if _, ok := c.router.pendingAppRequests[requestID]; ok {
			return fmt.Errorf(
				"failed to issue request with request id %d: %w",
				requestID,
				ErrRequestPending,
			)
		}

		if err := c.sender.SendAppRequest(
			ctx,
			set.Set[ids.NodeID]{nodeID: struct{}{}},
			requestID,
			appRequestBytes,
		); err != nil {
			return err
		}

		c.router.pendingAppRequests[requestID] = pendingAppRequest{
			onResponse: onResponse,
		}

		c.router.requestID++
		c.router.lock.Unlock()
	}
	return nil
}

func (c *Client) AppResponse(
	ctx context.Context,
	nodeID ids.NodeID,
	requestID uint32,
	response []byte,
) error {
	// response doesn't need to be prefixed with the app protocol because
	// it is registered in the router when making the request
	return c.sender.SendAppResponse(ctx, nodeID, requestID, response)
}

func (c *Client) AppGossip(
	ctx context.Context,
	appGossipBytes []byte,
) error {
	return c.sender.SendAppGossip(
		ctx,
		c.prefixMessage(appGossipBytes),
	)
}

func (c *Client) AppGossipSpecific(
	ctx context.Context,
	nodeIDs set.Set[ids.NodeID],
	appGossipBytes []byte,
) error {
	return c.sender.SendAppGossipSpecific(
		ctx,
		nodeIDs,
		c.prefixMessage(appGossipBytes),
	)
}

func (c *Client) CrossChainAppRequest(
	ctx context.Context,
	chainID ids.ID,
	appRequestBytes []byte,
	onResponse CrossChainAppResponseCallback,
) error {
	c.router.lock.Lock()
	requestID := c.router.requestID

	if _, ok := c.router.pendingCrossChainAppRequests[requestID]; ok {
		return fmt.Errorf(
			"failed to issue request with request id %d: %w",
			requestID,
			ErrRequestPending,
		)
	}

	if err := c.sender.SendCrossChainAppRequest(
		ctx,
		chainID,
		c.router.requestID,
		c.prefixMessage(appRequestBytes),
	); err != nil {
		return err
	}

	c.router.pendingCrossChainAppRequests[requestID] = pendingCrossChainAppRequest{
		onResponse: onResponse,
	}
	c.router.requestID++
	c.router.lock.Unlock()

	return nil
}

func (c *Client) CrossChainAppResponse(
	ctx context.Context,
	chainID ids.ID,
	requestID uint32,
	response []byte,
) error {
	// response doesn't need to be prefixed with the app protocol because
	// it is registered in the router when making the request
	return c.sender.SendCrossChainAppResponse(ctx, chainID, requestID, response)
}

func (c *Client) prefixMessage(src []byte) []byte {
	messageBytes := make([]byte, 1+len(src))
	messageBytes[0] = c.handlerID
	copy(messageBytes[1:], src)
	return messageBytes
}

// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package appsender

import (
	"context"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/utils/set"

	appsenderpb "github.com/ava-labs/avalanchego/proto/pb/appsender"
)

var _ common.AppSender = (*Client)(nil)

type Client struct {
	client appsenderpb.AppSenderClient
}

// NewClient returns a client that is connected to a remote AppSender.
func NewClient(client appsenderpb.AppSenderClient) *Client {
	return &Client{client: client}
}

func (c *Client) SendCrossChainAppRequest(ctx context.Context, chainID ids.ID, requestID uint32, appRequestBytes []byte) error {
	_, err := c.client.SendCrossChainAppRequest(
		ctx,
		&appsenderpb.SendCrossChainAppRequestMsg{
			ChainId:   chainID[:],
			RequestId: requestID,
			Request:   appRequestBytes,
		},
	)
	return err
}

func (c *Client) SendCrossChainAppResponse(ctx context.Context, chainID ids.ID, requestID uint32, appResponseBytes []byte) error {
	_, err := c.client.SendCrossChainAppResponse(
		ctx,
		&appsenderpb.SendCrossChainAppResponseMsg{
			ChainId:   chainID[:],
			RequestId: requestID,
			Response:  appResponseBytes,
		},
	)
	return err
}

func (c *Client) SendCrossChainAppError(ctx context.Context, chainID ids.ID, requestID uint32, errorCode int32, errorMessage string) error {
	_, err := c.client.SendCrossChainAppError(
		ctx,
		&appsenderpb.SendCrossChainAppErrorMsg{
			ChainId:      chainID[:],
			RequestId:    requestID,
			ErrorCode:    errorCode,
			ErrorMessage: errorMessage,
		},
	)

	return err
}

func (c *Client) SendAppRequest(ctx context.Context, nodeIDs set.Set[ids.NodeID], requestID uint32, request []byte) error {
	nodeIDsBytes := make([][]byte, nodeIDs.Len())
	i := 0
	for nodeID := range nodeIDs {
		nodeIDsBytes[i] = nodeID.Bytes()
		i++
	}
	_, err := c.client.SendAppRequest(
		ctx,
		&appsenderpb.SendAppRequestMsg{
			NodeIds:   nodeIDsBytes,
			RequestId: requestID,
			Request:   request,
		},
	)
	return err
}

func (c *Client) SendAppResponse(ctx context.Context, nodeID ids.NodeID, requestID uint32, response []byte) error {
	_, err := c.client.SendAppResponse(
		ctx,
		&appsenderpb.SendAppResponseMsg{
			NodeId:    nodeID.Bytes(),
			RequestId: requestID,
			Response:  response,
		},
	)
	return err
}

func (c *Client) SendAppError(ctx context.Context, nodeID ids.NodeID, requestID uint32, errorCode int32, errorMessage string) error {
	_, err := c.client.SendAppError(ctx,
		&appsenderpb.SendAppErrorMsg{
			NodeId:       nodeID[:],
			RequestId:    requestID,
			ErrorCode:    errorCode,
			ErrorMessage: errorMessage,
		},
	)

	return err
}

func (c *Client) SendAppGossip(
	ctx context.Context,
	msg []byte,
	numValidators int,
	numNonValidators int,
	numPeers int,
) error {
	_, err := c.client.SendAppGossip(
		ctx,
		&appsenderpb.SendAppGossipMsg{
			Msg:              msg,
			NumValidators:    uint64(numValidators),
			NumNonValidators: uint64(numNonValidators),
			NumPeers:         uint64(numPeers),
		},
	)
	return err
}

func (c *Client) SendAppGossipSpecific(ctx context.Context, nodeIDs set.Set[ids.NodeID], msg []byte) error {
	nodeIDsBytes := make([][]byte, nodeIDs.Len())
	i := 0
	for nodeID := range nodeIDs {
		nodeIDsBytes[i] = nodeID.Bytes()
		i++
	}
	_, err := c.client.SendAppGossipSpecific(
		ctx,
		&appsenderpb.SendAppGossipSpecificMsg{
			NodeIds: nodeIDsBytes,
			Msg:     msg,
		},
	)
	return err
}

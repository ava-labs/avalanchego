// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
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
	config common.SendConfig,
	msg []byte,
) error {
	nodeIDs := make([][]byte, config.NodeIDs.Len())
	i := 0
	for nodeID := range config.NodeIDs {
		nodeIDs[i] = nodeID.Bytes()
		i++
	}
	_, err := c.client.SendAppGossip(
		ctx,
		&appsenderpb.SendAppGossipMsg{
			NodeIds:       nodeIDs,
			Validators:    uint64(config.Validators),
			NonValidators: uint64(config.NonValidators),
			Peers:         uint64(config.Peers),
			Msg:           msg,
		},
	)
	return err
}

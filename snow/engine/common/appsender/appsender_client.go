// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package appsender

import (
	"context"

	"github.com/ava-labs/avalanchego/api/proto/appsenderproto"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/engine/common"
)

var _ common.AppSender = &Client{}

type Client struct {
	client appsenderproto.AppSenderClient
}

// NewClient returns a client that is connected to a remote AppSender.
func NewClient(client appsenderproto.AppSenderClient) *Client {
	return &Client{client: client}
}

func (c *Client) SendAppRequest(nodeIDs ids.ShortSet, requestID uint32, request []byte) error {
	nodeIDsBytes := make([][]byte, nodeIDs.Len())
	i := 0
	for nodeID := range nodeIDs {
		nodeID := nodeID // Prevent overwrite in next iteration
		nodeIDsBytes[i] = nodeID[:]
		i++
	}
	_, err := c.client.SendAppRequest(
		context.Background(),
		&appsenderproto.SendAppRequestMsg{
			NodeIds:   nodeIDsBytes,
			RequestId: requestID,
			Request:   request,
		},
	)
	return err
}

func (c *Client) SendAppResponse(nodeID ids.ShortID, requestID uint32, response []byte) error {
	_, err := c.client.SendAppResponse(
		context.Background(),
		&appsenderproto.SendAppResponseMsg{
			NodeId:    nodeID[:],
			RequestId: requestID,
			Response:  response,
		},
	)
	return err
}

func (c *Client) SendAppGossip(msg []byte) error {
	_, err := c.client.SendAppGossip(
		context.Background(),
		&appsenderproto.SendAppGossipMsg{
			Msg: msg,
		},
	)
	return err
}

func (c *Client) SendAppGossipSpecific(nodeIDs ids.ShortSet, msg []byte) error {
	nodeIDsBytes := make([][]byte, nodeIDs.Len())
	i := 0
	for nodeID := range nodeIDs {
		nodeID := nodeID // Prevent overwrite in next iteration
		nodeIDsBytes[i] = nodeID[:]
		i++
	}
	_, err := c.client.SendAppGossipSpecific(
		context.Background(),
		&appsenderproto.SendAppGossipSpecificMsg{
			NodeIds: nodeIDsBytes,
			Msg:     msg,
		},
	)
	return err
}

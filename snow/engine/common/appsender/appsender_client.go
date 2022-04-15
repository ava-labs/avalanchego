// Copyright (C) 2022, Chain4Travel AG. All rights reserved.
//
// This file is a derived work, based on ava-labs code whose
// original notices appear below.
//
// It is distributed under the same license conditions as the
// original code from which it is derived.
//
// Much love to the original authors for their work.
// **********************************************************

// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package appsender

import (
	"context"

	"github.com/chain4travel/caminogo/ids"
	"github.com/chain4travel/caminogo/snow/engine/common"

	appsenderpb "github.com/chain4travel/caminogo/proto/pb/appsender"
)

var _ common.AppSender = &Client{}

type Client struct {
	client appsenderpb.AppSenderClient
}

// NewClient returns a client that is connected to a remote AppSender.
func NewClient(client appsenderpb.AppSenderClient) *Client {
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
		&appsenderpb.SendAppRequestMsg{
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
		&appsenderpb.SendAppResponseMsg{
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
		&appsenderpb.SendAppGossipMsg{
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
		&appsenderpb.SendAppGossipSpecificMsg{
			NodeIds: nodeIDsBytes,
			Msg:     msg,
		},
	)
	return err
}

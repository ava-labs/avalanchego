// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package appsender

import (
	"context"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/engine/common"

	appsenderpb "github.com/ava-labs/avalanchego/proto/pb/appsender"
)

var _ common.AppSender = &Client{}

type Client struct {
	client appsenderpb.AppSenderClient
}

// NewClient returns a client that is connected to a remote AppSender.
func NewClient(client appsenderpb.AppSenderClient) *Client {
	return &Client{client: client}
}

func (c *Client) SendAppRequest(nodeIDs ids.NodeIDSet, sourceChainID ids.ID, destinationChainID ids.ID, requestID uint32, request []byte) error {
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
			NodeIds:            nodeIDsBytes,
			SourceChainId:      sourceChainID[:],
			DestinationChainId: destinationChainID[:],
			RequestId:          requestID,
			Request:            request,
		},
	)

	return err
}

func (c *Client) SendAppResponse(nodeID ids.NodeID, sourceChainID ids.ID, destinationChainID ids.ID, requestID uint32, response []byte) error {
	_, err := c.client.SendAppResponse(
		context.Background(),
		&appsenderpb.SendAppResponseMsg{
			NodeId:             nodeID[:],
			SourceChainId:      sourceChainID[:],
			DestinationChainId: destinationChainID[:],
			RequestId:          requestID,
			Response:           response,
		},
	)

	return err
}

func (c *Client) SendAppGossip(sourceChainID ids.ID, destinationChainID ids.ID, msg []byte) error {
	_, err := c.client.SendAppGossip(
		context.Background(),
		&appsenderpb.SendAppGossipMsg{
			SourceChainId:      sourceChainID[:],
			DestinationChainId: destinationChainID[:],
			Msg:                msg,
		},
	)
	return err
}

func (c *Client) SendAppGossipSpecific(nodeIDs ids.NodeIDSet, sourceChainID ids.ID, destinationChainID ids.ID, msg []byte) error {
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
			NodeIds:            nodeIDsBytes,
			SourceChainId:      sourceChainID[:],
			DestinationChainId: destinationChainID[:],
			Msg:                msg,
		},
	)
	return err
}
